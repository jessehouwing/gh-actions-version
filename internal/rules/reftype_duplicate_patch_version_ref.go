package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "duplicate_patch_version_ref",
		Description: "Patch versions must be tags only - branches with same version should be deleted",
		Findings:    []FindingDef{{ID: "patch_ref_duplicate", Summary: "A patch version exists as both a tag and a branch"}},
		Priority:    6,
		Category:    "ref_type",
		Condition: func(st *state.State, cfg *state.Config) []any {
			return findDuplicateRefs(st, func(v *state.VersionRef) bool { return v.IsPatch })
		},
		Check: func(item any, st *state.State, cfg *state.Config) bool { return false },
		// Patches must always be tags (immutable releases), so the branch is always deleted.
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			d := item.(*DuplicateRef)
			issue := state.NewIssue("patch_ref_duplicate", state.SeverityError,
				fmt.Sprintf("Patch version '%s' exists as both tag and branch. Branch will be deleted (patches must be tags for immutable releases)", d.Version))
			issue.Version = d.Version
			issue.Action = actions.NewDeleteBranchAction(d.Version)
			return issue
		},
	})
}
