package cmd

import (
	"fmt"
	"time"

	"github.com/blakep-lms/tally/internal/core"
	"github.com/spf13/cobra"
)

func reportCmd() *cobra.Command {
	var week, today, all bool
	var format, since, until string
	c := &cobra.Command{
		Use:   "report",
		Short: "Per-project hours summary (markdown, csv, or json)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()

			now := time.Now()
			from, to := core.WeekRange(now)
			switch {
			case today:
				from, to = core.TodayRange(now)
			case all:
				from, to = time.Time{}, now.AddDate(0, 0, 1)
			case week:
				from, to = core.WeekRange(now)
			}
			if since != "" {
				t, err := time.Parse("2006-01-02", since)
				if err != nil {
					return fmt.Errorf("invalid --since: %w", err)
				}
				from = t
			}
			if until != "" {
				t, err := time.Parse("2006-01-02", until)
				if err != nil {
					return fmt.Errorf("invalid --until: %w", err)
				}
				to = t.AddDate(0, 0, 1)
			}

			rep, err := app.Report(from, to)
			if err != nil {
				return err
			}
			if jsonOut || format == "json" {
				return emitJSON(rep)
			}
			switch format {
			case "csv":
				out, err := rep.CSV()
				if err != nil {
					return err
				}
				fmt.Print(out)
			default:
				fmt.Print(rep.Markdown())
			}
			return nil
		},
	}
	c.Flags().BoolVar(&week, "week", false, "this week (default)")
	c.Flags().BoolVar(&today, "today", false, "today")
	c.Flags().BoolVar(&all, "all", false, "all time")
	c.Flags().StringVar(&format, "format", "markdown", "markdown, csv, or json")
	c.Flags().StringVar(&since, "since", "", "start date YYYY-MM-DD")
	c.Flags().StringVar(&until, "until", "", "end date YYYY-MM-DD (inclusive)")
	return c
}
