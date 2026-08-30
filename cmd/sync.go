package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func syncCmd() *cobra.Command {
	var days int
	var since, until, timezone string
	c := &cobra.Command{
		Use:   "sync",
		Short: "Pull events from the capture provider (idempotent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()

			loc, err := time.LoadLocation(timezone)
			if err != nil {
				return fmt.Errorf("invalid --timezone: %w", err)
			}
			to := time.Now().In(loc)
			from := to.AddDate(0, 0, -days)
			if since != "" {
				t, err := time.ParseInLocation("2006-01-02", since, loc)
				if err != nil {
					return fmt.Errorf("invalid --since date: %w", err)
				}
				from = t
			}
			if until != "" {
				t, err := time.ParseInLocation("2006-01-02", until, loc)
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
			fmt.Printf("Pulled %d event(s): %d new, %d updated, %d deleted, %d reconciliation conflicts.\n", res.Pulled, res.Created, res.Updated, res.Deleted, res.Conflicts)
			return nil
		},
	}
	c.Flags().IntVar(&days, "days", 1, "how many days back to pull")
	c.Flags().StringVar(&since, "since", "", "start date YYYY-MM-DD (overrides --days)")
	c.Flags().StringVar(&until, "until", "", "end date YYYY-MM-DD (inclusive)")
	c.Flags().StringVar(&timezone, "timezone", time.Local.String(), "IANA timezone for date boundaries")
	return c
}
