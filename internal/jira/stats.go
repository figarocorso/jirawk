package jira

import (
	"context"
	"time"
)

// Stats summarises the user's in-progress count and weekly resolved history.
type Stats struct {
	InProgress int   `json:"in_progress"`
	Weeks      []int `json:"weeks"`      // index 0 = most recent week
	DoneTotal  int   `json:"done_total"` // sum of Weeks
}

// DefaultWeeks is the look-back window for the resolved-issues breakdown.
const DefaultWeeks = 8

// ComputeStats gathers the in-progress count and the weekly resolved breakdown.
func ComputeStats(ctx context.Context, c Client, weeks int) (Stats, error) {
	if weeks <= 0 {
		weeks = DefaultWeeks
	}
	inProgress, err := c.InProgress(ctx)
	if err != nil {
		return Stats{}, err
	}
	wk, err := c.WeeklyDone(ctx, weeks)
	if err != nil {
		return Stats{}, err
	}
	st := Stats{InProgress: len(inProgress), Weeks: wk}
	for _, n := range wk {
		st.DoneTotal += n
	}
	return st, nil
}

// WeekLabel renders the calendar range for week bucket n relative to now,
// e.g. "Jun 02–Jun 08". Index 0 is the current week.
func WeekLabel(now time.Time, n int) string {
	end := now.AddDate(0, 0, -7*n)
	start := end.AddDate(0, 0, -7)
	return start.Format("Jan 02") + "–" + end.Format("Jan 02")
}
