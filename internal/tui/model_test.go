package tui

import (
	"fmt"
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
		open:       []jira.Issue{{Key: "OP-3", Summary: "later", Status: "To Do", Created: now}},
		done:       []jira.Issue{{Key: "OP-2", Summary: "shipped", Status: "Done", Updated: now}},
	}
	updated, _ = m.Update(msg)
	m = updated.(*Model)

	view := m.View()
	// active tab is in-progress; its rows render, tab bar shows all three counts
	assert.Contains(t, view, "In progress (1)")
	assert.Contains(t, view, "Open (1)")
	assert.Contains(t, view, "Closed")
	assert.Contains(t, view, "OP-1")
	assert.False(t, m.loading)
}

func TestModelSortInProgressOldestFirst(t *testing.T) {
	m := testModel()
	now := time.Now()
	m.handleFetch(fetchMsg{inProgress: []jira.Issue{
		{Key: "NEW", Status: "In Progress", Updated: now.Add(-time.Hour)},
		{Key: "OLD", Status: "In Progress", Updated: now.Add(-100 * time.Hour)},
	}})
	rows := m.tabs[tabInProgress].rows
	require.Len(t, rows, 2)
	assert.Equal(t, "OLD", rows[0].Key, "oldest (most stale) should sort first")
}

func TestModelTabSwitch(t *testing.T) {
	m := testModel()
	require.Equal(t, tabInProgress, m.active)
	updated, _, _ := m.handleKey("right")
	m = updated.(*Model)
	assert.Equal(t, tabOpen, m.active)
	updated, _, _ = m.handleKey("l")
	m = updated.(*Model)
	assert.Equal(t, tabClosed, m.active)
	// wraps around
	updated, _, _ = m.handleKey("right")
	m = updated.(*Model)
	assert.Equal(t, tabInProgress, m.active)
	// vim left
	updated, _, _ = m.handleKey("h")
	m = updated.(*Model)
	assert.Equal(t, tabClosed, m.active)
}

func TestModelScrollIndicator(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m = updated.(*Model)
	rows := make([]jira.Issue, 20)
	for i := range rows {
		rows[i] = jira.Issue{Key: fmt.Sprintf("OP-%d", i), Status: "In Progress"}
	}
	m.handleFetch(fetchMsg{inProgress: rows})
	// cursor at top: only "below" indicator
	assert.Contains(t, m.renderScroll(), "more below")
	assert.NotContains(t, m.renderScroll(), "more above")
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
	view := m.View()
	assert.Contains(t, view, "boom")
	assert.Contains(t, view, "jirawk check")
}

func TestModelPreservesCursorOnRefresh(t *testing.T) {
	m := testModel()
	rows := []jira.Issue{
		{Key: "OP-1", Status: "In Progress"},
		{Key: "OP-2", Status: "In Progress"},
		{Key: "OP-3", Status: "In Progress"},
	}
	m.handleFetch(fetchMsg{inProgress: rows})
	m.tabs[tabInProgress].table.SetCursor(1) // select OP-2

	// Refresh with the list reordered; OP-2 is now last.
	m.handleFetch(fetchMsg{inProgress: []jira.Issue{
		{Key: "OP-3", Status: "In Progress"},
		{Key: "OP-1", Status: "In Progress"},
		{Key: "OP-2", Status: "In Progress"},
	}})
	assert.Equal(t, "OP-2", m.selectedKeyOf(tabInProgress), "cursor should follow the selected key")
}

func TestModelFollowsSelectionAcrossTabs(t *testing.T) {
	m := testModel()
	m.handleFetch(fetchMsg{inProgress: []jira.Issue{{Key: "OP-1", Status: "In Progress"}}})
	require.Equal(t, tabInProgress, m.active)
	require.Equal(t, "OP-1", m.selectedKeyOf(tabInProgress))

	// OP-1 gets resolved → moves to the closed tab. Active view follows it.
	m.handleFetch(fetchMsg{done: []jira.Issue{{Key: "OP-1", Status: "Done"}}})
	assert.Equal(t, tabClosed, m.active)
	assert.Equal(t, "OP-1", m.selectedKeyOf(tabClosed))
}

type assertErr struct{}

func (assertErr) Error() string { return "boom" }
