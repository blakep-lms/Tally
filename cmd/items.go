package cmd

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/blakep-lms/tally/internal/model"
	"github.com/spf13/cobra"
)

func itemsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "items", Aliases: []string{"item"}, Short: "Manage work items"}
	cmd.AddCommand(itemsAddCmd(), itemsListCmd(), itemsUpdateCmd(), itemsDoneCmd(), itemsReactivateCmd())
	return cmd
}

func itemsAddCmd() *cobra.Command {
	var kind, context, desc string
	c := &cobra.Command{Use: "add <name>", Short: "Create a work item", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		app, closeFn, err := openApp()
		if err != nil {
			return err
		}
		defer closeFn()
		w, err := app.AddWorkItem(args[0], model.WorkItemKind(kind), context, desc)
		if err != nil {
			return err
		}
		if jsonOut {
			return emitJSON(w)
		}
		fmt.Printf("Created item #%d %q (%s)\n", w.ID, w.Name, w.Kind)
		return nil
	}}
	c.Flags().StringVar(&kind, "kind", "project", "project, product, goal, or other")
	c.Flags().StringVar(&context, "context", "", "optional user/client/product context label")
	c.Flags().StringVar(&desc, "description", "", "optional description")
	return c
}
func itemsListCmd() *cobra.Command {
	var status string
	c := &cobra.Command{Use: "list", Short: "List work items", RunE: func(cmd *cobra.Command, args []string) error {
		app, closeFn, err := openApp()
		if err != nil {
			return err
		}
		defer closeFn()
		items, err := app.ListWorkItems(model.WorkItemStatus(status))
		if err != nil {
			return err
		}
		if jsonOut {
			return emitJSON(items)
		}
		if len(items) == 0 {
			fmt.Println("No work items yet. Add one with `tally items add <name>`.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tKIND\tCONTEXT\tSTATUS")
		for _, it := range items {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", it.ID, it.Name, it.Kind, dashv(it.Context), it.Status)
		}
		return w.Flush()
	}}
	c.Flags().StringVar(&status, "status", "", "filter by status: active or done")
	return c
}
func itemsUpdateCmd() *cobra.Command {
	var name, kind, context, desc string
	c := &cobra.Command{Use: "update <id>", Short: "Update a work item", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		app, closeFn, err := openApp()
		if err != nil {
			return err
		}
		defer closeFn()
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return err
		}
		cur, err := app.Store.GetWorkItem(id)
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("name") {
			name = cur.Name
		}
		if !cmd.Flags().Changed("kind") {
			kind = string(cur.Kind)
		}
		if !cmd.Flags().Changed("context") {
			context = cur.Context
		}
		if !cmd.Flags().Changed("description") {
			desc = cur.Description
		}
		w, err := app.UpdateWorkItem(id, name, model.WorkItemKind(kind), context, desc)
		if err != nil {
			return err
		}
		if jsonOut {
			return emitJSON(w)
		}
		fmt.Printf("Updated item #%d %q.\n", w.ID, w.Name)
		return nil
	}}
	c.Flags().StringVar(&name, "name", "", "new name")
	c.Flags().StringVar(&kind, "kind", "", "project, product, goal, or other")
	c.Flags().StringVar(&context, "context", "", "context label")
	c.Flags().StringVar(&desc, "description", "", "description")
	return c
}
func itemsDoneCmd() *cobra.Command {
	return &cobra.Command{Use: "done <id|name>", Aliases: []string{"archive"}, Short: "Mark a work item done", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		app, closeFn, err := openApp()
		if err != nil {
			return err
		}
		defer closeFn()
		w, err := app.MarkWorkItemDone(args[0])
		if err != nil {
			return err
		}
		if jsonOut {
			return emitJSON(w)
		}
		fmt.Printf("Archived %q. Its rules are now inactive; history is preserved.\n", w.Name)
		return nil
	}}
}
func itemsReactivateCmd() *cobra.Command {
	return &cobra.Command{Use: "reactivate <id|name>", Short: "Reactivate a done work item", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		app, closeFn, err := openApp()
		if err != nil {
			return err
		}
		defer closeFn()
		w, err := app.ReactivateWorkItem(args[0])
		if err != nil {
			return err
		}
		if jsonOut {
			return emitJSON(w)
		}
		fmt.Printf("Reactivated %q. Its existing rules are active again.\n", w.Name)
		return nil
	}}
}
