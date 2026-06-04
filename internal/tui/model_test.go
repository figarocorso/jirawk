package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/figarocorso/jirawk/internal/config"
	"github.com/figarocorso/jirawk/internal/jira"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testModel() *Model {
	cfg := &config.Config{DoneWindow: 24 * time.Hour, Weeks: 4}
	return New(cfg, jira.NewMockClient())
}

func TestModelRendersSectionsAfterFetch(t *testing.T) {
	m := testModel()
	// size the screen so tables lay out
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(*Model)

	now := time.Now()
	msg := fetchMsg{
		inProgress: []jira.Issue{{Key: "OP-1", Summary: "fix", Status: "In Progress", Updated: now}},
		done:       []jira.Issue{{Key: "OP-2", Summary: "shipped", Status: "Done", Updated: now}},
	}
	updated, _ = m.Update(msg)
	m = updated.(*Model)

	view := m.View()
	assert.Contains(t, view, "In progress (1)")
	assert.Contains(t, view, "OP-1")
	assert.Contains(t, view, "Closed")
	assert.Contains(t, view, "OP-2")
	assert.False(t, m.loading)
}

func TestModelTabSwitchesFocus(t *testing.T) {
	m := testModel()
	require.Equal(t, secInProgress, m.focus)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(*Model)
	assert.Equal(t, secDone, m.focus)
}

func TestModelUsageOverlay(t *testing.T) {
	m := testModel()
	m.handleUsage(usageMsg{stats: jira.Stats{InProgress: 3, Weeks: []int{2, 1}, DoneTotal: 3}})
	assert.Contains(t, m.View(), "jirawk usage")
	assert.Contains(t, m.View(), "in progress : 3")

	// esc closes the overlay.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	assert.Empty(t, m.overlay)
}

func TestModelFetchError(t *testing.T) {
	m := testModel()
	m.handleFetch(fetchMsg{err: assertErr{}})
	assert.Contains(t, m.View(), "boom")
}

type assertErr struct{}

func (assertErr) Error() string { return "boom" }
