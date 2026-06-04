package jira

import "sort"

// SortByAgeOldestFirst orders issues so the least-recently-updated (oldest,
// most stale) appear first. Ties break by key for stable output.
func SortByAgeOldestFirst(issues []Issue) {
	sort.SliceStable(issues, func(a, b int) bool {
		if !issues[a].Updated.Equal(issues[b].Updated) {
			return issues[a].Updated.Before(issues[b].Updated)
		}
		return issues[a].Key < issues[b].Key
	})
}

// SortByUpdatedNewestFirst orders issues most-recently-updated first.
func SortByUpdatedNewestFirst(issues []Issue) {
	sort.SliceStable(issues, func(a, b int) bool {
		if !issues[a].Updated.Equal(issues[b].Updated) {
			return issues[a].Updated.After(issues[b].Updated)
		}
		return issues[a].Key < issues[b].Key
	})
}

// SortByCreatedNewestFirst orders issues most-recently-created first.
func SortByCreatedNewestFirst(issues []Issue) {
	sort.SliceStable(issues, func(a, b int) bool {
		if !issues[a].Created.Equal(issues[b].Created) {
			return issues[a].Created.After(issues[b].Created)
		}
		return issues[a].Key < issues[b].Key
	})
}
