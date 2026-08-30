package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsArePrivateAndLive(t *testing.T) {
	cfg := Defaults()
	if cfg.UIAddr != "127.0.0.1:7654" || cfg.AutoSyncIntervalSeconds <= 0 {
		t.Fatalf("unsafe or inactive defaults: %+v", cfg)
	}
	if cfg.StoreURLPaths || len(cfg.IgnoredApps) == 0 || cfg.LLMEnabled {
		t.Fatalf("privacy defaults: %+v", cfg)
	}
}

func TestSaveLoadPathsPermissionsAndEnvFallbacks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TALLY_HOME", home)
	cfg := Defaults()
	cfg.LLMMinConfidence = 0.91
	cfg.HTTPAPIToken = "configured-token"
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LLMMinConfidence != 0.91 || loaded.APIToken() != "configured-token" {
		t.Fatalf("loaded=%+v", loaded)
	}
	info, err := os.Stat(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config permissions=%#o", info.Mode().Perm())
	}
	if db, _ := DBPath(); db != filepath.Join(home, "tally.db") {
		t.Fatalf("db path=%q", db)
	}

	t.Setenv("TALLY_API_TOKEN", "env-token")
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	if got := (Config{}).APIToken(); got != "env-token" {
		t.Fatalf("api token=%q", got)
	}
	if got := (Config{}).APIKey(); got != "env-key" {
		t.Fatalf("api key=%q", got)
	}
}
