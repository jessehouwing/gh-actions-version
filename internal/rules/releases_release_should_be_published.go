package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "release_should_be_published",
		Description: "Draft releases for patch versions should be published when check-releases or check-release-immutability is enabled",
		Findings:    []FindingDef{{ID: "release_is_draft", Summary: "A patch release is still a draft"}},
		Priority:    11,
		Category:    "releases",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if !checkEnabled(cfg.CheckReleases) && !checkEnabled(cfg.CheckImmutability) {
				return nil
			}
			var dupIDs []int64
			for _, d := range DuplicateDraftReleases(st) {
				dupIDs = append(dupIDs, d.ID)
			}
			var items []any
			for _, r := range st.Releases {
				if !r.IsDraft || r.IsIgnored || !patchTagPrefixRe.MatchString(r.TagName) || containsID(dupIDs, r.ID) {
					continue
				}
				items = append(items, r)
			}
			return items
		},
		Check: func(item any, st *state.State, cfg *state.Config) bool { return false },
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			r := item.(*state.ReleaseInfo)
			issue := state.NewIssue("release_is_draft", mostSevere(cfg.CheckImmutability, cfg.CheckReleases),
				fmt.Sprintf("Release %s is still in draft status and should be published", r.TagName))
			issue.Version = r.TagName
			action := actions.NewPublishReleaseAction(r.TagName, r.ID)
			applyMakeLatest(action, makeLatestUnlessHighest(st, r.TagName, r))
			issue.Action = action
			return issue
		},
	})
}
