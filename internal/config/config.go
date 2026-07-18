// Package config resolves on-disk locations and user settings for Tally.
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds user-tunable settings persisted to ~/.tally/config.toml.
type Config struct {
	// ActivityWatchURL is the base URL of the local ActivityWatch server.
	ActivityWatchURL string `toml:"activitywatch_url"`
	// LLMEnabled turns on the optional LLM classification fallback.
	LLMEnabled bool `toml:"llm_enabled"`
	// LLMModel is the Anthropic model used for classification.
	LLMModel string `toml:"llm_model"`
	// AnthropicAPIKey is the BYO key. Empty means read ANTHROPIC_API_KEY env.
	AnthropicAPIKey string `toml:"anthropic_api_key,omitempty"`
	// UIAddr is the address the local dashboard binds to.
	UIAddr string `toml:"ui_addr"`
}

// Defaults returns the zero-config baseline.
func Defaults() Config {
	return Config{
		ActivityWatchURL: "http://localhost:5600",
		LLMEnabled:       false,
		LLMModel:         "claude-opus-4-8",
		UIAddr:           "127.0.0.1:7654",
	}
}

// Dir returns Tally's data directory (~/.tally), honoring TALLY_HOME.
func Dir() (string, error) {
	if h := os.Getenv("TALLY_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tally"), nil
}

// DBPath returns the path to the SQLite database file.
func DBPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "tally.db"), nil
}

func configPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.toml"), nil
}

// Load reads config from disk, falling back to defaults for missing values.
func Load() (Config, error) {
	cfg := Defaults()
	p, err := configPath()
	if err != nil {
		return cfg, err
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return cfg, nil
	}
	if _, err := toml.DecodeFile(p, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Save writes config to disk, creating the data directory if needed.
func Save(cfg Config) error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	p, err := configPath()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// APIKey returns the configured key, or the ANTHROPIC_API_KEY env fallback.
func (c Config) APIKey() string {
	if c.AnthropicAPIKey != "" {
		return c.AnthropicAPIKey
	}
	return os.Getenv("ANTHROPIC_API_KEY")
}
