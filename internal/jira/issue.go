// Package jira fetches issue metadata from Jira via the `jira` CLI.
//
// The Client interface keeps the jira-cli-specific code behind a small surface
// so the TUI/CLI/MCP layers can be exercised against a mock in tests.
package jira

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Issue is the structured view of a single Jira issue.
type Issue struct {
	Key      string    `json:"key"`
	Summary  string    `json:"summary,omitempty"`
	Status   string    `json:"status,omitempty"`
	Category string    `json:"category,omitempty"` // statusCategory: "In Progress" | "Done" | "To Do"
	Type     string    `json:"type,omitempty"`
	Parent   string    `json:"parent,omitempty"` // parent issue key (epic for a story, initiative for an epic)
	Priority string    `json:"priority,omitempty"`
	Assignee string    `json:"assignee,omitempty"`
	Reporter string    `json:"reporter,omitempty"`
	Labels   []string  `json:"labels,omitempty"`
	Created  time.Time `json:"created,omitzero"`
	Updated  time.Time `json:"updated,omitzero"`
	Resolved time.Time `json:"resolved,omitzero"`
	URL      string    `json:"url,omitempty"`
}

// Client is the surface used by the CLI/TUI/MCP layers to read issue state.
type Client interface {
	// InProgress returns issues assigned to the current user whose status
	// category is "In Progress".
	InProgress(ctx context.Context) ([]Issue, error)
	// Open returns the user's not-yet-started issues (status category "To Do").
	Open(ctx context.Context) ([]Issue, error)
	// RecentlyDone returns issues resolved within the given window.
	RecentlyDone(ctx context.Context, within time.Duration) ([]Issue, error)
	// Ancestors walks the parent chain of seeds upward up to depth levels and
	// returns the distinct epics/initiatives encountered. Their own status is not
	// filtered. depth 1 = direct parents (epics); depth 2 also includes those
	// epics' parents (initiatives).
	Ancestors(ctx context.Context, seeds []Issue, depth int) ([]Issue, error)
	// WeeklyDone returns per-week resolved counts for the last `weeks` weeks.
	// Index 0 is the most recent week (now-1w .. now).
	WeeklyDone(ctx context.Context, weeks int) ([]int, error)
	// Get fetches a single issue by key.
	Get(ctx context.Context, key string) (Issue, error)
	// Transition moves an issue to the named target state (e.g. "Done").
	Transition(ctx context.Context, key, state string) error
}

// Section identifies which bucket an issue belongs to in the UI.
type Section string

const (
	SectionInProgress Section = "in-progress"
	SectionDone       Section = "done"
)

// baseJQL scopes every query to issues assigned to the current user across all
// projects. Mentioning `project` stops jira-cli from injecting its default
// project filter, so queries span every project the user touches.
const baseJQL = "project IS NOT EMPTY AND assignee = currentUser()"

// jqlInProgress builds the JQL for the "in progress" section.
func jqlInProgress(statusClause string) string {
	if statusClause == "" {
		statusClause = `statusCategory = "In Progress"`
	}
	return fmt.Sprintf("%s AND %s", baseJQL, statusClause)
}

// jqlNotStarted builds the JQL for the "open / not started" section.
func jqlNotStarted() string {
	return fmt.Sprintf(`%s AND statusCategory = "To Do"`, baseJQL)
}

// jqlDoneWithin builds the JQL for issues resolved within a window like 24h.
func jqlDoneWithin(within time.Duration) string {
	return fmt.Sprintf("%s AND statusCategory = Done AND resolved >= -%s",
		baseJQL, jqlDuration(within))
}

// jqlDoneWeek builds the JQL for issues resolved in week bucket n (0 = current
// week: now-1w .. now; 1 = previous week: now-2w .. now-1w; ...).
func jqlDoneWeek(n int) string {
	if n <= 0 {
		return fmt.Sprintf("%s AND statusCategory = Done AND resolved >= -1w", baseJQL)
	}
	return fmt.Sprintf("%s AND statusCategory = Done AND resolved >= -%dw AND resolved < -%dw",
		baseJQL, n+1, n)
}

// jqlByKeys builds JQL selecting the given issue keys. Returns "" for no keys.
// The `project IS NOT EMPTY` prefix stops jira-cli from injecting its default
// project filter, so keys from any project (e.g. a parent epic in another
// project than the child) are returned. Unlike baseJQL it omits the assignee
// filter: ancestor epics/initiatives are often assigned to someone else.
func jqlByKeys(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	return fmt.Sprintf("project IS NOT EMPTY AND key IN (%s)", strings.Join(keys, ", "))
}

// IsEpicOrInitiative reports whether the issue type is an epic or initiative.
func IsEpicOrInitiative(i Issue) bool {
	switch strings.ToLower(i.Type) {
	case "epic", "initiative":
		return true
	}
	return false
}

// jqlDuration renders a Go duration as a JQL relative-time token (h/m).
func jqlDuration(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// IsDone reports whether the issue has reached the Done status category.
func IsDone(i Issue) bool {
	return strings.EqualFold(i.Category, "Done")
}

// BrowseURL returns the canonical browse URL for a key given a server base.
func BrowseURL(server, key string) string {
	server = strings.TrimRight(server, "/")
	if server == "" {
		return key
	}
	return fmt.Sprintf("%s/browse/%s", server, key)
}
