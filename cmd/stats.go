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

var statsWeeks int

var statsCmd = &cobra.Command{
	Use:     "stats",
	Aliases: []string{"usage"},
	Short:   "Count in-progress issues and chart weekly closed issues",
	RunE:    runStats,
}

func init() {
	statsCmd.Flags().IntVar(&statsWeeks, "weeks", 0, "weeks of closed-issue history to chart (default from config: 8)")
	statsCmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of human-readable output")
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, _ []string) error {
	cfg, client, err := loadConfigAndClient()
	if err != nil {
		return err
	}
	weeks := statsWeeks
	if weeks <= 0 {
		weeks = cfg.Weeks
	}
	st, err := jira.ComputeStats(context.Background(), client, weeks)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	}
	renderStats(cmd.OutOrStdout(), st, time.Now())
	return nil
}

// barWidth is the maximum width (in blocks) of a chart bar.
const barWidth = 30

func renderStats(out io.Writer, st jira.Stats, now time.Time) {
	plain := ui.IsPlain(out)
	fmt.Fprintf(out, "%s\n\n", ui.Title(plain, "jirawk · usage"))
	fmt.Fprintf(out, "  in progress : %d\n\n", st.InProgress)

	fmt.Fprintf(out, "  closed — last %d weeks (%d total)\n", len(st.Weeks), st.DoneTotal)
	if st.DoneTotal == 0 {
		fmt.Fprintln(out, ui.Dim(plain, "    (nothing closed in this window)"))
		return
	}
	peak := 0
	for _, n := range st.Weeks {
		if n > peak {
			peak = n
		}
	}
	// Print oldest → newest so the most recent week sits at the bottom.
	for i := len(st.Weeks) - 1; i >= 0; i-- {
		n := st.Weeks[i]
		label := jira.WeekLabel(now, i)
		bar := renderBar(n, peak)
		marker := "  "
		if i == 0 {
			marker = "▸ " // current week
		}
		fmt.Fprintf(out, "  %s%-15s %s %d\n", marker, label, ui.Bar(plain, bar), n)
	}
}

func renderBar(n, peak int) string {
	if peak <= 0 || n <= 0 {
		return ""
	}
	w := max(n*barWidth/peak, 1)
	return strings.Repeat("█", w)
}
