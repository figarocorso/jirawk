// Package tui hosts the Bubble Tea TUI for jirawk.
package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/figarocorso/jirawk/internal/config"
	"github.com/figarocorso/jirawk/internal/jira"
)

// Run boots the TUI. A positive interval enables `watch`-style auto-refresh.
func Run(cfg *config.Config, client jira.Client, interval time.Duration) error {
	m := New(cfg, client)
	m.SetRefreshInterval(interval)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// Per-section fetch messages. Each section loads independently so its tab can
// render the moment its data arrives, rather than blocking on the slowest query.
type inProgressMsg struct {
	issues []jira.Issue
	err    error
}
type openMsg struct {
	issues []jira.Issue
	err    error
}
type doneMsg struct {
	issues []jira.Issue
	err    error
}

// epicsMsg carries the ancestor epics/initiatives of the in-progress issues. Its
// fetch is chained after inProgressMsg lands, since it walks their parent chain.
type epicsMsg struct {
	issues []jira.Issue
	err    error
}

// usageMsg carries the computed stats for the usage overlay.
type usageMsg struct {
	stats jira.Stats
	err   error
}

// transitionMsg reports the result of moving an issue to a new state.
type transitionMsg struct {
	key string
	err error
}

// transitionCmd moves an issue to the first reachable state, trying each
// candidate in order (e.g. "Done", then "Resolved") until one succeeds.
func transitionCmd(client jira.Client, key string, states ...string) tea.Cmd {
	return func() tea.Msg {
		var err error
		for _, state := range states {
			if err = client.Transition(context.Background(), key, state); err == nil {
				return transitionMsg{key: key, err: nil}
			}
		}
		return transitionMsg{key: key, err: err}
	}
}

// fetchAllCmd starts a refresh by fetching the first tab's section. The
// remaining sections are chained in tab order (in progress → epics → open →
// closed) by each section's handler, so they load and render in that order
// rather than racing in parallel.
func fetchAllCmd(_ *config.Config, client jira.Client) tea.Cmd {
	return inProgressCmd(client)
}

func inProgressCmd(client jira.Client) tea.Cmd {
	return func() tea.Msg {
		i, err := client.InProgress(context.Background())
		return inProgressMsg{issues: i, err: err}
	}
}

func openCmd(client jira.Client) tea.Cmd {
	return func() tea.Msg {
		i, err := client.Open(context.Background())
		return openMsg{issues: i, err: err}
	}
}

func doneCmd(cfg *config.Config, client jira.Client) tea.Cmd {
	return func() tea.Msg {
		i, err := client.RecentlyDone(context.Background(), cfg.DoneWindow)
		return doneMsg{issues: i, err: err}
	}
}

// epicsCmd walks the parent chain of the in-progress seeds up to initiatives
// (2 levels: story → epic → initiative).
func epicsCmd(client jira.Client, seeds []jira.Issue) tea.Cmd {
	return func() tea.Msg {
		i, err := client.Ancestors(context.Background(), seeds, epicAncestorDepth)
		return epicsMsg{issues: i, err: err}
	}
}

// usageCmd computes the weekly stats for the overlay.
func usageCmd(cfg *config.Config, client jira.Client) tea.Cmd {
	return func() tea.Msg {
		st, err := jira.ComputeStats(context.Background(), client, cfg.Weeks)
		return usageMsg{stats: st, err: err}
	}
}
