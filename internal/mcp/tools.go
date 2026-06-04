package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/figarocorso/jirawk/internal/jira"
)

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func (s *Server) toolDefinitions() []toolDef {
	return []toolDef{
		{
			Name:        "list_issues",
			Description: "List the current user's Jira issues. section=in-progress (default), done (closed in the configured window, e.g. last 24h), or all.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"section": map[string]any{"type": "string", "enum": []string{"in-progress", "done", "all"}, "default": "in-progress"},
				},
			},
		},
		{
			Name:        "get_issue",
			Description: "Fetch full detail (status, type, priority, assignee, labels, URL) for a single issue by key.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"key": map[string]any{"type": "string", "description": "issue key, e.g. OP-649"}},
				"required":   []string{"key"},
			},
		},
		{
			Name:        "stats",
			Description: "Return the in-progress count and a weekly breakdown of resolved issues over the last N weeks (index 0 = current week).",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"weeks": map[string]any{"type": "integer", "default": jira.DefaultWeeks}},
			},
		},
	}
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func jsonResult(v any) toolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return toolResult{Content: []toolContent{{Type: "text", Text: err.Error()}}, IsError: true}
	}
	return toolResult{Content: []toolContent{{Type: "text", Text: string(b)}}}
}

func (s *Server) handleToolCall(ctx context.Context, params json.RawMessage) (toolResult, error) {
	var call toolCallParams
	if err := json.Unmarshal(params, &call); err != nil {
		return toolResult{}, fmt.Errorf("parse params: %w", err)
	}
	switch call.Name {
	case "list_issues":
		return s.toolListIssues(ctx, call.Arguments)
	case "get_issue":
		return s.toolGetIssue(ctx, call.Arguments)
	case "stats":
		return s.toolStats(ctx, call.Arguments)
	default:
		return toolResult{}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func (s *Server) toolListIssues(ctx context.Context, args map[string]any) (toolResult, error) {
	section := strings.ToLower(stringArg(args, "section"))
	if section == "" {
		section = "in-progress"
	}
	payload := map[string]any{}
	if section == "in-progress" || section == "all" {
		issues, err := s.client.InProgress(ctx)
		if err != nil {
			return toolResult{}, err
		}
		payload["in_progress"] = issues
	}
	if section == "done" || section == "all" {
		issues, err := s.client.RecentlyDone(ctx, s.cfg.DoneWindow)
		if err != nil {
			return toolResult{}, err
		}
		payload["done"] = issues
	}
	if len(payload) == 0 {
		return toolResult{}, fmt.Errorf("invalid section %q", section)
	}
	return jsonResult(payload), nil
}

func (s *Server) toolGetIssue(ctx context.Context, args map[string]any) (toolResult, error) {
	key := strings.ToUpper(strings.TrimSpace(stringArg(args, "key")))
	if key == "" {
		return toolResult{}, fmt.Errorf("key is required")
	}
	issue, err := s.client.Get(ctx, key)
	if err != nil {
		return toolResult{}, err
	}
	return jsonResult(issue), nil
}

func (s *Server) toolStats(ctx context.Context, args map[string]any) (toolResult, error) {
	weeks := intArg(args, "weeks")
	if weeks <= 0 {
		weeks = s.cfg.Weeks
	}
	st, err := jira.ComputeStats(ctx, s.client, weeks)
	if err != nil {
		return toolResult{}, err
	}
	return jsonResult(st), nil
}
