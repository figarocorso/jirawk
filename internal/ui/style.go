// Package ui centralises terminal styling helpers used by the CLI.
//
// Two modes:
//   - fancy: lipgloss colors + emoji. For human terminals.
//   - plain: no ANSI, ASCII tokens (e.g. "[OK]"). For pipes, AI agents,
//     CI logs, or when --plain / NO_COLOR is set.
package ui

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/figarocorso/jirawk/internal/jira"
	"github.com/mattn/go-isatty"
)

// Plain forces ASCII-only, color-free output. Set from the --plain flag.
var Plain bool

// IsPlain reports whether w should receive plain (AI-friendly) output.
// Order: explicit flag, NO_COLOR env, non-TTY writer.
func IsPlain(w io.Writer) bool {
	if Plain {
		return true
	}
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	f, ok := w.(*os.File)
	if !ok {
		return true
	}
	fd := f.Fd()
	return !isatty.IsTerminal(fd) && !isatty.IsCygwinTerminal(fd)
}

var (
	okColor     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnColor   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errColor    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	infoColor   = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	dimColor    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("244"))
	doneColor   = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	barColor    = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
)

// OK returns the success marker (fancy "✓" colored green, plain "[OK]").
func OK(plain bool) string {
	if plain {
		return "[OK]"
	}
	return okColor.Render("✓")
}

// Warn returns the warning marker.
func Warn(plain bool) string {
	if plain {
		return "[WARN]"
	}
	return warnColor.Render("⚠")
}

// Err returns the error marker.
func Err(plain bool) string {
	if plain {
		return "[ERR]"
	}
	return errColor.Render("✗")
}

// Info returns the info marker.
func Info(plain bool) string {
	if plain {
		return "[INFO]"
	}
	return infoColor.Render("ℹ")
}

// Arrow returns a bullet-style arrow for list items.
func Arrow(plain bool) string {
	if plain {
		return "->"
	}
	return dimColor.Render("→")
}

// Title styles top-level headers (bold + accent color in fancy mode).
func Title(plain bool, s string) string {
	if plain {
		return s
	}
	return titleStyle.Render(s)
}

// Header styles column headers (bold + dim in fancy mode).
func Header(plain bool, s string) string {
	if plain {
		return s
	}
	return headerStyle.Render(s)
}

// Dim renders secondary text in a muted color.
func Dim(plain bool, s string) string {
	if plain {
		return s
	}
	return dimColor.Render(s)
}

// Bar renders a horizontal bar string in the chart accent color.
func Bar(plain bool, s string) string {
	if plain {
		return s
	}
	return barColor.Render(s)
}

// StatusBadge returns a colored, emoji-prefixed badge for a Jira status name
// (e.g. "In Progress", "Resolved", "In Review"). The emoji/color is chosen by
// deriving the status category from the name; the original name is displayed.
// In plain mode the name is returned verbatim.
func StatusBadge(plain bool, status string) string {
	if status == "" {
		status = "unknown"
	}
	if plain {
		return status
	}
	emoji, style := badgeStyle(jira.DeriveCategory(status))
	return style.Render(emoji + " " + status)
}

func badgeStyle(category string) (string, lipgloss.Style) {
	switch category {
	case "in review":
		return "👀", warnColor
	case "done":
		return "🟣", doneColor
	case "to do":
		return "⚪", dimColor
	case "blocked":
		return "⛔", errColor
	case "unknown":
		return "❓", dimColor
	default: // in progress
		return "🔵", infoColor
	}
}
