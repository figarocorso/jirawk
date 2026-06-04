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
	tabActive    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("212")).Padding(0, 1)
	tabInactive  = lipgloss.NewStyle().Faint(true).Padding(0, 1)
	tabSeparator = lipgloss.NewStyle().Faint(true)
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

// tabKind enumerates the three views.
type tabKind int

const (
	tabInProgress tabKind = iota
	tabOpen
	tabClosed
	numTabs
)

// tab holds one view's table, its rows, and presentation metadata.
type tab struct {
	kind    tabKind
	title   string
	ageDesc string // header label for the Age column (carries the sort arrow)
	table   table.Model
	rows    []jira.Issue
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
}

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
		loading: true,
		status:  "Loading…",
		active:  tabInProgress,
	}
	// In-progress is sorted oldest-first by age (▲ marks the sort column).
	meta := [numTabs]struct {
		kind    tabKind
		title   string
		ageDesc string
	}{
		{tabInProgress, "In progress", "Age ▲"},
		{tabOpen, "Open", "Age"},
		{tabClosed, fmt.Sprintf("Closed · %s", humanDuration(cfg.DoneWindow)), "Age"},
	}
	for i, md := range meta {
		m.tabs[i] = tab{
			kind:    md.kind,
			title:   md.title,
			ageDesc: md.ageDesc,
			table:   newTable(md.kind == tabInProgress),
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
	st.Header = st.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("212")).
		BorderTop(false).BorderLeft(false).BorderRight(false).BorderBottom(true).
		Bold(true)
	st.Selected = st.Selected.Foreground(lipgloss.Color("46")).Bold(true)
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
	cmds := []tea.Cmd{m.spinner.Tick, fetchCmd(m.cfg, m.client)}
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
		if model, cmd, handled := m.handleKey(msg.String()); handled {
			return model, cmd
		}
	case fetchMsg:
		m.handleFetch(msg)
		return m, nil
	case usageMsg:
		m.handleUsage(msg)
		return m, nil
	case autoRefreshTickMsg:
		return m, m.handleAutoRefresh()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading {
			return m, cmd
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.tabs[m.active].table, cmd = m.tabs[m.active].table.Update(msg)
	return m, cmd
}

func (m *Model) handleKey(key string) (tea.Model, tea.Cmd, bool) {
	if m.overlay != "" {
		switch key {
		case "esc", "u", "q", "ctrl+c":
			m.overlay = ""
			m.status = "Overlay closed"
			return m, nil, true
		}
		return m, nil, true
	}
	switch key {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit, true
	case "right", "l", "tab":
		return m.switchTab(1), nil, true
	case "left", "h", "shift+tab":
		return m.switchTab(-1), nil, true
	case "1":
		return m.gotoTab(tabInProgress), nil, true
	case "2":
		return m.gotoTab(tabOpen), nil, true
	case "3":
		return m.gotoTab(tabClosed), nil, true
	case "r", "ctrl+r":
		return m.refresh()
	case "u":
		m.status = "Computing usage…"
		return m, tea.Batch(m.spinner.Tick, usageCmd(m.cfg, m.client)), true
	case "enter":
		if url := m.selectedURL(); url != "" {
			_ = openInBrowser(url)
		}
		return m, nil, true
	case "c":
		m.copySelected()
		return m, nil, true
	}
	return m, nil, false
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
	m.status = m.tabs[m.active].title
	return m
}

func (m *Model) refresh() (tea.Model, tea.Cmd, bool) {
	if m.loading {
		return m, nil, false
	}
	m.loading = true
	m.status = "Refreshing…"
	return m, tea.Batch(m.spinner.Tick, fetchCmd(m.cfg, m.client)), true
}

func (m *Model) handleAutoRefresh() tea.Cmd {
	next := m.autoRefreshCmd()
	if next == nil {
		return nil
	}
	if m.loading {
		return next
	}
	m.loading = true
	m.status = "Auto-refreshing…"
	return tea.Batch(m.spinner.Tick, fetchCmd(m.cfg, m.client), next)
}

func (m *Model) handleFetch(msg fetchMsg) {
	m.loading = false
	if msg.err != nil {
		m.err = msg.err.Error()
		return
	}
	m.err = ""
	// In progress: oldest (most stale) first. Open: newest created first.
	// Closed: most recently updated first.
	jira.SortByAgeOldestFirst(msg.inProgress)
	jira.SortByCreatedNewestFirst(msg.open)
	jira.SortByUpdatedNewestFirst(msg.done)
	m.tabs[tabInProgress].rows = msg.inProgress
	m.tabs[tabOpen].rows = msg.open
	m.tabs[tabClosed].rows = msg.done
	for i := range m.tabs {
		m.tabs[i].table.SetRows(issuesToRows(m.tabs[i].rows))
	}
	m.layoutTables()
	m.status = fmt.Sprintf("%d in progress · %d open · %d closed",
		len(msg.inProgress), len(msg.open), len(msg.done))
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
	if m.loading {
		b.WriteString(m.spinner.View())
		b.WriteString(" ")
	}
	b.WriteString(statusStyle.Render(m.status))
	b.WriteString("\n")

	if m.err != "" {
		b.WriteString("\n" + errStyle.Render("✗ "+m.err) + "\n")
	}

	if m.overlay != "" {
		b.WriteString("\n" + m.overlay + "\n")
		b.WriteString("\n" + hintStyle.Render("esc/u close"))
		return b.String()
	}

	b.WriteString("\n" + m.renderTabBar() + "\n\n")

	cur := &m.tabs[m.active]
	if len(cur.rows) == 0 && !m.loading {
		b.WriteString(hintStyle.Render("    — none —") + "\n")
	} else {
		b.WriteString(cur.table.View() + "\n")
	}

	b.WriteString(m.renderScroll() + "\n")
	b.WriteString(m.renderHints())
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
		keyStyle.Render("c") + hintStyle.Render(" copy"),
		keyStyle.Render("u") + hintStyle.Render(" usage"),
		keyStyle.Render("r") + hintStyle.Render(" refresh"),
		keyStyle.Render("q") + hintStyle.Render(" quit"),
	}
	return strings.Join(hints, hintStyle.Render(" · "))
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
