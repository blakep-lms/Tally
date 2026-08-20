package cmd

import (
	"fmt"
	"strconv"

	"github.com/blakep-lms/tally/internal/model"
	"github.com/spf13/cobra"
)

func billingCmd() *cobra.Command {
	root := &cobra.Command{Use: "billing", Short: "Configure optional billing profiles and preview billing-ready exports"}
	var client, project string
	show := &cobra.Command{Use: "show", RunE: func(cmd *cobra.Command, args []string) error {
		app, closeFn, err := openApp()
		if err != nil {
			return err
		}
		defer closeFn()
		if project != "" {
			r, err := app.ResolveBillingProfile(project)
			if err != nil {
				return err
			}
			return emitJSON(r)
		}
		scope, key := model.BillingScopeGlobal, ""
		if client != "" {
			scope, key = model.BillingScopeClient, client
		}
		p, err := app.Store.GetBillingProfile(scope, key)
		if err != nil {
			return err
		}
		return emitJSON(p)
	}}
	show.Flags().StringVar(&client, "client", "", "client/context scope")
	show.Flags().StringVar(&project, "project", "", "work item/project scope")
	show.Flags().StringVar(&project, "item", "", "work item scope")
	var rate int64
	var enabled bool
	var currency, roundingMode, roundingScope, periodMode, anchor string
	var minutes int
	set := &cobra.Command{Use: "set", RunE: func(cmd *cobra.Command, args []string) error {
		app, closeFn, err := openApp()
		if err != nil {
			return err
		}
		defer closeFn()
		p := model.DefaultBillingProfile()
		p.HourlyRateMinor = rate
		p.Enabled = enabled
		p.Currency = currency
		p.RoundingMode = model.RoundingMode(roundingMode)
		p.RoundingIncrementMinutes = minutes
		p.RoundingScope = model.RoundingScope(roundingScope)
		p.PeriodMode = model.PeriodMode(periodMode)
		p.PeriodAnchor = anchor
		if project != "" {
			w, err := app.ResolveWorkItem(project)
			if err != nil {
				return err
			}
			p.ScopeType = model.BillingScopeWorkItem
			p.ScopeKey = strconv.FormatInt(w.ID, 10)
		} else if client != "" {
			p.ScopeType = model.BillingScopeClient
			p.ScopeKey = client
		} else {
			p.ScopeType = model.BillingScopeGlobal
		}
		out, err := app.SetBillingProfile(p)
		if err != nil {
			return err
		}
		return emitJSON(out)
	}}
	set.Flags().StringVar(&client, "client", "", "client/context scope")
	set.Flags().StringVar(&project, "project", "", "work item/project scope")
	set.Flags().StringVar(&project, "item", "", "work item scope")
	set.Flags().Int64Var(&rate, "rate-minor", 0, "hourly rate in minor units")
	set.Flags().BoolVar(&enabled, "enabled", true, "include matching work in billing projections")
	set.Flags().StringVar(&currency, "currency", "USD", "currency code")
	set.Flags().StringVar(&roundingMode, "rounding-mode", "up", "rounding policy (v1: up)")
	set.Flags().IntVar(&minutes, "rounding-minutes", 15, "rounding interval minutes")
	set.Flags().StringVar(&roundingScope, "rounding-scope", "period_work_item", "rounding scope (v1: period_work_item)")
	set.Flags().StringVar(&periodMode, "period-mode", "custom", "weekly, biweekly, semimonthly, monthly, final, custom")
	set.Flags().StringVar(&anchor, "period-anchor", "", "optional period anchor")
	preview := reportCmd()
	preview.Use = "preview"
	preview.Short = "Preview billing-ready exact/adjusted export"
	preview.Flags().Set("billing", "true")
	snapshots := &cobra.Command{Use: "snapshots", Short: "List or inspect frozen billing report snapshots"}
	snapshots.AddCommand(
		&cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()
			list, err := app.Store.ListReportSnapshots()
			if err != nil {
				return err
			}
			return emitJSON(list)
		}},
		&cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid snapshot ID: %w", err)
			}
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()
			snapshot, err := app.Store.GetReportSnapshot(id)
			if err != nil {
				return err
			}
			return emitJSON(snapshot)
		}},
	)
	root.AddCommand(show, set, preview, snapshots)
	return root
}
