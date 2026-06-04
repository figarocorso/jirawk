package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/figarocorso/jirawk/internal/jira"
	"github.com/figarocorso/jirawk/internal/ui"
	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get KEY",
	Short: "Fetch a single issue (use --json for agent-friendly output)",
	Args:  cobra.ExactArgs(1),
	RunE:  runGet,
}

func init() {
	getCmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of human-readable output")
	rootCmd.AddCommand(getCmd)
}

func runGet(cmd *cobra.Command, args []string) error {
	_, client, err := loadConfigAndClient()
	if err != nil {
		return err
	}
	issue, err := client.Get(context.Background(), strings.ToUpper(strings.TrimSpace(args[0])))
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(toJSONIssue(issue))
	}
	out := cmd.OutOrStdout()
	plain := ui.IsPlain(out)
	fmt.Fprintf(out, "%s %s\n", ui.Title(plain, issue.Key), issue.Summary)
	label := func(s string) string { return ui.Dim(plain, s) }
	fmt.Fprintf(out, "  %s %s\n", label("Status:   "), ui.StatusBadge(plain, issue.Status))
	fmt.Fprintf(out, "  %s %s\n", label("Type:     "), valueOrDash(issue.Type))
	fmt.Fprintf(out, "  %s %s\n", label("Priority: "), jira.PriorityLabel(issue))
	fmt.Fprintf(out, "  %s %s\n", label("Assignee: "), jira.AssigneeLabel(issue))
	fmt.Fprintf(out, "  %s %s\n", label("Updated:  "), jira.AgeLabel(issue.Updated))
	if len(issue.Labels) > 0 {
		fmt.Fprintf(out, "  %s %s\n", label("Labels:   "), strings.Join(issue.Labels, ", "))
	}
	fmt.Fprintf(out, "  %s %s\n", label("URL:      "), ui.Dim(plain, issue.URL))
	return nil
}

func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
