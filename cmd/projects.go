package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/blakep-lms/tally/internal/model"
	"github.com/spf13/cobra"
)

func projectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Aliases: []string{"project"},
		Short:   "Manage projects",
	}
	cmd.AddCommand(projectsAddCmd(), projectsListCmd(), projectsDoneCmd())
	return cmd
}

func projectsAddCmd() *cobra.Command {
	var typ, client string
	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()
			p, err := app.AddProject(args[0], model.ProjectType(typ), client)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(p)
			}
			fmt.Printf("Created project #%d %q (%s)\n", p.ID, p.Name, p.Type)
			return nil
		},
	}
	c.Flags().StringVar(&typ, "type", "billable", "billable or internal")
	c.Flags().StringVar(&client, "client", "", "optional client label")
	return c
}

func projectsListCmd() *cobra.Command {
	var status string
	c := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()
			projects, err := app.ListProjects(model.ProjectStatus(status))
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(projects)
			}
			if len(projects) == 0 {
				fmt.Println("No projects yet. Add one with `tally projects add <name>`.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tTYPE\tCLIENT\tSTATUS")
			for _, p := range projects {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Type, dashv(p.Client), p.Status)
			}
			return w.Flush()
		},
	}
	c.Flags().StringVar(&status, "status", "", "filter by status: active or done")
	return c
}

func projectsDoneCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "done <id|name>",
		Aliases: []string{"archive"},
		Short:   "Mark a project done (archive it, keep its history)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()
			p, err := app.MarkDone(args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(p)
			}
			fmt.Printf("Archived %q. Its rules are now inactive; history is preserved.\n", p.Name)
			return nil
		},
	}
}

func dashv(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
