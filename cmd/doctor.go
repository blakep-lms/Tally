package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blakep-lms/tally/internal/capture"
	"github.com/blakep-lms/tally/internal/config"
	"github.com/spf13/cobra"
)

type doctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type doctorReport struct {
	Healthy bool          `json:"healthy"`
	Home    string        `json:"home"`
	Checks  []doctorCheck `json:"checks"`
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check Tally configuration, data, privacy, and capture health",
		RunE: func(cmd *cobra.Command, args []string) error {
			report := runDoctor(cmd.Context())
			if jsonOut {
				if err := emitJSON(report); err != nil {
					return err
				}
			} else {
				fmt.Println("Tally doctor")
				for _, check := range report.Checks {
					mark := "✓"
					if !check.OK {
						mark = "✗"
					}
					fmt.Printf("  %s %-18s %s\n", mark, check.Name, check.Detail)
				}
			}
			if !report.Healthy {
				return fmt.Errorf("Tally is not healthy; fix the failed checks and run `tally doctor` again")
			}
			return nil
		},
	}
}

func runDoctor(ctx context.Context) doctorReport {
	home, err := config.Dir()
	report := doctorReport{Home: home}
	add := func(name string, ok bool, detail string) {
		report.Checks = append(report.Checks, doctorCheck{Name: name, OK: ok, Detail: detail})
	}
	if err != nil {
		add("data directory", false, err.Error())
		report.Healthy = false
		return report
	}

	info, err := os.Stat(home)
	add("data directory", err == nil && info.IsDir() && info.Mode().Perm()&0o077 == 0, permissionDetail(info, err))

	configPath := filepath.Join(home, "config.toml")
	configInfo, configErr := os.Stat(configPath)
	cfg, loadErr := config.Load()
	configOK := configErr == nil && loadErr == nil && configInfo.Mode().Perm()&0o077 == 0
	configDetail := permissionDetail(configInfo, firstError(configErr, loadErr))
	add("config", configOK, configDetail)

	dbPath, dbPathErr := config.DBPath()
	dbOK, dbDetail := checkDatabase(dbPath, dbPathErr)
	add("database", dbOK, dbDetail)

	uiOK := isLoopbackAddress(cfg.UIAddr)
	add("dashboard binding", uiOK, cfg.UIAddr)

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	providerOK := capture.NewAWWithPrivacy(cfg.ActivityWatchURL, cfg.IgnoredApps, cfg.StoreURLPaths).Available(probeCtx)
	add("activitywatch", providerOK, cfg.ActivityWatchURL)

	report.Healthy = true
	for _, check := range report.Checks {
		report.Healthy = report.Healthy && check.OK
	}
	return report
}

func checkDatabase(path string, pathErr error) (bool, string) {
	if pathErr != nil {
		return false, pathErr.Error()
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err.Error()
	}
	if info.Mode().Perm()&0o077 != 0 {
		return false, fmt.Sprintf("%s has permissions %#o; expected 0600", path, info.Mode().Perm())
	}
	u := &url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro")
	if err != nil {
		return false, err.Error()
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return false, err.Error()
	}
	if strings.ToLower(result) != "ok" {
		return false, result
	}
	return true, path
}

func isLoopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func permissionDetail(info os.FileInfo, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("%s (%#o)", info.Name(), info.Mode().Perm())
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
