package cmd

import (
	"fmt"

	"github.com/blakep-lms/tally/internal/config"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Set up Tally: write config, create the database, check for ActivityWatch",
		Long: "Initializes Tally's data directory (~/.tally), writes a default config,\n" +
			"and verifies whether an ActivityWatch capture provider is reachable.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			dir, _ := config.Dir()

			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()

			connected := app.Provider().Available(cmd.Context())
			if jsonOut {
				return emitJSON(map[string]any{
					"home":               dir,
					"activitywatch_url":  cfg.ActivityWatchURL,
					"provider_connected": connected,
				})
			}

			fmt.Printf("Initialized Tally in %s\n", dir)
			if connected {
				fmt.Printf("ActivityWatch is running at %s — capture is ready.\n", cfg.ActivityWatchURL)
				fmt.Println("Next: define work items (`tally items add`), then `tally sync` and `tally classify`.")
			} else {
				fmt.Printf("No capture provider connected at %s.\n", cfg.ActivityWatchURL)
				fmt.Println("Install and start ActivityWatch (https://activitywatch.net/), then re-run `tally status`.")
			}
			return nil
		},
	}
}
