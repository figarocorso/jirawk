package jira

import (
	"fmt"
	"strings"
	"time"
)

// StatusLabel returns the lowercased Jira status name ("in progress", "in
// review", "done", ...), or "unknown" when empty.
func StatusLabel(i Issue) string {
	if i.Status == "" {
		return "unknown"
	}
	return strings.ToLower(i.Status)
}

// DeriveCategory maps a Jira status name to a coarse category, since jira-cli's
// --raw output omits the real statusCategory. Returns one of: "in progress",
// "in review", "done", "to do", "blocked", "unknown".
func DeriveCategory(status string) string {
	s := strings.ToLower(strings.TrimSpace(status))
	switch {
	case s == "" || s == "unknown":
		return "unknown"
	case strings.Contains(s, "review"):
		return "in review"
	case strings.Contains(s, "block"):
		return "blocked"
	}
	switch s {
	case "done", "resolved", "closed", "complete", "completed", "cancelled",
		"canceled", "won't do", "won't fix", "abandoned", "merged", "released":
		return "done"
	case "to do", "todo", "open", "backlog", "new", "reopened",
		"selected for development":
		return "to do"
	default:
		return "in progress"
	}
}

// AssigneeLabel renders the assignee column ("-" when unassigned).
func AssigneeLabel(i Issue) string {
	if i.Assignee == "" {
		return "-"
	}
	return i.Assignee
}

// PriorityLabel renders the priority column ("-" when unset).
func PriorityLabel(i Issue) string {
	if i.Priority == "" {
		return "-"
	}
	return i.Priority
}

// AgeLabel renders a compact relative age ("3h", "2d", "5w") from t to now.
// Returns "-" when t is zero.
func AgeLabel(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return relAge(time.Since(t))
}

func relAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	default:
		return fmt.Sprintf("%dw", int(d.Hours())/(24*7))
	}
}
