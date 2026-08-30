package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show capture connectivity and tracking totals",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, closeFn, err := openApp()
			if err != nil {
				return err
			}
			defer closeFn()

			st, err := app.Status(cmd.Context())
			if err != nil {
				return err
			}
			if jsonOut {
				return emitJSON(st)
			}
			conn := "no capture provider connected"
			if st.ProviderConnected {
				conn = st.Provider + " connected"
			}
			fmt.Printf("Capture:       %s\n", conn)
			fmt.Printf("Work items:    %d active, %d done\n", st.ActiveWorkItems, st.DoneWorkItems)
			fmt.Printf("Active rules:  %d\n", st.ActiveRules)
			fmt.Printf("Events:        %d captured\n", st.EventsTotal)
			fmt.Printf("Today:         %.1f h tracked (%.1f h unclassified)\n", st.TrackedToday, st.UnclassifiedToday)
			fmt.Printf("This week:     %.1f h tracked\n", st.TrackedWeek)
			fmt.Printf("LLM fallback:  %v\n", st.LLMEnabled)
			return nil
		},
	}
}
