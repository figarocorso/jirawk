package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/figarocorso/jirawk/internal/jira"
	"github.com/figarocorso/jirawk/internal/ui"
	"github.com/spf13/cobra"
)

var listSection string

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Print your in-progress and recently-closed issues as a table",
	RunE:  runList,
}

func init() {
	listCmd.Flags().StringVar(&listSection, "section", "all", "which section to show: in-progress | done | all")
	listCmd.Flags().BoolVar(&jsonOut, "json", false, "emit a JSON object instead of a table")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, _ []string) error {
	cfg, client, err := loadConfigAndClient()
	if err != nil {
		return err
	}
	ctx := context.Background()

	var inProgress, done []jira.Issue
	section := strings.ToLower(listSection)
	if section == "in-progress" || section == "all" {
		inProgress, err = client.InProgress(ctx)
		if err != nil {
			return err
		}
	}
	if section == "done" || section == "all" {
		done, err = client.RecentlyDone(ctx, cfg.DoneWindow)
		if err != nil {
			return err
		}
	}

	if jsonOut {
		return emitListJSON(cmd.OutOrStdout(), inProgress, done)
	}

	out := cmd.OutOrStdout()
	plain := ui.IsPlain(out)
	if section == "in-progress" || section == "all" {
		renderSection(out, plain, "in progress", inProgress)
	}
	if section == "all" {
		fmt.Fprintln(out)
	}
	if section == "done" || section == "all" {
		renderSection(out, plain, fmt.Sprintf("closed · last %s", humanDuration(cfg.DoneWindow)), done)
	}
	return nil
}

var listHeaders = []string{"KEY", "Status", "Priority", "Age", "Summary"}

func renderSection(out io.Writer, plain bool, title string, issues []jira.Issue) {
	fmt.Fprintf(out, "%s %s\n", ui.Title(plain, title), ui.Dim(plain, fmt.Sprintf("(%d)", len(issues))))
	if len(issues) == 0 {
		if plain {
			fmt.Fprintln(out, "  none")
		} else {
			fmt.Fprintln(out, ui.Dim(false, "  📭 none"))
		}
		return
	}
	rows := make([][]string, 0, len(issues))
	for _, i := range issues {
		rows = append(rows, issueRow(i))
	}
	renderTable(out, listHeaders, rows, plain, func(col int, raw string) string {
		if col == 1 {
			return ui.StatusBadge(plain, raw)
		}
		if col == 0 {
			return ui.Dim(plain, raw)
		}
		return raw
	})
}

func issueRow(i jira.Issue) []string {
	return []string{
		i.Key,
		jira.StatusLabel(i),
		jira.PriorityLabel(i),
		jira.AgeLabel(i.Updated),
		i.Summary,
	}
}

type jsonIssue struct {
	Key      string   `json:"key"`
	Summary  string   `json:"summary,omitempty"`
	Status   string   `json:"status,omitempty"`
	Category string   `json:"category,omitempty"`
	Type     string   `json:"type,omitempty"`
	Priority string   `json:"priority,omitempty"`
	Assignee string   `json:"assignee,omitempty"`
	Labels   []string `json:"labels,omitempty"`
	URL      string   `json:"url,omitempty"`
	Updated  string   `json:"updated,omitempty"`
}

func toJSONIssue(i jira.Issue) jsonIssue {
	category := i.Category
	if category == "" {
		category = jira.DeriveCategory(i.Status)
	}
	j := jsonIssue{
		Key: i.Key, Summary: i.Summary, Status: i.Status, Category: category,
		Type: i.Type, Priority: i.Priority, Assignee: i.Assignee, Labels: i.Labels, URL: i.URL,
	}
	if !i.Updated.IsZero() {
		j.Updated = i.Updated.Format("2006-01-02T15:04:05Z07:00")
	}
	return j
}

func emitListJSON(out io.Writer, inProgress, done []jira.Issue) error {
	payload := map[string][]jsonIssue{
		"in_progress": mapIssues(inProgress),
		"done":        mapIssues(done),
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func mapIssues(in []jira.Issue) []jsonIssue {
	out := make([]jsonIssue, 0, len(in))
	for _, i := range in {
		out = append(out, toJSONIssue(i))
	}
	return out
}

func humanDuration(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		days := int(d.Hours()) / 24
		if days == 1 {
			return "24h"
		}
		return fmt.Sprintf("%dd", days)
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return d.String()
}
