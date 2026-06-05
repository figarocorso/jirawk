package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/figarocorso/jirawk/internal/config"
	"github.com/figarocorso/jirawk/internal/jira"
	"github.com/mattn/go-runewidth"
)

type autoRefreshTickMsg time.Time

var (
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	keyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	hintStyle    = lipgloss.NewStyle().Faint(true)
	barStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	scrollStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	tabActive    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("212")).Padding(0, 1)
	tabInactive  = lipgloss.NewStyle().Foreground(lipgloss.Color("251")).Background(lipgloss.Color("237")).Padding(0, 1)
	tabSeparator = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Shared table styles, used both to seed the bubbles table (cursor/layout
	// state) and by renderColoredTable, which draws the rows itself so it can
	// colorize each row by age without bubbles' runewidth.Truncate corrupting
	// embedded ANSI (the reason age-coloring was previously deferred).
	tblHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Padding(0, 1).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("212")).
			BorderTop(false).BorderLeft(false).BorderRight(false).BorderBottom(true)
	tblCellStyle = lipgloss.NewStyle().Padding(0, 1)
	// Selected row: subtle bg tint + bold, keeping the age-coloured foreground.
	// Deliberately avoids green so it never collides with the age gradient.
	tblSelectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("237")).Bold(true)
	// selMarkerStyle draws the left "▶" cursor marker on the selected row.
	selMarkerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Background(lipgloss.Color("237")).Bold(true)
)

// statusEmojiLabel returns an emoji-prefixed status name (no ANSI, so the
// bubbles table can truncate it correctly). The emoji is chosen by deriving the
// status category from the name.
func statusEmojiLabel(status string) string {
	if status == "" {
		status = "unknown"
	}
	var emoji string
	switch jira.DeriveCategory(status) {
	case "in review":
		emoji = "👀"
	case "done":
		emoji = "🟣"
	case "to do":
		emoji = "⚪"
	case "blocked":
		emoji = "⛔"
	case "unknown":
		emoji = "❓"
	default:
		emoji = "🔵"
	}
	return emoji + " " + status
}

// tabKind enumerates the views.
type tabKind int

const (
	tabInProgress tabKind = iota
	tabEpics
	tabOpen
	tabClosed
	numTabs
)

// epicAncestorDepth is how far the Epics tab walks up the parent chain:
// 2 levels covers story → epic → initiative.
const epicAncestorDepth = 2

// tab holds one view's table, its rows, and presentation metadata.
type tab struct {
	kind    tabKind
	title   string
	ageDesc string // header label for the Age column (carries the sort arrow)
	table   table.Model
	rows    []jira.Issue
	offset  int  // first visible row index for the custom colored renderer
	loading bool // section fetch in flight; drives the per-tab spinner/empty state
}

// Model is the Bubble Tea state for jirawk's tabbed TUI.
type Model struct {
	cfg     *config.Config
	client  jira.Client
	tabs    [numTabs]tab
	active  tabKind
	spinner spinner.Model
	loading bool
	status  string
	err     string
	width   int
	height  int

	refreshInterval time.Duration
	overlay         string

	// palette is the prowl-style slash-command line (e.g. "/usage").
	palette      bool
	paletteInput string

	// confirmDone gates the in-progress → Done transition behind a y/N prompt.
	confirmDone bool
	pendingDone jira.Issue

	// followKey is the active tab's selection at the start of a refresh. If that
	// issue migrates to another tab (e.g. resolved → Closed), the active view
	// follows it there once that section's data lands.
	followKey string
}

// doneState is the display name for the Done transition.
const doneState = "Done"

// doneStates are the target status names tried in order when moving an issue to
// Done. Workflows vary: some boards use "Done", others "Resolved".
var doneStates = []string{"Done", "Resolved"}

// SetRefreshInterval enables watch-style auto-refresh. Non-positive disables it.
func (m *Model) SetRefreshInterval(d time.Duration) {
	if d <= 0 {
		m.refreshInterval = 0
		return
	}
	m.refreshInterval = d
}

func (m *Model) autoRefreshCmd() tea.Cmd {
	if m.refreshInterval <= 0 {
		return nil
	}
	return tea.Tick(m.refreshInterval, func(t time.Time) tea.Msg {
		return autoRefreshTickMsg(t)
	})
}

var columnHeaders = []string{"KEY", "Status", "Priority", "Age", "Summary"}

const ageColIdx = 3
const columnPad = 2
const minSummaryWidth = 20

// New builds an unstarted Model.
func New(cfg *config.Config, client jira.Client) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))

	m := &Model{
		cfg:     cfg,
		client:  client,
		spinner: sp,
		status:  "Loading…",
		active:  tabInProgress,
	}
	// In-progress is sorted oldest-first by age (▲ marks the sort column).
	meta := [numTabs]struct {
		kind    tabKind
		title   string
		ageDesc string
	}{
		// Arrow marks the sort direction so each tab shows why its list is
		// ordered: In progress climbs oldest-first (▲); Open and Closed lead
		// with the most recent (▼).
		{tabInProgress, "In progress", "Age ▲"},
		{tabEpics, "Epics", "Age ▼"},
		{tabOpen, "Open", "Age ▼"},
		{tabClosed, fmt.Sprintf("Closed · %s", humanDuration(cfg.DoneWindow)), "Age ▼"},
	}
	for i, md := range meta {
		m.tabs[i] = tab{
			kind:    md.kind,
			title:   md.title,
			ageDesc: md.ageDesc,
			table:   newTable(md.kind == tabInProgress),
			loading: true,
		}
	}
	return m
}

func newTable(focused bool) table.Model {
	t := table.New(
		table.WithColumns(initialColumns("Age")),
		table.WithFocused(focused),
		table.WithHeight(12),
	)
	st := table.DefaultStyles()
	st.Header = tblHeaderStyle
	st.Cell = tblCellStyle
	st.Selected = tblSelectedStyle
	t.SetStyles(st)
	return t
}

func initialColumns(ageTitle string) []table.Column {
	cols := make([]table.Column, len(columnHeaders))
	for i, h := range columnHeaders {
		title := h
		if i == ageColIdx {
			title = ageTitle
		}
		cols[i] = table.Column{Title: title, Width: lipgloss.Width(title) + columnPad}
	}
	return cols
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, fetchAllCmd(m.cfg, m.client)}
	if tick := m.autoRefreshCmd(); tick != nil {
		cmds = append(cmds, tick)
	}
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layoutTables()
	case tea.KeyMsg:
		if m.palette {
			return m.handlePaletteKey(msg)
		}
		if model, cmd, handled := m.handleKey(msg.String()); handled {
			return model, cmd
		}
	case inProgressMsg:
		return m, m.handleInProgress(msg)
	case epicsMsg:
		return m, m.handleEpics(msg)
	case openMsg:
		return m, m.handleOpen(msg)
	case doneMsg:
		m.handleDone(msg)
		return m, nil
	case usageMsg:
		m.handleUsage(msg)
		return m, nil
	case transitionMsg:
		return m, m.handleTransition(msg)
	case autoRefreshTickMsg:
		return m, m.handleAutoRefresh()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.busy() {
			return m, cmd
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.tabs[m.active].table, cmd = m.tabs[m.active].table.Update(msg)
	m.reconcileScroll(m.active)
	return m, cmd
}

func (m *Model) handleKey(key string) (tea.Model, tea.Cmd, bool) {
	if m.confirmDone {
		return m.handleDoneConfirm(key)
	}
	if m.overlay != "" {
		switch key {
		case "esc", "q", "ctrl+c":
			m.overlay = ""
			m.status = "Overlay closed"
			return m, nil, true
		}
		return m, nil, true
	}
	switch key {
	case "/", ":":
		return m.openPalette(), nil, true
	case "q", "ctrl+c", "esc":
		return m, tea.Quit, true
	case "right", "l", "tab":
		return m.switchTab(1), nil, true
	case "left", "h", "shift+tab":
		return m.switchTab(-1), nil, true
	case "1":
		return m.gotoTab(tabInProgress), nil, true
	case "2":
		return m.gotoTab(tabEpics), nil, true
	case "3":
		return m.gotoTab(tabOpen), nil, true
	case "4":
		return m.gotoTab(tabClosed), nil, true
	case "r", "ctrl+r":
		return m.refresh()
	case "enter":
		return m, m.primaryAction(), true
	case "d":
		return m.promptDone()
	case "c":
		m.copySelected()
		return m, nil, true
	}
	return m, nil, false
}

// promptDone opens the y/N confirmation to move the selected in-progress issue
// to Done. It is a no-op outside the In progress tab.
func (m *Model) promptDone() (tea.Model, tea.Cmd, bool) {
	if m.active != tabInProgress && m.active != tabOpen {
		m.status = "Move to Done only available on the In progress and Open tabs"
		return m, nil, true
	}
	cur := &m.tabs[m.active]
	cursor := cur.table.Cursor()
	if cursor < 0 || cursor >= len(cur.rows) {
		return m, nil, true
	}
	m.confirmDone = true
	m.pendingDone = cur.rows[cursor]
	m.err = ""
	m.status = "Confirm move to Done"
	return m, nil, true
}

// handleDoneConfirm routes keys while the Done confirmation prompt is up.
func (m *Model) handleDoneConfirm(key string) (tea.Model, tea.Cmd, bool) {
	switch key {
	case "y", "Y", "enter":
		issue := m.pendingDone
		m.confirmDone = false
		m.pendingDone = jira.Issue{}
		m.loading = true
		m.status = fmt.Sprintf("Moving %s to %s…", issue.Key, doneState)
		return m, tea.Batch(m.spinner.Tick, transitionCmd(m.client, issue.Key, doneStates...)), true
	case "n", "N", "esc", "q", "ctrl+c":
		m.confirmDone = false
		m.pendingDone = jira.Issue{}
		m.status = "Move to Done cancelled"
		return m, nil, true
	}
	return m, nil, true
}

func (m *Model) handleTransition(msg transitionMsg) tea.Cmd {
	if msg.err != nil {
		m.loading = false
		m.err = msg.err.Error()
		m.status = fmt.Sprintf("Failed to move %s", msg.key)
		return nil
	}
	m.err = ""
	m.status = fmt.Sprintf("Moved %s to %s — refreshing…", msg.key, doneState)
	// Clear the transition flag and refetch every section to reflect the move.
	m.loading = false
	m.markAllLoading()
	return tea.Batch(m.spinner.Tick, fetchAllCmd(m.cfg, m.client))
}

// openPalette enters the slash-command line, mirroring prowl's "/usage" UX.
func (m *Model) openPalette() *Model {
	m.palette = true
	m.paletteInput = ""
	m.overlay = ""
	m.err = ""
	m.status = "Command palette — type usage · esc cancels"
	return m
}

// handlePaletteKey edits/runs the slash-command line. Enter runs, esc cancels,
// backspace edits, plain runes append.
func (m *Model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.palette = false
		m.paletteInput = ""
		m.status = "Palette closed"
		return m, nil
	case tea.KeyEnter:
		input := strings.TrimSpace(m.paletteInput)
		m.palette = false
		m.paletteInput = ""
		return m.runPaletteCommand(input)
	case tea.KeyBackspace:
		if r := []rune(m.paletteInput); len(r) > 0 {
			m.paletteInput = string(r[:len(r)-1])
		}
		return m, nil
	case tea.KeySpace:
		m.paletteInput += " "
		return m, nil
	case tea.KeyRunes:
		m.paletteInput += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

// runPaletteCommand dispatches a palette command. A leading slash is tolerated.
func (m *Model) runPaletteCommand(input string) (tea.Model, tea.Cmd) {
	input = strings.TrimPrefix(input, "/")
	if input == "" {
		m.status = "Empty command"
		return m, nil
	}
	cmd := strings.ToLower(strings.Fields(input)[0])
	switch cmd {
	case "usage", "stats":
		m.status = "Computing usage…"
		return m, tea.Batch(m.spinner.Tick, usageCmd(m.cfg, m.client))
	default:
		m.err = "unknown command: " + cmd
		m.status = "Try: usage"
		return m, nil
	}
}

func (m *Model) switchTab(delta int) *Model {
	next := tabKind((int(m.active) + delta + int(numTabs)) % int(numTabs))
	return m.gotoTab(next)
}

func (m *Model) gotoTab(k tabKind) *Model {
	if k == m.active {
		return m
	}
	m.tabs[m.active].table.Blur()
	m.active = k
	m.tabs[m.active].table.Focus()
	m.reconcileScroll(m.active)
	m.status = m.tabs[m.active].title
	return m
}

func (m *Model) refresh() (tea.Model, tea.Cmd, bool) {
	if m.busy() {
		return m, nil, false
	}
	m.markAllLoading()
	m.status = "Refreshing…"
	return m, tea.Batch(m.spinner.Tick, fetchAllCmd(m.cfg, m.client)), true
}

// markAllLoading flags every tab as fetching ahead of a full refresh and
// records the active selection so it can be followed if it changes tabs.
func (m *Model) markAllLoading() {
	m.followKey = m.selectedKeyOf(m.active)
	for i := range m.tabs {
		m.tabs[i].loading = true
	}
}

func (m *Model) handleAutoRefresh() tea.Cmd {
	next := m.autoRefreshCmd()
	if next == nil {
		return nil
	}
	if m.busy() || m.confirmDone || m.palette {
		return next
	}
	m.markAllLoading()
	m.status = "Auto-refreshing…"
	return tea.Batch(m.spinner.Tick, fetchAllCmd(m.cfg, m.client), next)
}

// busy reports whether any work is in flight: a non-section op (transition,
// usage) or any section still fetching. Drives the header spinner.
func (m *Model) busy() bool {
	if m.loading {
		return true
	}
	for i := range m.tabs {
		if m.tabs[i].loading {
			return true
		}
	}
	return false
}

// handleInProgress installs the in-progress rows and, on success, chains the
// epics fetch that walks their parent chain.
func (m *Model) handleInProgress(msg inProgressMsg) tea.Cmd {
	m.tabs[tabInProgress].loading = false
	if msg.err != nil {
		m.err = msg.err.Error()
		// Keep the chain alive so open/closed still load; epics has no seeds.
		return epicsCmd(m.client, nil)
	}
	m.err = ""
	jira.SortByAgeOldestFirst(msg.issues)
	m.applySection(tabInProgress, msg.issues)
	m.updateLoadStatus()
	// Epics depend on the in-progress seeds; fetch them now that we have them.
	return epicsCmd(m.client, msg.issues)
}

// handleEpics installs the epics rows and chains the open fetch (next tab). An
// in-progress issue can itself be an epic/initiative that parents other
// in-progress work, so it surfaces in both lists; once it shows in Epics we drop
// it from In progress to avoid the duplicate.
func (m *Model) handleEpics(msg epicsMsg) tea.Cmd {
	m.tabs[tabEpics].loading = false
	if msg.err != nil {
		m.err = msg.err.Error()
	} else {
		jira.SortByUpdatedNewestFirst(msg.issues)
		m.applySection(tabEpics, msg.issues)
		m.applySection(tabInProgress, withoutKeys(m.tabs[tabInProgress].rows, keySet(msg.issues)))
	}
	m.updateLoadStatus()
	return openCmd(m.client)
}

// keySet returns the set of issue keys.
func keySet(issues []jira.Issue) map[string]bool {
	s := make(map[string]bool, len(issues))
	for _, i := range issues {
		s[i.Key] = true
	}
	return s
}

// withoutKeys returns the issues whose key is not in exclude.
func withoutKeys(issues []jira.Issue, exclude map[string]bool) []jira.Issue {
	out := make([]jira.Issue, 0, len(issues))
	for _, i := range issues {
		if !exclude[i.Key] {
			out = append(out, i)
		}
	}
	return out
}

// handleOpen installs the open rows and chains the closed fetch (next tab).
func (m *Model) handleOpen(msg openMsg) tea.Cmd {
	m.tabs[tabOpen].loading = false
	if msg.err != nil {
		m.err = msg.err.Error()
	} else {
		jira.SortByCreatedNewestFirst(msg.issues)
		m.applySection(tabOpen, msg.issues)
	}
	m.updateLoadStatus()
	return doneCmd(m.cfg, m.client)
}

// handleDone installs the closed rows; it is the last link in the fetch chain.
func (m *Model) handleDone(msg doneMsg) {
	m.tabs[tabClosed].loading = false
	if msg.err != nil {
		m.err = msg.err.Error()
		return
	}
	jira.SortByUpdatedNewestFirst(msg.issues)
	m.applySection(tabClosed, msg.issues)
	m.updateLoadStatus()
}

// applySection swaps one tab's rows in place, preserving its selection by issue
// key and re-laying out columns (widths are shared across all tabs).
func (m *Model) applySection(kind tabKind, issues []jira.Issue) {
	prevKey := m.selectedKeyOf(kind)
	m.tabs[kind].rows = issues
	m.tabs[kind].table.SetRows(issuesToRows(issues))
	m.layoutTables()
	if idx := indexOfKey(issues, prevKey); idx >= 0 {
		m.tabs[kind].table.SetCursor(idx)
	}
	// If the active selection left its tab and resurfaced here, follow it.
	if m.followKey != "" && kind != m.active &&
		indexOfKey(m.tabs[m.active].rows, m.followKey) < 0 {
		if idx := indexOfKey(issues, m.followKey); idx >= 0 {
			m.tabs[m.active].table.Blur()
			m.active = kind
			m.tabs[m.active].table.Focus()
			m.tabs[m.active].table.SetCursor(idx)
		}
	}
	m.reconcileScroll(kind)
}

// updateLoadStatus summarises loaded sections, marking those still in flight.
func (m *Model) updateLoadStatus() {
	if m.busy() {
		m.status = "Loading…"
		return
	}
	m.status = fmt.Sprintf("%d in progress · %d epics · %d open · %d closed",
		len(m.tabs[tabInProgress].rows), len(m.tabs[tabEpics].rows),
		len(m.tabs[tabOpen].rows), len(m.tabs[tabClosed].rows))
}

func (m *Model) handleUsage(msg usageMsg) {
	m.loading = false
	if msg.err != nil {
		m.err = msg.err.Error()
		return
	}
	m.overlay = formatUsage(msg.stats, time.Now())
	m.status = "Usage overlay — press esc/u to close"
}

func (m *Model) layoutTables() {
	if m.width > 0 {
		m.recomputeColumnWidths()
	}
	// header + tab bar + spacing + footer ≈ 7 lines.
	avail := max(m.height-7, 4)
	for i := range m.tabs {
		m.tabs[i].table.SetHeight(avail)
		m.reconcileScroll(tabKind(i))
	}
}

func (m *Model) recomputeColumnWidths() {
	allRows := make([][]jira.Issue, 0, numTabs)
	for i := range m.tabs {
		allRows = append(allRows, m.tabs[i].rows)
	}
	widths, hasContent := colWidthsFromRows(allRows...)
	finalizeColWidths(widths, hasContent)
	summaryIdx := len(columnHeaders) - 1
	widths[summaryIdx] = expandSummaryWidth(widths, summaryIdx, m.width)
	for i := range m.tabs {
		cols := make([]table.Column, len(columnHeaders))
		for c, h := range columnHeaders {
			title := h
			if c == ageColIdx {
				title = m.tabs[i].ageDesc
			}
			cols[c] = table.Column{Title: title, Width: widths[c]}
		}
		m.tabs[i].table.SetColumns(cols)
	}
}

func colWidthsFromRows(sets ...[]jira.Issue) ([]int, []bool) {
	widths := make([]int, len(columnHeaders))
	hasContent := make([]bool, len(columnHeaders))
	for i, h := range columnHeaders {
		widths[i] = lipgloss.Width(h)
	}
	// "Age ▲" header is wider than "Age"; reserve for it.
	widths[ageColIdx] = lipgloss.Width("Age ▲")
	for _, set := range sets {
		for _, issue := range set {
			for i, c := range issueCells(issue) {
				if w := lipgloss.Width(c); w > widths[i] {
					widths[i] = w
				}
				if c != "" && c != "-" {
					hasContent[i] = true
				}
			}
		}
	}
	return widths, hasContent
}

func finalizeColWidths(widths []int, hasContent []bool) {
	for i, h := range columnHeaders {
		if !hasContent[i] {
			widths[i] = lipgloss.Width(h)
			if i == ageColIdx {
				widths[i] = lipgloss.Width("Age ▲")
			}
		}
		widths[i] += columnPad
	}
}

func expandSummaryWidth(widths []int, summaryIdx, termWidth int) int {
	if termWidth <= 0 {
		return widths[summaryIdx]
	}
	used := 0
	for i, w := range widths {
		if i == summaryIdx {
			continue
		}
		used += w
	}
	remaining := termWidth - used - len(columnHeaders) - 1
	if remaining > widths[summaryIdx] {
		widths[summaryIdx] = remaining
	}
	return max(widths[summaryIdx], minSummaryWidth)
}

func (m *Model) View() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("🦅 jirawk"))
	b.WriteString("  ")
	if m.busy() {
		b.WriteString(m.spinner.View())
		b.WriteString(" ")
	}
	b.WriteString(statusStyle.Render(m.status))
	b.WriteString("\n")

	if m.err != "" {
		b.WriteString("\n" + errStyle.Render("✗ "+m.err) + "\n")
		b.WriteString(hintStyle.Render("  run 'jirawk check' to diagnose") + "\n")
	}

	if m.overlay != "" {
		b.WriteString("\n" + m.overlay + "\n")
		b.WriteString("\n" + hintStyle.Render("esc/u close"))
		return b.String()
	}

	b.WriteString("\n" + m.renderTabBar() + "\n\n")

	cur := &m.tabs[m.active]
	if len(cur.rows) == 0 && !cur.loading {
		b.WriteString(hintStyle.Render("    — none —") + "\n")
	} else {
		b.WriteString(m.renderColoredTable(m.active) + "\n")
	}

	b.WriteString(m.renderScroll() + "\n")

	switch {
	case m.confirmDone:
		b.WriteString(errStyle.Render(fmt.Sprintf("🟣 Move %s to %s? [Y/n]", m.pendingDone.Key, doneState)))
	case m.palette:
		b.WriteString(keyStyle.Render("/"+m.paletteInput+"▌") +
			hintStyle.Render("   (commands: usage · esc cancels)"))
	default:
		b.WriteString(m.renderHints())
	}
	return b.String()
}

func (m *Model) renderTabBar() string {
	parts := make([]string, 0, numTabs)
	for i := range m.tabs {
		label := fmt.Sprintf("%s (%d)", m.tabs[i].title, len(m.tabs[i].rows))
		if tabKind(i) == m.active {
			parts = append(parts, tabActive.Render(label))
		} else {
			parts = append(parts, tabInactive.Render(label))
		}
	}
	return strings.Join(parts, tabSeparator.Render("  "))
}

// renderScroll shows how many rows sit above/below the current selection, so a
// long list visibly signals there is more to scroll to.
func (m *Model) renderScroll() string {
	cur := &m.tabs[m.active]
	total := len(cur.rows)
	if total == 0 {
		return ""
	}
	cursor := cur.table.Cursor()
	above := cursor
	below := total - cursor - 1
	var parts []string
	if above > 0 {
		parts = append(parts, scrollStyle.Render(fmt.Sprintf("▲ %d more above", above)))
	}
	if below > 0 {
		parts = append(parts, scrollStyle.Render(fmt.Sprintf("▼ %d more below", below)))
	}
	pos := hintStyle.Render(fmt.Sprintf("%d/%d", cursor+1, total))
	if len(parts) == 0 {
		return pos
	}
	return pos + "  " + strings.Join(parts, hintStyle.Render(" · "))
}

func (m *Model) renderHints() string {
	hints := []string{
		keyStyle.Render("←→/hl") + hintStyle.Render(" tabs"),
		keyStyle.Render("↑↓/jk") + hintStyle.Render(" nav"),
		keyStyle.Render("⏎") + hintStyle.Render(" open"),
	}
	if m.active == tabInProgress || m.active == tabOpen {
		hints = append(hints, keyStyle.Render("d")+hintStyle.Render(" done"))
	}
	hints = append(hints,
		keyStyle.Render("c")+hintStyle.Render(" copy"),
		keyStyle.Render("/")+hintStyle.Render(" cmd"),
		keyStyle.Render("r")+hintStyle.Render(" refresh"),
		keyStyle.Render("q")+hintStyle.Render(" quit"),
	)
	return strings.Join(hints, hintStyle.Render(" · "))
}

// primaryAction runs the active view's default ("intro") action triggered by
// Enter. Every view's primary action is currently "open the selected issue in
// the browser"; kept as a per-view switch so views can diverge later.
func (m *Model) primaryAction() tea.Cmd {
	switch m.active {
	case tabInProgress, tabOpen, tabClosed, tabEpics:
		if url := m.selectedURL(); url != "" {
			_ = openInBrowser(url)
		}
	}
	return nil
}

// selectedKeyOf returns the issue key currently under tab k's cursor, or "".
func (m *Model) selectedKeyOf(k tabKind) string {
	t := &m.tabs[k]
	cursor := t.table.Cursor()
	if cursor < 0 || cursor >= len(t.rows) {
		return ""
	}
	return t.rows[cursor].Key
}

// indexOfKey returns the row index of the issue with the given key, or -1.
func indexOfKey(rows []jira.Issue, key string) int {
	if key == "" {
		return -1
	}
	for i := range rows {
		if rows[i].Key == key {
			return i
		}
	}
	return -1
}

func (m *Model) selectedURL() string {
	cur := &m.tabs[m.active]
	cursor := cur.table.Cursor()
	if cursor < 0 || cursor >= len(cur.rows) {
		return ""
	}
	return cur.rows[cursor].URL
}

func (m *Model) copySelected() {
	url := m.selectedURL()
	if url == "" {
		return
	}
	if err := copyToClipboard(url); err != nil {
		m.err = err.Error()
		return
	}
	m.status = "Copied " + url
}

// renderColoredTable draws the active tab's header and visible rows, colouring
// each row by age. It reuses the bubbles table only for cursor/viewport state
// (Cursor/Height) and the precomputed column widths; rows are rendered here so
// the age colour is applied *after* truncation, sidestepping the ANSI
// corruption that blocked this in the bubbles table itself.
func (m *Model) renderColoredTable(kind tabKind) string {
	t := &m.tabs[kind]
	cols := t.table.Columns()

	cellsToRow := func(values []string, base lipgloss.Style) string {
		rendered := make([]string, 0, len(cols))
		for i, c := range cols {
			if c.Width <= 0 {
				continue
			}
			val := ""
			if i < len(values) {
				val = values[i]
			}
			inner := lipgloss.NewStyle().Width(c.Width).MaxWidth(c.Width).Inline(true)
			rendered = append(rendered, base.Render(inner.Render(runewidth.Truncate(val, c.Width, "…"))))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	}

	titles := make([]string, len(cols))
	for i, c := range cols {
		titles[i] = c.Title
	}

	h := max(t.table.Height(), 1)
	cursor := t.table.Cursor()
	start := t.offset
	end := min(start+h, len(t.rows))
	now := time.Now()

	lines := make([]string, 0, h+1)
	lines = append(lines, "  "+cellsToRow(titles, tblHeaderStyle))
	for idx := start; idx < end; idx++ {
		issue := t.rows[idx]
		age := ageColor(kind, issue, now)
		row := cellsToRow(issueCells(issue), tblCellStyle)
		if idx == cursor {
			row = selMarkerStyle.Render("▶ ") + tblSelectedStyle.Foreground(age).Render(row)
		} else {
			row = "  " + lipgloss.NewStyle().Foreground(age).Render(row)
		}
		lines = append(lines, row)
	}
	// Pad to a stable height so the footer doesn't jump for short lists.
	for len(lines) < h+1 {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// reconcileScroll keeps the active cursor inside the visible window, mirroring
// the bubbles table's minimal-scroll behaviour now that rows are drawn here.
func (m *Model) reconcileScroll(kind tabKind) {
	t := &m.tabs[kind]
	h := max(t.table.Height(), 1)
	c := max(t.table.Cursor(), 0)
	if c < t.offset {
		t.offset = c
	}
	if c >= t.offset+h {
		t.offset = c - h + 1
	}
	if maxOff := max(len(t.rows)-h, 0); t.offset > maxOff {
		t.offset = maxOff
	}
	t.offset = max(t.offset, 0)
}

// ageColor maps an issue's age to a foreground colour, with a distinct gradient
// per tab so each list degrades differently:
//   - In progress: staleness of the last update, fresh green → stale red.
//   - Open: time waiting since creation, new blue → old magenta.
//   - Closed: recency of resolution, recent bright green → old faint grey.
func ageColor(kind tabKind, i jira.Issue, now time.Time) lipgloss.Color {
	ts := i.Updated
	if kind == tabOpen {
		ts = i.Created
	}
	if ts.IsZero() {
		return lipgloss.Color("245")
	}
	days := now.Sub(ts).Hours() / 24
	var ramp []struct {
		max   float64
		color string
	}
	switch kind {
	case tabInProgress:
		ramp = []struct {
			max   float64
			color string
		}{{1, "46"}, {3, "82"}, {7, "226"}, {14, "214"}, {1e9, "196"}}
	case tabOpen:
		ramp = []struct {
			max   float64
			color string
		}{{2, "39"}, {7, "75"}, {14, "141"}, {30, "170"}, {1e9, "201"}}
	case tabEpics:
		// Violet ramp by last-update recency. Deliberately never bottoms out in
		// grey so the foreground stays readable on the grey selected-row bg.
		ramp = []struct {
			max   float64
			color string
		}{{7, "213"}, {30, "177"}, {90, "141"}, {180, "98"}, {1e9, "97"}}
	default: // tabClosed
		ramp = []struct {
			max   float64
			color string
		}{{1, "46"}, {3, "42"}, {7, "35"}, {14, "65"}, {1e9, "240"}}
	}
	for _, step := range ramp {
		if days < step.max {
			return lipgloss.Color(step.color)
		}
	}
	return lipgloss.Color("245")
}

func issueCells(i jira.Issue) []string {
	return []string{
		i.Key,
		statusEmojiLabel(i.Status),
		jira.PriorityLabel(i),
		jira.AgeLabel(i.Updated),
		i.Summary,
	}
}

func issuesToRows(issues []jira.Issue) []table.Row {
	out := make([]table.Row, 0, len(issues))
	for _, i := range issues {
		cells := issueCells(i)
		row := make(table.Row, len(cells))
		copy(row, cells)
		out = append(out, row)
	}
	return out
}

func formatUsage(s jira.Stats, now time.Time) string {
	var b strings.Builder
	b.WriteString(okStyle.Render("📊 jirawk usage") + "\n\n")
	fmt.Fprintf(&b, "  in progress : %d\n\n", s.InProgress)
	fmt.Fprintf(&b, "  closed — last %d weeks (%d total)\n", len(s.Weeks), s.DoneTotal)
	peak := 0
	for _, n := range s.Weeks {
		if n > peak {
			peak = n
		}
	}
	for i := len(s.Weeks) - 1; i >= 0; i-- {
		n := s.Weeks[i]
		bar := ""
		if peak > 0 && n > 0 {
			bar = strings.Repeat("█", max(n*24/peak, 1))
		}
		marker := "  "
		if i == 0 {
			marker = "▸ "
		}
		fmt.Fprintf(&b, "  %s%-15s %s %d\n", marker, jira.WeekLabel(now, i), barStyle.Render(bar), n)
	}
	return b.String()
}

// humanDuration renders a window like 24h / 30d for tab titles.
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

func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func copyToClipboard(s string) error {
	candidates := [][]string{
		{"pbcopy"},
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		if _, err := stdin.Write([]byte(s)); err != nil {
			return err
		}
		if err := stdin.Close(); err != nil {
			return err
		}
		return cmd.Wait()
	}
	return fmt.Errorf("no clipboard tool found")
}
