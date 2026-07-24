package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func syncCmd() *cobra.Command {
	var days int
	var since, until string
	c := &cobra.Command{
		Use:   "sync",
		Short: "Pull events from the capture provider (idempotent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()

			to := time.Now()
			from := to.AddDate(0, 0, -days)
			if since != "" {
				t, err := time.Parse("2006-01-02", since)
				if err != nil {
					return fmt.Errorf("invalid --since date: %w", err)
				}
				from = t
			}
			if until != "" {
				t, err := time.Parse("2006-01-02", until)
				if err != nil {
					return fmt.Errorf("invalid --until date: %w", err)
				}
				to = t.AddDate(0, 0, 1)
			}

			res, err := app.Sync(cmd.Context(), from, to)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(res)
			}
			fmt.Printf("Pulled %d event(s): %d new, %d updated.\n", res.Pulled, res.Created, res.Updated)
			return nil
		},
	}
	c.Flags().IntVar(&days, "days", 1, "how many days back to pull")
	c.Flags().StringVar(&since, "since", "", "start date YYYY-MM-DD (overrides --days)")
	c.Flags().StringVar(&until, "until", "", "end date YYYY-MM-DD (inclusive)")
	return c
}
