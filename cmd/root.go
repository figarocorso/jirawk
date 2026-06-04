package cmd

import (
	"fmt"
	"os"

	"github.com/figarocorso/jirawk/internal/ui"
	"github.com/spf13/cobra"
)

var jsonOut bool

var rootCmd = &cobra.Command{
	Use:   "jirawk",
	Short: "🦅 Keep watch over your Jira issues",
	Long: `jirawk — track the Jira issues you care about.

Run with no arguments to open the interactive TUI: your in-progress issues on
top, issues you closed in the last 24h below. Use subcommands for
non-interactive workflows (list, stats, get, check).`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&ui.Plain, "plain", false, "disable colors/emoji; emit ASCII-only output (also honors NO_COLOR)")
	rootCmd.PersistentFlags().BoolVar(&ui.Plain, "no-color", false, "alias for --plain")
}
