package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "release_should_be_immutable",
		Description: "Published releases for patch versions should be immutable (via repository settings)",
		Findings:    []FindingDef{{ID: "release_not_immutable", Summary: "A published patch release is not immutable"}},
		Priority:    12,
		Category:    "releases",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if !checkEnabled(cfg.CheckImmutability) {
				return nil
			}
			dupIDs := DuplicateReleaseIDs(st)
			var items []any
			for _, r := range st.Releases {
				if r.IsDraft || r.IsIgnored || !patchTagPrefixRe.MatchString(r.TagName) || containsID(dupIDs, r.ID) {
					continue
				}
				items = append(items, r)
			}
			return items
		},
		Check: func(item any, st *state.State, cfg *state.Config) bool {
			return item.(*state.ReleaseInfo).IsImmutable
		},
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			r := item.(*state.ReleaseInfo)
			issue := state.NewIssue("release_not_immutable", releaseSeverity(cfg.CheckImmutability),
				fmt.Sprintf("Release %s is published but not immutable (repository 'Release immutability' setting may not be enabled)", r.TagName))
			issue.Version = r.TagName
			action := actions.NewRepublishReleaseAction(r.TagName)
			// Preserve latest when already latest, or make it latest when it is the highest non-prerelease.
			makeLatest := republishMakeLatest(st, r)
			applyMakeLatest(action, &makeLatest)
			issue.Action = action
			return issue
		},
	})
}

// republishMakeLatest preserves latest when already latest, or makes it latest when it is the highest non-prerelease.
func republishMakeLatest(st *state.State, r *state.ReleaseInfo) bool {
	return r.IsLatest || ShouldBeLatestRelease(st, r.TagName, r)
}
