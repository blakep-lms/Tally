// Package cmd implements Tally's command-line interface. Every command shares
// the core application service and supports --json for machine consumption.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	"github.com/blakep-lms/tally/internal/config"
	"github.com/blakep-lms/tally/internal/core"
	"github.com/blakep-lms/tally/internal/store"
	"github.com/spf13/cobra"
)

// version is stamped at build time via -ldflags.
var version = "dev"

var jsonOut bool

// SetVersion lets main inject the build version.
func SetVersion(v string) {
	if v != "" {
		version = v
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "tally",
		Short: "Automatic, local-first time tracking across all kinds of work.",
		Long: "Tally passively captures what you focus on, classifies it into projects, products, goals, and other work,\n" +
			"then reports exact time with optional billing projections — no timers, ever.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
	root.AddCommand(
		initCmd(), doctorCmd(), statusCmd(), itemsCmd(), projectsCmd(), rulesCmd(),
		classifyCmd(), reportCmd(), billingCmd(), syncCmd(), uiCmd(), mcpCmd(),
	)
	return root
}

// Execute runs the CLI with a signal-aware context so long-running commands
// (ui, mcp) shut down cleanly on Ctrl+C.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := rootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// openApp loads config and opens the store, returning the shared service.
func openApp() (*core.App, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	path, err := config.DBPath()
	if err != nil {
		return nil, nil, err
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	return core.New(st, cfg), func() { st.Close() }, nil
}

// emitJSON prints v as indented JSON to stdout.
func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
