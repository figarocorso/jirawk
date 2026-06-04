package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// watchMinInterval is the smallest auto-refresh cadence we accept. Anything
// lower would hammer Jira faster than the data meaningfully changes.
const watchMinInterval = 5 * time.Second

var watchInterval time.Duration

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Open the TUI and auto-refresh every --interval",
	Long: `watch opens the same interactive TUI as 'jirawk' but periodically
re-fetches your issues (default every 30s). Press q or Ctrl-C to exit.`,
	RunE: runWatch,
}

func init() {
	watchCmd.Flags().DurationVar(&watchInterval, "interval", 30*time.Second, "auto-refresh interval (minimum 5s)")
	rootCmd.AddCommand(watchCmd)
}

func runWatch(_ *cobra.Command, _ []string) error {
	if watchInterval < watchMinInterval {
		return fmt.Errorf("--interval must be at least %s (got %s)", watchMinInterval, watchInterval)
	}
	return runTUIWatch(watchInterval)
}
