package cmd

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/blakep-lms/tally/internal/model"
	"github.com/spf13/cobra"
)

func rulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rules",
		Aliases: []string{"rule"},
		Short:   "Manage classification rules",
	}
	cmd.AddCommand(rulesAddCmd(), rulesListCmd(), rulesTestCmd(), rulesDeleteCmd())
	return cmd
}

func rulesAddCmd() *cobra.Command {
	var project, field, match string
	var priority int
	c := &cobra.Command{
		Use:   "add <pattern>",
		Short: "Add a rule mapping matching events to a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()
			rule, err := app.AddRule(project, model.RuleField(field), model.MatchKind(match), args[0], priority)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(rule)
			}
			fmt.Printf("Added rule #%d: %s %s %q -> project #%d\n", rule.ID, rule.Field, rule.Match, rule.Pattern, rule.ProjectID)
			return nil
		},
	}
	c.Flags().StringVar(&project, "project", "", "project id or name (required)")
	c.Flags().StringVar(&field, "field", "title", "app, title, url, or repo")
	c.Flags().StringVar(&match, "match", "contains", "contains, equals, or regex")
	c.Flags().IntVar(&priority, "priority", 100, "lower is evaluated first")
	c.MarkFlagRequired("project")
	return c
}

func rulesListCmd() *cobra.Command {
	var activeOnly bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()
			rules, err := app.ListRules(activeOnly)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(rules)
			}
			if len(rules) == 0 {
				fmt.Println("No rules yet.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tPROJECT\tFIELD\tMATCH\tPATTERN\tPRIO\tACTIVE")
			for _, r := range rules {
				fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\t%d\t%v\n", r.ID, r.ProjectID, r.Field, r.Match, r.Pattern, r.Priority, r.Active)
			}
			return w.Flush()
		},
	}
	c.Flags().BoolVar(&activeOnly, "active", false, "only active rules of active projects")
	return c
}

func rulesTestCmd() *cobra.Command {
	var field, match string
	var limit int
	c := &cobra.Command{
		Use:   "test <pattern>",
		Short: "Dry-run a rule against captured events",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()
			hits, err := app.TestRule(model.RuleField(field), model.MatchKind(match), args[0], limit)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(hits)
			}
			fmt.Printf("%d event(s) would match:\n", len(hits))
			for _, e := range hits {
				fmt.Printf("  [%d] %s | %s\n", e.ID, e.App, e.Title)
			}
			return nil
		},
	}
	c.Flags().StringVar(&field, "field", "title", "app, title, url, or repo")
	c.Flags().StringVar(&match, "match", "contains", "contains, equals, or regex")
	c.Flags().IntVar(&limit, "limit", 25, "max events to show")
	return c
}

func rulesDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid rule id %q", args[0])
			}
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()
			if err := app.DeleteRule(id); err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(map[string]any{"deleted": id})
			}
			fmt.Printf("Deleted rule #%d\n", id)
			return nil
		},
	}
}
