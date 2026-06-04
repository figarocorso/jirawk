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
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	doneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	keyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	hintStyle    = lipgloss.NewStyle().Faint(true)
	barStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	dimSection   = lipgloss.NewStyle().Faint(true).Bold(true)
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

// section indexes the two focusable tables.
type section int

const (
	secInProgress section = iota
	secDone
)

// Model is the Bubble Tea state for jirawk's TUI.
type Model struct {
	cfg     *config.Config
	client  jira.Client
	tables  [2]table.Model
	rows    [2][]jira.Issue
	focus   section
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
		focus:   secInProgress,
	}
	for i := range m.tables {
		m.tables[i] = newTable(i == 0)
	}
	return m
}

func newTable(focused bool) table.Model {
	t := table.New(
		table.WithColumns(initialColumns()),
		table.WithFocused(focused),
		table.WithHeight(6),
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

func initialColumns() []table.Column {
	cols := make([]table.Column, len(columnHeaders))
	for i, h := range columnHeaders {
		cols[i] = table.Column{Title: h, Width: lipgloss.Width(h) + columnPad}
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
	m.tables[m.focus], cmd = m.tables[m.focus].Update(msg)
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
	case "tab", "shift+tab":
		return m.toggleFocus(), nil, true
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

func (m *Model) toggleFocus() *Model {
	m.tables[m.focus].Blur()
	if m.focus == secInProgress {
		m.focus = secDone
	} else {
		m.focus = secInProgress
	}
	m.tables[m.focus].Focus()
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
	m.rows[secInProgress] = msg.inProgress
	m.rows[secDone] = msg.done
	m.tables[secInProgress].SetRows(issuesToRows(msg.inProgress))
	m.tables[secDone].SetRows(issuesToRows(msg.done))
	m.layoutTables()
	m.status = fmt.Sprintf("%d in progress · %d closed recently", len(msg.inProgress), len(msg.done))
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
	// Split the vertical space: header + two section titles + hints ≈ 8 lines.
	avail := max(m.height-9, 6)
	per := max(avail/2, 3)
	m.tables[secInProgress].SetHeight(per)
	m.tables[secDone].SetHeight(per)
}

func (m *Model) recomputeColumnWidths() {
	widths, hasContent := colWidthsFromRows(m.rows[secInProgress], m.rows[secDone])
	finalizeColWidths(widths, hasContent)
	summaryIdx := len(columnHeaders) - 1
	widths[summaryIdx] = expandSummaryWidth(widths, summaryIdx, m.width)
	cols := make([]table.Column, len(columnHeaders))
	for i, h := range columnHeaders {
		cols[i] = table.Column{Title: h, Width: widths[i]}
	}
	for i := range m.tables {
		m.tables[i].SetColumns(cols)
	}
}

func colWidthsFromRows(sets ...[]jira.Issue) ([]int, []bool) {
	widths := make([]int, len(columnHeaders))
	hasContent := make([]bool, len(columnHeaders))
	for i, h := range columnHeaders {
		widths[i] = lipgloss.Width(h)
	}
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

	b.WriteString("\n")
	b.WriteString(m.renderSection(secInProgress, "In progress", okStyle))
	b.WriteString("\n")
	b.WriteString(m.renderSection(secDone, fmt.Sprintf("Closed · last %s", humanDuration(m.cfg.DoneWindow)), doneStyle))

	hints := []string{
		keyStyle.Render("↑↓/jk") + hintStyle.Render(" nav"),
		keyStyle.Render("⇥") + hintStyle.Render(" switch"),
		keyStyle.Render("⏎") + hintStyle.Render(" open"),
		keyStyle.Render("c") + hintStyle.Render(" copy"),
		keyStyle.Render("u") + hintStyle.Render(" usage"),
		keyStyle.Render("r") + hintStyle.Render(" refresh"),
		keyStyle.Render("q") + hintStyle.Render(" quit"),
	}
	b.WriteString("\n" + strings.Join(hints, hintStyle.Render(" · ")))
	return b.String()
}

func (m *Model) renderSection(sec section, title string, accent lipgloss.Style) string {
	count := len(m.rows[sec])
	style := dimSection
	if m.focus == sec {
		style = sectionStyle
	}
	head := style.Render(fmt.Sprintf("%s (%d)", title, count))
	if m.focus == sec {
		head = accent.Render("▸ ") + head
	} else {
		head = "  " + head
	}
	if count == 0 && !m.loading {
		return head + "\n" + hintStyle.Render("    — none —") + "\n"
	}
	return head + "\n" + m.tables[sec].View() + "\n"
}

func (m *Model) selectedURL() string {
	rows := m.rows[m.focus]
	cursor := m.tables[m.focus].Cursor()
	if cursor < 0 || cursor >= len(rows) {
		return ""
	}
	return rows[cursor].URL
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

// humanDuration renders a window like 24h / 2d for section titles.
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
