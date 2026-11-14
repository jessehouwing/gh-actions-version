package rules

import (
	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "latest_tag_tracks_global_highest",
		Description: "Latest tag must point to the global highest patch version",
		Findings:    []FindingDef{{ID: "latest_tag_wrong_target", Summary: "The latest tag points to the wrong patch version"}},
		Priority:    30,
		Category:    "latest",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if cfg != nil && cfg.FloatingVersionsUse == "branches" {
				return nil
			}
			return findLatest(st.Tags)
		},
		Check: latestCheck,
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			issue, h := latestIssue("latest_tag_wrong_target", item, st, cfg)
			if issue != nil {
				issue.Action = actions.NewUpdateTagAction("latest", h.Sha, true)
			}
			return issue
		},
	})
}
