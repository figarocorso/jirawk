package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// maxResults caps how many issues a single jira-cli query returns. jira-cli's
// --paginate limit is 100, which a single user's per-week resolved count or
// in-progress list is very unlikely to exceed.
const maxResults = 100

// CLIClient implements Client by shelling out to the `jira` binary with --raw
// and parsing its JSON. It inherits whatever auth `jira` is configured with.
type CLIClient struct {
	bin    string // jira binary, default "jira"
	server string // base URL for browse links, e.g. https://acme.atlassian.net
	// statusClause optionally overrides the default in-progress JQL predicate.
	statusClause string
	// runner executes the jira CLI; overridable in tests.
	runner func(ctx context.Context, args ...string) ([]byte, error)
}

// Option configures a CLIClient.
type Option func(*CLIClient)

// WithBinary sets the jira binary path.
func WithBinary(bin string) Option {
	return func(c *CLIClient) {
		if bin != "" {
			c.bin = bin
		}
	}
}

// WithServer sets the base URL used to build browse links.
func WithServer(server string) Option {
	return func(c *CLIClient) { c.server = server }
}

// WithStatusClause overrides the default `statusCategory = "In Progress"`
// predicate used by InProgress.
func WithStatusClause(clause string) Option {
	return func(c *CLIClient) { c.statusClause = clause }
}

// WithRunner injects a custom command runner. Used in tests to avoid shelling
// out to a real `jira` binary.
func WithRunner(r func(ctx context.Context, args ...string) ([]byte, error)) Option {
	return func(c *CLIClient) { c.runner = r }
}

// NewCLIClient builds a CLIClient. Defaults: bin="jira".
func NewCLIClient(opts ...Option) *CLIClient {
	c := &CLIClient{bin: "jira"}
	for _, o := range opts {
		o(c)
	}
	if c.runner == nil {
		c.runner = c.exec
	}
	return c
}

func (c *CLIClient) exec(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("jira: %s", msg)
	}
	return stdout.Bytes(), nil
}

// query runs `jira issue list -q <jql> --raw` and parses the result.
func (c *CLIClient) query(ctx context.Context, jql string) ([]Issue, error) {
	out, err := c.runner(ctx,
		"issue", "list",
		"-q", jql,
		"--raw",
		"--paginate", fmt.Sprintf("0:%d", maxResults),
	)
	if err != nil {
		return nil, err
	}
	return c.parse(out)
}

// rawIssue mirrors the subset of jira-cli's --raw JSON we consume.
type rawIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary   string   `json:"summary"`
		Labels    []string `json:"labels"`
		IssueType struct {
			Name string `json:"name"`
		} `json:"issueType"`
		Assignee *struct {
			DisplayName string `json:"displayName"`
		} `json:"assignee"`
		Reporter *struct {
			DisplayName string `json:"displayName"`
		} `json:"reporter"`
		Priority *struct {
			Name string `json:"name"`
		} `json:"priority"`
		Status struct {
			Name     string `json:"name"`
			Category struct {
				Name string `json:"name"`
			} `json:"statusCategory"`
		} `json:"status"`
		Created string `json:"created"`
		Updated string `json:"updated"`
	} `json:"fields"`
}

const jiraTimeLayout = "2006-01-02T15:04:05.999-0700"

// deriveCategoryFromRaw prefers jira-cli's statusCategory when present (rare in
// --raw output) and otherwise derives the category from the status name.
func deriveCategoryFromRaw(status struct {
	Name     string `json:"name"`
	Category struct {
		Name string `json:"name"`
	} `json:"statusCategory"`
}) string {
	if status.Category.Name != "" {
		return strings.ToLower(status.Category.Name)
	}
	return DeriveCategory(status.Name)
}

func (c *CLIClient) parse(out []byte) ([]Issue, error) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, nil
	}
	var raw []rawIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse jira output: %w", err)
	}
	issues := make([]Issue, 0, len(raw))
	for _, r := range raw {
		issues = append(issues, c.toIssue(r))
	}
	return issues, nil
}

func (c *CLIClient) toIssue(r rawIssue) Issue {
	i := Issue{
		Key:      r.Key,
		Summary:  r.Fields.Summary,
		Status:   r.Fields.Status.Name,
		Category: deriveCategoryFromRaw(r.Fields.Status),
		Type:     r.Fields.IssueType.Name,
		Labels:   r.Fields.Labels,
		URL:      BrowseURL(c.server, r.Key),
	}
	if r.Fields.Assignee != nil {
		i.Assignee = r.Fields.Assignee.DisplayName
	}
	if r.Fields.Reporter != nil {
		i.Reporter = r.Fields.Reporter.DisplayName
	}
	if r.Fields.Priority != nil {
		i.Priority = r.Fields.Priority.Name
	}
	if t, err := time.Parse(jiraTimeLayout, r.Fields.Created); err == nil {
		i.Created = t
	}
	if t, err := time.Parse(jiraTimeLayout, r.Fields.Updated); err == nil {
		i.Updated = t
	}
	return i
}

// InProgress fetches the user's in-progress issues across all projects.
func (c *CLIClient) InProgress(ctx context.Context) ([]Issue, error) {
	return c.query(ctx, jqlInProgress(c.statusClause))
}

// RecentlyDone fetches issues the user resolved within the given window.
func (c *CLIClient) RecentlyDone(ctx context.Context, within time.Duration) ([]Issue, error) {
	return c.query(ctx, jqlDoneWithin(within))
}

// WeeklyDone returns per-week resolved counts for the last `weeks` weeks,
// running one count query per bucket concurrently. Index 0 = most recent week.
func (c *CLIClient) WeeklyDone(ctx context.Context, weeks int) ([]int, error) {
	if weeks <= 0 {
		return nil, nil
	}
	counts := make([]int, weeks)
	errs := make([]error, weeks)
	var wg sync.WaitGroup
	for n := range weeks {
		wg.Go(func() {
			issues, err := c.query(ctx, jqlDoneWeek(n))
			if err != nil {
				errs[n] = err
				return
			}
			counts[n] = len(issues)
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return counts, err
		}
	}
	return counts, nil
}

// Get fetches a single issue by key via JQL (key = <KEY>).
func (c *CLIClient) Get(ctx context.Context, key string) (Issue, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return Issue{}, fmt.Errorf("empty issue key")
	}
	issues, err := c.query(ctx, fmt.Sprintf("key = %s", key))
	if err != nil {
		return Issue{}, err
	}
	if len(issues) == 0 {
		return Issue{}, fmt.Errorf("issue not found: %s", key)
	}
	return issues[0], nil
}
