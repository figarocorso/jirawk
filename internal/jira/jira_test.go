package jira

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveCategory(t *testing.T) {
	cases := map[string]string{
		"In Progress":              "in progress",
		"In Review":                "in review",
		"Code Review":              "in review",
		"Done":                     "done",
		"Resolved":                 "done",
		"Closed":                   "done",
		"To Do":                    "to do",
		"Backlog":                  "to do",
		"Selected for Development": "to do",
		"Blocked":                  "blocked",
		"Waiting / Blocked":        "blocked",
		"":                         "unknown",
		"Implementing":             "in progress",
	}
	for status, want := range cases {
		assert.Equalf(t, want, DeriveCategory(status), "status %q", status)
	}
}

func TestJQLBuilders(t *testing.T) {
	assert.Contains(t, jqlInProgress(""), `statusCategory = "In Progress"`)
	assert.Contains(t, jqlInProgress(""), "assignee = currentUser()")
	assert.Contains(t, jqlInProgress(""), "project IS NOT EMPTY")
	assert.Contains(t, jqlInProgress(`status = "Doing"`), `status = "Doing"`)

	assert.Contains(t, jqlDoneWithin(24*time.Hour), "resolved >= -24h")
	assert.Contains(t, jqlDoneWithin(90*time.Minute), "resolved >= -90m")

	assert.Contains(t, jqlDoneWeek(0), "resolved >= -1w")
	assert.NotContains(t, jqlDoneWeek(0), "resolved <")
	wk2 := jqlDoneWeek(2)
	assert.Contains(t, wk2, "resolved >= -3w")
	assert.Contains(t, wk2, "resolved < -2w")
}

func TestBrowseURL(t *testing.T) {
	assert.Equal(t, "https://x.atlassian.net/browse/OP-1", BrowseURL("https://x.atlassian.net", "OP-1"))
	assert.Equal(t, "https://x.atlassian.net/browse/OP-1", BrowseURL("https://x.atlassian.net/", "OP-1"))
	assert.Equal(t, "OP-1", BrowseURL("", "OP-1"))
}

const sampleJSON = `[
  {"key":"OP-1","fields":{
    "summary":"first","labels":["a","b"],
    "issueType":{"name":"Task"},
    "assignee":{"displayName":"Mig"},
    "priority":{"name":"High"},
    "status":{"name":"In Progress"},
    "created":"2026-06-01T10:00:00.000-0700",
    "updated":"2026-06-02T11:00:00.000-0700"}},
  {"key":"OP-2","fields":{
    "summary":"second","assignee":null,"priority":null,
    "status":{"name":"Resolved"}}}
]`

func newTestClient(t *testing.T, out string, capture *[]string) *CLIClient {
	t.Helper()
	return NewCLIClient(
		WithServer("https://x.atlassian.net"),
		WithRunner(func(_ context.Context, args ...string) ([]byte, error) {
			if capture != nil {
				*capture = args
			}
			return []byte(out), nil
		}),
	)
}

func TestParseAndInProgress(t *testing.T) {
	var args []string
	c := newTestClient(t, sampleJSON, &args)
	issues, err := c.InProgress(context.Background())
	require.NoError(t, err)
	require.Len(t, issues, 2)

	assert.Equal(t, "OP-1", issues[0].Key)
	assert.Equal(t, "first", issues[0].Summary)
	assert.Equal(t, "Task", issues[0].Type)
	assert.Equal(t, "Mig", issues[0].Assignee)
	assert.Equal(t, "High", issues[0].Priority)
	assert.Equal(t, "in progress", issues[0].Category)
	assert.Equal(t, []string{"a", "b"}, issues[0].Labels)
	assert.Equal(t, "https://x.atlassian.net/browse/OP-1", issues[0].URL)
	assert.False(t, issues[0].Updated.IsZero())

	// nil assignee/priority must not panic and stay empty.
	assert.Equal(t, "", issues[1].Assignee)
	assert.Equal(t, "", issues[1].Priority)
	assert.Equal(t, "done", issues[1].Category)

	// JQL passed to the runner spans projects and filters in-progress.
	joined := strings.Join(args, " ")
	assert.Contains(t, joined, "project IS NOT EMPTY")
	assert.Contains(t, joined, `statusCategory = "In Progress"`)
}

func TestParseEmpty(t *testing.T) {
	c := newTestClient(t, "  ", nil)
	issues, err := c.InProgress(context.Background())
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestWeeklyDoneRunsPerBucket(t *testing.T) {
	var mu = make(chan string, 16)
	c := NewCLIClient(WithRunner(func(_ context.Context, args ...string) ([]byte, error) {
		// echo the jql so we can assert one query per week ran
		for i, a := range args {
			if a == "-q" && i+1 < len(args) {
				mu <- args[i+1]
			}
		}
		return []byte(`[{"key":"X-1","fields":{"status":{"name":"Done"}}}]`), nil
	}))
	counts, err := c.WeeklyDone(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, []int{1, 1, 1}, counts)
	close(mu)
	got := 0
	for range mu {
		got++
	}
	assert.Equal(t, 3, got)
}

func TestCLIAncestorsWalksTwoLevels(t *testing.T) {
	// Two-level chain: story OP-1 → epic EPIC-1 → initiative INIT-1. The runner
	// answers each `key IN (...)` batch by the keys requested.
	var queries []string
	c := NewCLIClient(WithRunner(func(_ context.Context, args ...string) ([]byte, error) {
		jql := ""
		for i, a := range args {
			if a == "-q" && i+1 < len(args) {
				jql = args[i+1]
			}
		}
		queries = append(queries, jql)
		switch {
		case strings.Contains(jql, "EPIC-1"):
			return []byte(`[{"key":"EPIC-1","fields":{"issueType":{"name":"Epic"},"parent":{"key":"INIT-1"},"status":{"name":"In Progress"}}}]`), nil
		case strings.Contains(jql, "INIT-1"):
			return []byte(`[{"key":"INIT-1","fields":{"issueType":{"name":"Initiative"},"status":{"name":"To Do"}}}]`), nil
		}
		return []byte(`[]`), nil
	}))

	seeds := []Issue{{Key: "OP-1", Parent: "EPIC-1"}}
	anc, err := c.Ancestors(context.Background(), seeds, 2)
	require.NoError(t, err)
	require.Len(t, anc, 2)
	assert.Equal(t, "EPIC-1", anc[0].Key)
	assert.Equal(t, "INIT-1", anc[1].Key)
	// One batch query per level.
	require.Len(t, queries, 2)
	assert.Contains(t, queries[0], "key IN (EPIC-1)")
	assert.Contains(t, queries[1], "key IN (INIT-1)")
}

func TestCLIAncestorsDedupesAndStopsAtDepth(t *testing.T) {
	var queries int
	c := NewCLIClient(WithRunner(func(_ context.Context, args ...string) ([]byte, error) {
		queries++
		// Every parent resolves to the same epic, which itself has no parent.
		return []byte(`[{"key":"EPIC-1","fields":{"issueType":{"name":"Epic"},"status":{"name":"In Progress"}}}]`), nil
	}))
	seeds := []Issue{{Key: "OP-1", Parent: "EPIC-1"}, {Key: "OP-2", Parent: "EPIC-1"}}
	anc, err := c.Ancestors(context.Background(), seeds, 2)
	require.NoError(t, err)
	// EPIC-1 appears once despite two seeds pointing at it; no parent → 1 query.
	require.Len(t, anc, 1)
	assert.Equal(t, "EPIC-1", anc[0].Key)
	assert.Equal(t, 1, queries)
}

func TestMockAncestors(t *testing.T) {
	m := NewMockClient()
	m.EpicIssues = []Issue{
		{Key: "EPIC-1", Type: "Epic", Parent: "INIT-1"},
		{Key: "INIT-1", Type: "Initiative"},
	}
	anc, err := m.Ancestors(context.Background(), []Issue{{Key: "OP-1", Parent: "EPIC-1"}}, 2)
	require.NoError(t, err)
	require.Len(t, anc, 2)
	assert.Equal(t, "EPIC-1", anc[0].Key)
	assert.Equal(t, "INIT-1", anc[1].Key)
}

func TestComputeStats(t *testing.T) {
	m := NewMockClient()
	now := time.Now()
	m.InProgressIssues = []Issue{{Key: "A"}, {Key: "B"}}
	m.DoneIssues = []Issue{
		{Key: "C", Resolved: now.Add(-2 * 24 * time.Hour)},  // week 0
		{Key: "D", Resolved: now.Add(-9 * 24 * time.Hour)},  // week 1
		{Key: "E", Resolved: now.Add(-10 * 24 * time.Hour)}, // week 1
	}
	st, err := ComputeStats(context.Background(), m, 4)
	require.NoError(t, err)
	assert.Equal(t, 2, st.InProgress)
	assert.Equal(t, 3, st.DoneTotal)
	assert.Equal(t, []int{1, 2, 0, 0}, st.Weeks)
}

func TestAgeLabel(t *testing.T) {
	now := time.Now()
	assert.Equal(t, "-", AgeLabel(time.Time{}))
	assert.Equal(t, "now", AgeLabel(now.Add(-30*time.Second)))
	assert.Equal(t, "5m", AgeLabel(now.Add(-5*time.Minute)))
	assert.Equal(t, "3h", AgeLabel(now.Add(-3*time.Hour)))
	assert.Equal(t, "2d", AgeLabel(now.Add(-2*24*time.Hour)))
	assert.Equal(t, "2w", AgeLabel(now.Add(-15*24*time.Hour)))
}

func TestLabels(t *testing.T) {
	assert.Equal(t, "-", AssigneeLabel(Issue{}))
	assert.Equal(t, "Mig", AssigneeLabel(Issue{Assignee: "Mig"}))
	assert.Equal(t, "-", PriorityLabel(Issue{}))
	assert.Equal(t, "unknown", StatusLabel(Issue{}))
	assert.Equal(t, "in progress", StatusLabel(Issue{Status: "In Progress"}))
	assert.True(t, IsDone(Issue{Category: "done"}))
	assert.False(t, IsDone(Issue{Category: "in progress"}))
}
