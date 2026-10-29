package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-version-action/internal/actions"
	"github.com/jessehouwing/gh-version-action/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "minor_branch_missing",
		Description: "Minor version branches should exist for all major.minor versions that have patches",
		Priority:    23,
		Category:    "version_tracking",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if !floatingIsBranches(cfg) || cfg.CheckMinorVersion == "none" {
				return nil
			}
			var out []any
			for _, m := range missingMinorsFromPatches(st, cfg) {
				out = append(out, m)
			}
			return out
		},
		Check: func(any, *state.State, *state.Config) bool { return false },
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			it := item.(minorItem)
			version := fmt.Sprintf("v%d.%d", it.Major, it.Minor)
			hp := HighestPatchForMinor(st, it.Major, it.Minor, cfg.IgnorePreviewReleases)
			issue := state.NewIssue("missing_minor_version", minorSeverity(cfg),
				fmt.Sprintf("Minor version branch %s does not exist. It should point to %s at %s", version, versionOf(hp), shaOf(hp)))
			issue.Version = version
			issue.ExpectedSha = shaOf(hp)
			issue.Action = actions.NewCreateBranchAction(version, shaOf(hp))
			return issue
		},
	})
}
