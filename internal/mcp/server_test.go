package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/figarocorso/jirawk/internal/config"
	"github.com/figarocorso/jirawk/internal/jira"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(in string) (*Server, *bytes.Buffer) {
	now := time.Now()
	m := jira.NewMockClient()
	m.InProgressIssues = []jira.Issue{{Key: "PROJ-1", Summary: "a", Status: "In Progress"}}
	m.DoneIssues = []jira.Issue{{Key: "PROJ-2", Summary: "b", Status: "Done", Resolved: now.Add(-2 * time.Hour), Updated: now.Add(-2 * time.Hour)}}
	var out bytes.Buffer
	s := NewServer(Options{
		Config: &config.Config{DoneWindow: 24 * time.Hour, Weeks: 4},
		Client: m,
		In:     strings.NewReader(in),
		Out:    &out,
	})
	return s, &out
}

func decodeResponses(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var msgs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		msgs = append(msgs, m)
	}
	return msgs
}

func TestInitializeAndToolsList(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	s, out := newTestServer(in)
	require.NoError(t, s.Run(context.Background()))
	msgs := decodeResponses(t, out.String())
	require.Len(t, msgs, 2)

	init := msgs[0]["result"].(map[string]any)
	assert.Equal(t, "jirawk", init["serverInfo"].(map[string]any)["name"])

	tools := msgs[1]["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tdef := range tools {
		names[tdef.(map[string]any)["name"].(string)] = true
	}
	assert.True(t, names["list_issues"])
	assert.True(t, names["get_issue"])
	assert.True(t, names["stats"])
}

func TestToolCallListIssues(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_issues","arguments":{"section":"all"}}}`
	s, out := newTestServer(in)
	require.NoError(t, s.Run(context.Background()))
	msgs := decodeResponses(t, out.String())
	require.Len(t, msgs, 1)
	text := msgs[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	assert.Contains(t, text, "PROJ-1")
	assert.Contains(t, text, "PROJ-2")
}

func TestToolCallStats(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"stats","arguments":{}}}`
	s, out := newTestServer(in)
	require.NoError(t, s.Run(context.Background()))
	msgs := decodeResponses(t, out.String())
	text := msgs[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	assert.Contains(t, text, `"in_progress": 1`)
	assert.Contains(t, text, `"done_total": 1`)
}

func TestUnknownTool(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`
	s, out := newTestServer(in)
	require.NoError(t, s.Run(context.Background()))
	msgs := decodeResponses(t, out.String())
	assert.NotNil(t, msgs[0]["error"])
}
