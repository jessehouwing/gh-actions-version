package rules

import (
	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "duplicate_latest_ref",
		Description: "The 'latest' version should not exist as both tag and branch simultaneously",
		Priority:    6,
		Category:    "ref_type",
		Condition: func(st *state.State, cfg *state.Config) []any {
			return findDuplicateRefs(st, func(v *state.VersionRef) bool { return v.Version == "latest" })
		},
		Check: func(item any, st *state.State, cfg *state.Config) bool { return false },
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			var issue *state.Issue
			if floatingUse(cfg) == "tags" {
				issue = state.NewIssue("duplicate_latest_ref", state.SeverityError,
					"'latest' exists as both tag and branch. Branch will be deleted (floating-versions-use: tags)")
				issue.Action = actions.NewDeleteBranchAction("latest")
			} else {
				issue = state.NewIssue("duplicate_latest_ref", state.SeverityError,
					"'latest' exists as both tag and branch. Tag will be deleted (floating-versions-use: branches)")
				issue.Action = actions.NewDeleteTagAction("latest")
			}
			issue.Version = "latest"
			return issue
		},
	})
}
