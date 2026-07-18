package cmd

import (
	"os"

	"github.com/blakep-lms/tally/internal/mcp"
	"github.com/spf13/cobra"
)

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server (stdio) exposing full agent access",
		Long: "Speaks the Model Context Protocol over stdio. Register it in Claude Desktop\n" +
			"or Claude Code to give an agent the same reach a human has: list/create\n" +
			"projects, query hours, classify events, add rules, mark done, generate reports.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()
			return mcp.New(app).Serve(cmd.Context(), os.Stdin, os.Stdout)
		},
	}
}
