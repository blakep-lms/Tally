package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/blakep-lms/tally/internal/config"
	"github.com/blakep-lms/tally/internal/store"
)

func TestInitSupportsSetupAlias(t *testing.T) {
	if !initCmd().HasAlias("setup") {
		t.Fatal("init command must support the setup alias")
	}
}

func TestDoctorRejectsMissingInstallState(t *testing.T) {
	t.Setenv("TALLY_HOME", t.TempDir())
	report := runDoctor(context.Background())
	if report.Healthy {
		t.Fatal("doctor must reject an uninitialized install")
	}
	if !hasDoctorFailure(report, "config") || !hasDoctorFailure(report, "database") {
		t.Fatalf("missing config/database failures: %+v", report.Checks)
	}
}

func TestDoctorAcceptsHealthyIsolatedInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TALLY_HOME", home)

	aw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/0/info" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"test"}`))
	}))
	defer aw.Close()

	cfg := config.Defaults()
	cfg.ActivityWatchURL = aw.URL
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	dbPath, err := config.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	report := runDoctor(context.Background())
	if !report.Healthy {
		t.Fatalf("healthy install rejected: %+v", report.Checks)
	}
	if report.Home != home {
		t.Fatalf("home=%q want %q", report.Home, home)
	}
	if got := filepath.Join(report.Home, "config.toml"); got == "" {
		t.Fatal("doctor did not resolve the config path")
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal(err)
	}
}

func hasDoctorFailure(report doctorReport, name string) bool {
	for _, check := range report.Checks {
		if check.Name == name && !check.OK {
			return true
		}
	}
	return false
}
