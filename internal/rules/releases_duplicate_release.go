package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "duplicate_release",
		Description: "Delete duplicate draft releases that point to the same tag as another release",
		Priority:    9,
		Category:    "releases",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if !checkEnabled(cfg.CheckReleases) {
				return nil
			}
			var items []any
			for _, r := range DuplicateDraftReleases(st) {
				items = append(items, r)
			}
			return items
		},
		Check: func(item any, st *state.State, cfg *state.Config) bool { return false },
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			r := item.(*state.ReleaseInfo)
			issue := state.NewIssue("duplicate_release", releaseSeverity(cfg.CheckReleases),
				fmt.Sprintf("Duplicate draft release found for %s (release ID: %d) - will be deleted", r.TagName, r.ID))
			issue.Version = r.TagName
			issue.Action = actions.NewDeleteReleaseAction(r.TagName, r.ID)
			return issue
		},
	})
}
