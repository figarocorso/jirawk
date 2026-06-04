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

// fetchMsg is delivered when a background fetch of all sections completes.
type fetchMsg struct {
	inProgress []jira.Issue
	open       []jira.Issue
	done       []jira.Issue
	err        error
}

// usageMsg carries the computed stats for the usage overlay.
type usageMsg struct {
	stats jira.Stats
	err   error
}

// fetchCmd fetches all sections concurrently.
func fetchCmd(cfg *config.Config, client jira.Client) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		type res struct {
			issues []jira.Issue
			err    error
		}
		ipCh := make(chan res, 1)
		openCh := make(chan res, 1)
		doneCh := make(chan res, 1)
		go func() {
			i, err := client.InProgress(ctx)
			ipCh <- res{i, err}
		}()
		go func() {
			i, err := client.Open(ctx)
			openCh <- res{i, err}
		}()
		go func() {
			i, err := client.RecentlyDone(ctx, cfg.DoneWindow)
			doneCh <- res{i, err}
		}()
		ip := <-ipCh
		op := <-openCh
		dn := <-doneCh
		msg := fetchMsg{inProgress: ip.issues, open: op.issues, done: dn.issues}
		switch {
		case ip.err != nil:
			msg.err = ip.err
		case op.err != nil:
			msg.err = op.err
		case dn.err != nil:
			msg.err = dn.err
		}
		return msg
	}
}

// usageCmd computes the weekly stats for the overlay.
func usageCmd(cfg *config.Config, client jira.Client) tea.Cmd {
	return func() tea.Msg {
		st, err := jira.ComputeStats(context.Background(), client, cfg.Weeks)
		return usageMsg{stats: st, err: err}
	}
}
