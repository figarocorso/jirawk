package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

// SetVersionInfo is called from main with ldflags-injected values.
func SetVersionInfo(version, commit, date string) {
	buildVersion = version
	buildCommit = commit
	buildDate = date
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "jirawk %s\ncommit:  %s\nbuilt:   %s\n", buildVersion, buildCommit, buildDate)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
