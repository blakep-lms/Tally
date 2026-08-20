package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/blakep-lms/tally/internal/core"
	"github.com/blakep-lms/tally/internal/model"
	"github.com/blakep-lms/tally/internal/report"
	"github.com/spf13/cobra"
)

func reportCmd() *cobra.Command {
	var week, today, month, all, billing, finalize bool
	var format, since, until, fromFlag, toFlag, period, timezone, itemRef, label string
	c := &cobra.Command{
		Use:   "report",
		Short: "Exact work-item hours with an optional billing projection",
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
			now := time.Now().In(loc)
			mode := model.PeriodWeekly
			if period != "" {
				mode = model.PeriodMode(period)
				if !mode.Valid() {
					return fmt.Errorf("invalid --period %q", period)
				}
			}
			from, to := core.PeriodRange(mode, "", now)
			switch {
			case today:
				from, to = core.TodayRange(now)
				mode = model.PeriodCustom
			case month:
				from, to = core.PeriodRange(model.PeriodMonthly, "", now)
				mode = model.PeriodMonthly
			case all:
				from, to = time.Time{}, now.AddDate(0, 0, 1)
				mode = model.PeriodCustom
			case week:
				from, to = core.WeekRange(now)
				mode = model.PeriodWeekly
			}
			if fromFlag != "" {
				since = fromFlag
			}
			if toFlag != "" {
				until = toFlag
			}
			if (since == "") != (until == "") {
				return fmt.Errorf("--from/--since and --to/--until are required together")
			}
			if mode == model.PeriodCustom && period == string(model.PeriodCustom) && (since == "" || until == "") {
				return fmt.Errorf("--period custom requires --from and --to")
			}
			if since != "" {
				from, err = time.ParseInLocation("2006-01-02", since, loc)
				if err != nil {
					return fmt.Errorf("invalid --from/--since: %w", err)
				}
				mode = model.PeriodCustom
			}
			if until != "" {
				endDate, parseErr := time.ParseInLocation("2006-01-02", until, loc)
				if parseErr != nil {
					return fmt.Errorf("invalid --to/--until: %w", parseErr)
				}
				to = endDate.AddDate(0, 0, 1)
				mode = model.PeriodCustom
			}
			if !to.After(from) {
				return fmt.Errorf("report range must be non-empty")
			}

			var rep report.Report
			if mode == model.PeriodFinal {
				if itemRef == "" {
					return fmt.Errorf("--period final requires --item")
				}
				item, resolveErr := app.ResolveWorkItem(itemRef)
				if resolveErr != nil {
					return resolveErr
				}
				from, to = core.FinalRange(item, now)
				r, reportErr := app.ReportWorkItemWithBilling(item, from, to, billing)
				if reportErr != nil {
					return reportErr
				}
				rep = r
			} else {
				r, reportErr := app.ReportWithBilling(from, to, billing)
				if reportErr != nil {
					return reportErr
				}
				rep = r
			}
			if finalize {
				saved, saveErr := app.FinalizeReport(rep, label, mode, timezone)
				err = saveErr
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "saved report snapshot %d\n", saved.ID)
			}
			if jsonOut || format == "json" {
				out, jsonErr := rep.JSON()
				if jsonErr != nil {
					return jsonErr
				}
				fmt.Println(out)
				return nil
			}
			switch format {
			case "csv":
				out, csvErr := rep.CSV()
				if csvErr != nil {
					return csvErr
				}
				fmt.Print(out)
			case "markdown":
				fmt.Print(rep.Markdown())
			default:
				return fmt.Errorf("invalid --format %q", format)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&week, "week", false, "this week (default)")
	c.Flags().BoolVar(&today, "today", false, "today")
	c.Flags().BoolVar(&month, "month", false, "this month")
	c.Flags().BoolVar(&all, "all", false, "all time")
	c.Flags().BoolVar(&billing, "billing", false, "include billing projection for enabled items with rates")
	c.Flags().BoolVar(&finalize, "finalize", false, "freeze this report as an auditable snapshot")
	c.Flags().StringVar(&label, "label", "", "snapshot label")
	c.Flags().StringVar(&period, "period", "", "weekly, biweekly, semimonthly, monthly, final, or custom")
	c.Flags().StringVar(&itemRef, "item", "", "work item ID or name (required for final)")
	c.Flags().StringVar(&timezone, "timezone", time.Local.String(), "IANA timezone for period boundaries")
	c.Flags().StringVar(&format, "format", "markdown", "markdown, csv, or json")
	c.Flags().StringVar(&since, "since", "", "start date YYYY-MM-DD")
	c.Flags().StringVar(&until, "until", "", "end date YYYY-MM-DD (inclusive)")
	c.Flags().StringVar(&fromFlag, "from", "", "start date YYYY-MM-DD")
	c.Flags().StringVar(&toFlag, "to", "", "end date YYYY-MM-DD (inclusive)")
	c.MarkFlagsMutuallyExclusive("week", "today", "month", "all")
	c.MarkFlagsMutuallyExclusive("since", "from")
	c.MarkFlagsMutuallyExclusive("until", "to")
	return c
}
