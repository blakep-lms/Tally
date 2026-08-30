package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/blakep-lms/tally/internal/core"
	"github.com/blakep-lms/tally/internal/model"
	"github.com/spf13/cobra"
)

func classifyCmd() *cobra.Command {
	var useLLM, interactive bool
	c := &cobra.Command{
		Use:   "classify",
		Short: "Classify unclassified events with rules (and optional LLM fallback)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()

			if interactive {
				return runInteractive(cmd, app)
			}

			res, err := app.Classify(cmd.Context(), useLLM)
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(res)
			}
			fmt.Printf("Considered %d unclassified event(s):\n", res.Considered)
			fmt.Printf("  matched by rule: %d\n", res.MatchedByRule)
			if useLLM {
				fmt.Printf("  matched by LLM:  %d\n", res.MatchedByLLM)
			}
			fmt.Printf("  still unclassified: %d\n", res.StillUnclassified)
			return nil
		},
	}
	c.Flags().BoolVar(&useLLM, "llm", false, "use the LLM fallback (requires llm_enabled and an API key)")
	c.Flags().BoolVarP(&interactive, "interactive", "i", false, "triage the unclassified queue one event at a time")
	return c
}

func runInteractive(cmd *cobra.Command, app *core.App) error {
	// Rules first so the queue only shows genuinely ambiguous time.
	if _, err := app.Classify(cmd.Context(), false); err != nil {
		return err
	}
	events, err := app.ListUnclassified(0)
	if err != nil {
		return err
	}
	items, err := app.ListWorkItems(model.StatusActive)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Println("Nothing to triage. 🎉")
		return nil
	}
	if len(items) == 0 {
		return fmt.Errorf("no active work items to assign to; create one first")
	}

	fmt.Println("Work items:")
	for _, item := range items {
		fmt.Printf("  %d) %s (%s)\n", item.ID, item.Name, item.Kind)
	}
	fmt.Println("For each event: enter a work item ID, `r <id>` to also make a rule, or Enter to skip. `q` quits.")
	reader := bufio.NewReader(os.Stdin)

	for _, e := range events {
		fmt.Printf("\n[%d] app=%q title=%q", e.ID, e.App, e.Title)
		if e.Repo != "" {
			fmt.Printf(" repo=%q", e.Repo)
		}
		if e.URL != "" {
			fmt.Printf(" url=%q", e.URL)
		}
		fmt.Printf(" (%.0f min)\n> ", e.Duration/60)

		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "q" {
			break
		}
		makeRule := false
		if strings.HasPrefix(line, "r ") {
			makeRule = true
			line = strings.TrimSpace(line[2:])
		}
		if _, _, err := app.AssignEvent(e.ID, line, makeRule, model.FieldTitle); err != nil {
			fmt.Printf("  ! %v\n", err)
			continue
		}
		if makeRule {
			fmt.Println("  assigned + rule created")
		} else {
			fmt.Println("  assigned")
		}
	}
	return nil
}
