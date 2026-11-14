package rules

import "github.com/jessehouwing/gh-actions-version/internal/state"

func init() {
	Register(&Rule{
		Name:        "major_branch_missing",
		Description: "Major version branches should exist for all major versions that have patches",
		Priority:    21,
		Category:    "version_tracking",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if !floatingIsBranches(cfg) {
				return nil
			}
			return missingMajors(st, cfg)
		},
		Check: func(any, *state.State, *state.Config) bool { return false },
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			return majorMissingIssue(item, st, cfg, true)
		},
	})
}
