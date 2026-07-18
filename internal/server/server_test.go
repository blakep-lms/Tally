package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blakep-lms/tally/internal/config"
	"github.com/blakep-lms/tally/internal/core"
	"github.com/blakep-lms/tally/internal/store"
)

func newServer(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(core.New(st, config.Defaults())).Handler()
}

func TestAPIProjectsAndReport(t *testing.T) {
	h := newServer(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Create a project via the API.
	resp, err := http.Post(srv.URL+"/api/projects", "application/json",
		strings.NewReader(`{"name":"Web","type":"billable","client":"ACME"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create project status = %d", resp.StatusCode)
	}
	var p map[string]any
	json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()
	if p["name"] != "Web" {
		t.Errorf("unexpected project: %v", p)
	}

	// List projects.
	resp, _ = http.Get(srv.URL + "/api/projects")
	var list []map[string]any
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("expected 1 project, got %d", len(list))
	}

	// Report endpoint responds with the expected shape.
	resp, _ = http.Get(srv.URL + "/api/report?range=all")
	var rep map[string]any
	json.NewDecoder(resp.Body).Decode(&rep)
	resp.Body.Close()
	if _, ok := rep["total_hours"]; !ok {
		t.Errorf("report missing total_hours: %v", rep)
	}
}

func TestServesSPA(t *testing.T) {
	h := newServer(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d", resp.StatusCode)
	}
	buf := make([]byte, 64)
	resp.Body.Read(buf)
	if !strings.Contains(string(buf), "<!DOCTYPE html>") {
		t.Errorf("expected HTML index, got %q", string(buf))
	}
}
