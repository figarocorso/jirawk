package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/figarocorso/jirawk/internal/ui"
	"golang.org/x/term"
)

// titleFallbackWidth caps the last (flexible) column when output isn't a TTY.
const titleFallbackWidth = 80

// minLastWidth keeps the flexible last column readable when the terminal is narrow.
const minLastWidth = 20

// renderTable prints a table where the last column flows to fill the terminal.
// headers are the column titles; rows are pre-rendered raw cell values (no
// ANSI). styleCell optionally re-styles a (col, raw) value for display.
func renderTable(out io.Writer, headers []string, rawRows [][]string, plain bool, styleCell func(col int, raw string) string) {
	cells := make([][]tableCell, 0, len(rawRows)+1)
	header := make([]tableCell, len(headers))
	for i, h := range headers {
		header[i] = tableCell{raw: h, rendered: ui.Header(plain, h)}
	}
	cells = append(cells, header)
	for _, r := range rawRows {
		row := make([]tableCell, len(r))
		for i, v := range r {
			rendered := v
			if styleCell != nil {
				rendered = styleCell(i, v)
			}
			row[i] = tableCell{raw: v, rendered: rendered}
		}
		cells = append(cells, row)
	}
	widths := smartColumnWidths(cells, headers, terminalWidth(out))
	for _, row := range cells {
		printTableRow(out, row, widths)
	}
}

type tableCell struct{ raw, rendered string }

func terminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	if !term.IsTerminal(int(f.Fd())) {
		return 0
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return cols
}

func smartColumnWidths(rows [][]tableCell, headers []string, termWidth int) []int {
	if len(rows) == 0 {
		return nil
	}
	cols := len(rows[0])
	lastIdx := cols - 1
	widths, hasContent := initColWidths(headers, rows[1:])
	collapseEmptyCols(widths, hasContent, headers)
	widths[lastIdx] = sizeLastColumn(widths, rows[1:], headers, termWidth, lastIdx)
	return widths
}

func initColWidths(headers []string, dataRows [][]tableCell) ([]int, []bool) {
	cols := len(headers)
	widths := make([]int, cols)
	hasContent := make([]bool, cols)
	for i, h := range headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range dataRows {
		for i, c := range row {
			if w := lipgloss.Width(c.raw); w > widths[i] {
				widths[i] = w
			}
			if c.raw != "" && c.raw != "-" {
				hasContent[i] = true
			}
		}
	}
	return widths, hasContent
}

func collapseEmptyCols(widths []int, hasContent []bool, headers []string) {
	for i, h := range headers {
		if !hasContent[i] {
			widths[i] = lipgloss.Width(h)
		}
	}
}

func sizeLastColumn(widths []int, dataRows [][]tableCell, headers []string, termWidth, lastIdx int) int {
	maxLast := 0
	for _, row := range dataRows {
		if w := lipgloss.Width(row[lastIdx].raw); w > maxLast {
			maxLast = w
		}
	}
	var w int
	if termWidth > 0 {
		used := 0
		for i, cw := range widths {
			if i == lastIdx {
				continue
			}
			used += cw + 2
		}
		remaining := max(termWidth-used, minLastWidth)
		w = min(remaining, maxLast)
	} else {
		w = min(maxLast, titleFallbackWidth)
	}
	return max(w, lipgloss.Width(headers[lastIdx]))
}

func printTableRow(out io.Writer, row []tableCell, widths []int) {
	for i, c := range row {
		if i == len(row)-1 {
			fmt.Fprint(out, truncateCell(c.rendered, c.raw, widths[i]))
			continue
		}
		pad := max(widths[i]-lipgloss.Width(c.raw)+2, 1)
		fmt.Fprint(out, c.rendered, strings.Repeat(" ", pad))
	}
	fmt.Fprintln(out)
}

func truncateCell(rendered, raw string, width int) string {
	if lipgloss.Width(raw) <= width {
		return rendered
	}
	return truncate(raw, width)
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
