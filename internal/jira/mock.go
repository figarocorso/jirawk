package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// MockClient is an in-memory Client for tests and offline development.
type MockClient struct {
	InProgressIssues []Issue
	DoneIssues       []Issue // candidates for RecentlyDone / WeeklyDone, with Resolved set
	GetErr           error
	ListErr          error
}

// NewMockClient builds an empty MockClient.
func NewMockClient() *MockClient { return &MockClient{} }

// LoadFixtures reads a JSON file of {"in_progress":[...],"done":[...]} issues.
func (m *MockClient) LoadFixtures(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read fixture %s: %w", path, err)
	}
	var fx struct {
		InProgress []Issue `json:"in_progress"`
		Done       []Issue `json:"done"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		return fmt.Errorf("parse fixture %s: %w", path, err)
	}
	m.InProgressIssues = fx.InProgress
	m.DoneIssues = fx.Done
	return nil
}

// InProgress returns the configured in-progress issues.
func (m *MockClient) InProgress(_ context.Context) ([]Issue, error) {
	if m.ListErr != nil {
		return nil, m.ListErr
	}
	return m.InProgressIssues, nil
}

// RecentlyDone returns done issues whose Resolved (or Updated) falls within the
// window.
func (m *MockClient) RecentlyDone(_ context.Context, within time.Duration) ([]Issue, error) {
	if m.ListErr != nil {
		return nil, m.ListErr
	}
	cutoff := time.Now().Add(-within)
	var out []Issue
	for _, i := range m.DoneIssues {
		if doneAt(i).After(cutoff) {
			out = append(out, i)
		}
	}
	return out, nil
}

// WeeklyDone buckets done issues by resolved week.
func (m *MockClient) WeeklyDone(_ context.Context, weeks int) ([]int, error) {
	if m.ListErr != nil {
		return nil, m.ListErr
	}
	counts := make([]int, weeks)
	now := time.Now()
	for _, i := range m.DoneIssues {
		age := now.Sub(doneAt(i))
		bucket := int(age / (7 * 24 * time.Hour))
		if bucket >= 0 && bucket < weeks {
			counts[bucket]++
		}
	}
	return counts, nil
}

// Get returns the first matching issue across both lists.
func (m *MockClient) Get(_ context.Context, key string) (Issue, error) {
	if m.GetErr != nil {
		return Issue{}, m.GetErr
	}
	for _, i := range append(m.InProgressIssues, m.DoneIssues...) {
		if i.Key == key {
			return i, nil
		}
	}
	return Issue{}, fmt.Errorf("issue not found: %s", key)
}

func doneAt(i Issue) time.Time {
	if !i.Resolved.IsZero() {
		return i.Resolved
	}
	return i.Updated
}
