package tui

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/figarocorso/jirawk/internal/config"
	"github.com/figarocorso/jirawk/internal/jira"
	"github.com/muesli/termenv"
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
	m = feed(m, inProgressMsg{issues: []jira.Issue{{Key: "OP-1", Summary: "fix", Status: "In Progress", Updated: now}}})
	m = feed(m, openMsg{issues: []jira.Issue{{Key: "OP-3", Summary: "later", Status: "To Do", Created: now}}})
	m = feed(m, doneMsg{issues: []jira.Issue{{Key: "OP-2", Summary: "shipped", Status: "Done", Updated: now}}})
	m = feed(m, epicsMsg{issues: []jira.Issue{{Key: "OP-9", Summary: "epic", Type: "Epic", Updated: now}}})

	view := m.View()
	// active tab is in-progress; its rows render, tab bar shows every count
	assert.Contains(t, view, "In progress (1)")
	assert.Contains(t, view, "Open (1)")
	assert.Contains(t, view, "Closed")
	assert.Contains(t, view, "Epics (1)")
	assert.Contains(t, view, "OP-1")
	assert.False(t, m.busy(), "all sections loaded → not busy")
}

// feed routes a section message through Update and returns the updated model.
func feed(m *Model, msg tea.Msg) *Model {
	updated, _ := m.Update(msg)
	return updated.(*Model)
}

func TestModelSortInProgressOldestFirst(t *testing.T) {
	m := testModel()
	now := time.Now()
	m.handleInProgress(inProgressMsg{issues: []jira.Issue{
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
	assert.Equal(t, tabEpics, m.active)
	updated, _, _ = m.handleKey("l")
	m = updated.(*Model)
	assert.Equal(t, tabOpen, m.active)
	updated, _, _ = m.handleKey("l")
	m = updated.(*Model)
	assert.Equal(t, tabClosed, m.active)
	// wraps around
	updated, _, _ = m.handleKey("right")
	m = updated.(*Model)
	assert.Equal(t, tabInProgress, m.active)
	// vim left wraps back to the last tab
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
	m.handleInProgress(inProgressMsg{issues: rows})
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
	m.handleInProgress(inProgressMsg{err: assertErr{}})
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
	m.handleInProgress(inProgressMsg{issues: rows})
	m.tabs[tabInProgress].table.SetCursor(1) // select OP-2

	// Refresh with the list reordered; OP-2 is now last.
	m.handleInProgress(inProgressMsg{issues: []jira.Issue{
		{Key: "OP-3", Status: "In Progress"},
		{Key: "OP-1", Status: "In Progress"},
		{Key: "OP-2", Status: "In Progress"},
	}})
	assert.Equal(t, "OP-2", m.selectedKeyOf(tabInProgress), "cursor should follow the selected key")
}

func TestModelFollowsSelectionAcrossTabs(t *testing.T) {
	m := testModel()
	m.handleInProgress(inProgressMsg{issues: []jira.Issue{{Key: "OP-1", Status: "In Progress"}}})
	require.Equal(t, tabInProgress, m.active)
	require.Equal(t, "OP-1", m.selectedKeyOf(tabInProgress))

	// Refresh: OP-1 gets resolved → leaves in-progress, lands in closed. The
	// active view should follow it across the independently-arriving sections.
	m.markAllLoading()
	m.handleInProgress(inProgressMsg{issues: nil})
	m.handleDone(doneMsg{issues: []jira.Issue{{Key: "OP-1", Status: "Done"}}})
	assert.Equal(t, tabClosed, m.active)
	assert.Equal(t, "OP-1", m.selectedKeyOf(tabClosed))
}

func TestModelEpicsChainAfterInProgress(t *testing.T) {
	mock := jira.NewMockClient()
	mock.EpicIssues = []jira.Issue{{Key: "EPIC-1", Type: "Epic", Parent: ""}}
	cfg := &config.Config{DoneWindow: 24 * time.Hour, Weeks: 4}
	m := New(cfg, mock)

	// In-progress lands first; its tab is ready while epics is still loading.
	cmd := m.handleInProgress(inProgressMsg{issues: []jira.Issue{{Key: "OP-1", Parent: "EPIC-1", Status: "In Progress"}}})
	assert.False(t, m.tabs[tabInProgress].loading, "in-progress ready immediately")
	assert.True(t, m.tabs[tabEpics].loading, "epics still loading until its fetch returns")
	require.NotNil(t, cmd, "in-progress handler chains the epics fetch")

	// Running the chained command yields the epics for that in-progress work.
	msg := cmd()
	em, ok := msg.(epicsMsg)
	require.True(t, ok, "chained command produces an epicsMsg")
	require.NoError(t, em.err)

	m = feed(m, em)
	assert.False(t, m.tabs[tabEpics].loading)
	assert.Equal(t, "EPIC-1", m.selectedKeyOf(tabEpics))
	assert.Contains(t, m.View(), "Epics (1)")
}

func TestModelEpicsRemovedFromInProgress(t *testing.T) {
	// OP-288 is in progress AND an ancestor of OP-294; it must end up only in the
	// Epics tab, not duplicated in In progress.
	mock := jira.NewMockClient()
	mock.EpicIssues = []jira.Issue{{Key: "OP-288", Type: "Initiative"}}
	cfg := &config.Config{DoneWindow: 24 * time.Hour, Weeks: 4}
	m := New(cfg, mock)

	cmd := m.handleInProgress(inProgressMsg{issues: []jira.Issue{
		{Key: "OP-294", Parent: "OP-288", Status: "In Progress"},
		{Key: "OP-288", Type: "Initiative", Status: "In Progress"},
	}})
	require.NotNil(t, cmd)
	// Before epics land, the epic still shows in In progress.
	require.Len(t, m.tabs[tabInProgress].rows, 2)

	em, ok := cmd().(epicsMsg)
	require.True(t, ok)
	m = feed(m, em)

	ipKeys := keySet(m.tabs[tabInProgress].rows)
	assert.False(t, ipKeys["OP-288"], "epic must leave In progress")
	assert.True(t, ipKeys["OP-294"], "non-epic child stays in In progress")
	assert.True(t, keySet(m.tabs[tabEpics].rows)["OP-288"], "epic shows in Epics")
}

func TestModelPaletteUsageCommand(t *testing.T) {
	m := testModel()
	// "/" opens the palette.
	updated, _, handled := m.handleKey("/")
	m = updated.(*Model)
	require.True(t, handled)
	require.True(t, m.palette)

	// Type "usage" then Enter runs the usage command.
	for _, r := range "usage" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(*Model)
	}
	assert.Equal(t, "usage", m.paletteInput)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	assert.False(t, m.palette)
	require.NotNil(t, cmd)

	// The command computes stats; feed the result back to render the overlay.
	m.handleUsage(usageMsg{stats: jira.Stats{InProgress: 2, Weeks: []int{1}, DoneTotal: 1}})
	assert.Contains(t, m.View(), "jirawk usage")
}

func TestModelPaletteEscCancels(t *testing.T) {
	m := testModel()
	updated, _, _ := m.handleKey("/")
	m = updated.(*Model)
	require.True(t, m.palette)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	assert.False(t, m.palette)
}

func TestModelMoveToDoneConfirmed(t *testing.T) {
	mock := jira.NewMockClient()
	cfg := &config.Config{DoneWindow: 24 * time.Hour, Weeks: 4}
	m := New(cfg, mock)
	m.handleInProgress(inProgressMsg{issues: []jira.Issue{{Key: "OP-1", Status: "In Progress"}}})

	// d prompts; view shows the confirmation.
	updated, _, handled := m.handleKey("d")
	m = updated.(*Model)
	require.True(t, handled)
	require.True(t, m.confirmDone)
	assert.Contains(t, m.View(), "Move OP-1 to Done?")

	// "y" confirms and fires the transition command.
	updated, cmd, _ := m.handleKey("y")
	m = updated.(*Model)
	assert.False(t, m.confirmDone)
	require.NotNil(t, cmd)
	msg := cmd()
	// Batch returns a slice of messages; find the transitionMsg.
	tmsg := extractTransition(t, msg)
	assert.Equal(t, "OP-1", tmsg.key)
	assert.Nil(t, tmsg.err)
	require.Len(t, mock.Transitions, 1)
	assert.Equal(t, [2]string{"OP-1", "Done"}, mock.Transitions[0])
}

func TestModelMoveToDoneCancelled(t *testing.T) {
	m := testModel()
	m.handleInProgress(inProgressMsg{issues: []jira.Issue{{Key: "OP-1", Status: "In Progress"}}})
	updated, _, _ := m.handleKey("d")
	m = updated.(*Model)
	require.True(t, m.confirmDone)
	updated, _, _ = m.handleKey("n")
	m = updated.(*Model)
	assert.False(t, m.confirmDone)
}

func TestModelMoveToDoneOnlyOnInProgressTab(t *testing.T) {
	m := testModel()
	m.gotoTab(tabOpen)
	updated, _, _ := m.handleKey("d")
	m = updated.(*Model)
	assert.False(t, m.confirmDone, "Done move should be a no-op outside the In progress tab")
}

// extractTransition pulls the transitionMsg out of a (possibly batched) tea.Msg.
func extractTransition(t *testing.T, msg tea.Msg) transitionMsg {
	t.Helper()
	switch v := msg.(type) {
	case transitionMsg:
		return v
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if tm, ok := extractTransitionFrom(c()); ok {
				return tm
			}
		}
	}
	t.Fatalf("no transitionMsg in %#v", msg)
	return transitionMsg{}
}

func extractTransitionFrom(msg tea.Msg) (transitionMsg, bool) {
	if tm, ok := msg.(transitionMsg); ok {
		return tm, true
	}
	return transitionMsg{}, false
}

func TestModelSortArrowsPerTab(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(*Model)
	assert.Equal(t, "Age ▲", m.tabs[tabInProgress].ageDesc, "in progress: oldest first")
	assert.Equal(t, "Age ▼", m.tabs[tabOpen].ageDesc, "open: newest first")
	assert.Equal(t, "Age ▼", m.tabs[tabClosed].ageDesc, "closed: newest first")
	assert.Contains(t, m.View(), "Age ▲", "active tab header carries its arrow")
}

func TestAgeColorGradients(t *testing.T) {
	now := time.Now()
	fresh := jira.Issue{Updated: now.Add(-2 * time.Hour), Created: now.Add(-2 * time.Hour)}
	stale := jira.Issue{Updated: now.Add(-30 * 24 * time.Hour), Created: now.Add(-30 * 24 * time.Hour)}

	// Each tab maps the same ages to different colours (degrades differently).
	assert.NotEqual(t, ageColor(tabInProgress, fresh, now), ageColor(tabInProgress, stale, now))
	assert.NotEqual(t, ageColor(tabOpen, fresh, now), ageColor(tabClosed, fresh, now))
	// Zero timestamp falls back to a neutral grey.
	assert.Equal(t, lipgloss.Color("245"), ageColor(tabInProgress, jira.Issue{}, now))
}

func TestRenderColoredTableEmbedsAnsiAndStaysIntact(t *testing.T) {
	// Tests run without a TTY (Ascii profile strips colour); force a profile so
	// the ANSI is actually emitted and we can prove it doesn't corrupt the text.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m := testModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(*Model)
	now := time.Now()
	m.handleInProgress(inProgressMsg{issues: []jira.Issue{
		{Key: "OP-1", Summary: "fix", Status: "In Progress", Updated: now.Add(-2 * time.Hour)},
	}})
	out := m.renderColoredTable(tabInProgress)
	assert.Contains(t, out, "\x1b[", "rows should carry ANSI colour")
	// The key text must survive intact (no mid-escape truncation corruption).
	assert.Contains(t, out, "OP-1")
}

func TestModelScrollWindowFollowsCursor(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m = updated.(*Model)
	rows := make([]jira.Issue, 50)
	for i := range rows {
		rows[i] = jira.Issue{Key: fmt.Sprintf("OP-%d", i), Status: "In Progress"}
	}
	m.handleInProgress(inProgressMsg{issues: rows})
	m.tabs[tabInProgress].table.SetCursor(40)
	m.reconcileScroll(tabInProgress)
	off := m.tabs[tabInProgress].offset
	h := m.tabs[tabInProgress].table.Height()
	assert.GreaterOrEqual(t, 40, off)
	assert.Less(t, 40, off+h, "cursor must be inside the visible window")
}

type assertErr struct{}

func (assertErr) Error() string { return "boom" }
