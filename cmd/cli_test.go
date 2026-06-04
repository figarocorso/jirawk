package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/figarocorso/jirawk/internal/config"
	"github.com/figarocorso/jirawk/internal/jira"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withMock swaps clientFactory for one returning m, restoring it after the test.
func withMock(t *testing.T, m jira.Client) {
	t.Helper()
	orig := clientFactory
	clientFactory = func(_ *config.Config) (jira.Client, error) { return m, nil }
	t.Cleanup(func() { clientFactory = orig })
}

// run executes the root command with args and returns combined stdout.
func run(t *testing.T, args ...string) string {
	t.Helper()
	// Reset persistent global flag state between invocations of the shared root.
	jsonOut = false
	listSection = "all"
	statsWeeks = 0
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)
	require.NoError(t, rootCmd.Execute())
	return buf.String()
}

func sampleMock() *jira.MockClient {
	now := time.Now()
	m := jira.NewMockClient()
	m.InProgressIssues = []jira.Issue{
		{Key: "OP-1", Summary: "fix thing", Status: "In Progress", Priority: "High", Updated: now.Add(-2 * time.Hour), URL: "https://x/browse/OP-1"},
		{Key: "QA-9", Summary: "test thing", Status: "In Review", Priority: "Medium", Updated: now.Add(-26 * time.Hour)},
	}
	m.OpenIssues = []jira.Issue{
		{Key: "OP-7", Summary: "todo thing", Status: "To Do", Priority: "Low", Created: now.Add(-time.Hour)},
	}
	m.DoneIssues = []jira.Issue{
		{Key: "OP-2", Summary: "done thing", Status: "Resolved", Priority: "Low", Resolved: now.Add(-3 * time.Hour), Updated: now.Add(-3 * time.Hour)},
		{Key: "OP-3", Summary: "old thing", Status: "Done", Resolved: now.Add(-20 * 24 * time.Hour), Updated: now.Add(-20 * 24 * time.Hour)},
	}
	return m
}

func TestListAll(t *testing.T) {
	withMock(t, sampleMock())
	out := run(t, "list", "--section", "all")
	assert.Contains(t, out, "in progress (2)")
	assert.Contains(t, out, "OP-1")
	assert.Contains(t, out, "QA-9")
	assert.Contains(t, out, "open (1)")
	assert.Contains(t, out, "OP-7")
	assert.Contains(t, out, "closed")
	assert.Contains(t, out, "OP-2") // resolved within window
	assert.Contains(t, out, "OP-3") // resolved 20d ago, within new 30d window
}

func TestListOpenSection(t *testing.T) {
	withMock(t, sampleMock())
	out := run(t, "list", "--section", "open")
	assert.Contains(t, out, "open (1)")
	assert.Contains(t, out, "OP-7")
	assert.NotContains(t, out, "OP-1")
}

func TestListJSON(t *testing.T) {
	withMock(t, sampleMock())
	out := run(t, "list", "--json")
	assert.Contains(t, out, `"in_progress"`)
	assert.Contains(t, out, `"done"`)
	assert.Contains(t, out, `"OP-1"`)
	assert.Contains(t, out, `"category": "in progress"`)
}

func TestGet(t *testing.T) {
	withMock(t, sampleMock())
	out := run(t, "get", "OP-1")
	assert.Contains(t, out, "OP-1")
	assert.Contains(t, out, "fix thing")
	assert.Contains(t, out, "High")
}

func TestStats(t *testing.T) {
	withMock(t, sampleMock())
	out := run(t, "stats", "--weeks", "4")
	assert.Contains(t, out, "in progress : 2")
	assert.Contains(t, out, "closed — last 4 weeks")
	// OP-2 in week 0, OP-3 in week 2 → total 2 within 4-week window.
	assert.Contains(t, out, "(2 total)")
}

func TestRenderBar(t *testing.T) {
	assert.Equal(t, "", renderBar(0, 5))
	assert.Equal(t, "", renderBar(3, 0))
	assert.Equal(t, strings.Repeat("█", barWidth), renderBar(5, 5))
	assert.Equal(t, 1, len([]rune(renderBar(1, 1000)))) // clamps to at least 1
}

func TestClientFactoryPreflightMissingBinary(t *testing.T) {
	_, err := clientFactory(&config.Config{JiraBin: "jira-definitely-not-installed-xyz"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jirawk check")
}

func TestHumanDuration(t *testing.T) {
	assert.Equal(t, "24h", humanDuration(24*time.Hour))
	assert.Equal(t, "2d", humanDuration(48*time.Hour))
	assert.Equal(t, "12h", humanDuration(12*time.Hour))
}
