package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// DuplicateRef is a version that exists as both tag and branch.
type DuplicateRef struct {
	Version string
	Tag     *state.VersionRef
	Branch  *state.VersionRef
}

// findDuplicateRefs returns tags (matching pred) that also exist as a non-ignored branch.
func findDuplicateRefs(st *state.State, pred func(*state.VersionRef) bool) []any {
	var out []any
	for _, t := range st.Tags {
		if t.IsIgnored || !pred(t) {
			continue
		}
		for _, b := range st.Branches {
			if b.Version == t.Version && !b.IsIgnored {
				out = append(out, &DuplicateRef{Version: t.Version, Tag: t, Branch: b})
				break
			}
		}
	}
	return out
}

func init() {
	Register(&Rule{
		Name:        "duplicate_floating_version_ref",
		Description: "Floating versions should not exist as both tag and branch simultaneously",
		Findings:    []FindingDef{{ID: "floating_tag_and_branch_both_exist", Summary: "A floating version exists as both a tag and a branch"}},
		Priority:    6,
		Category:    "ref_type",
		Condition: func(st *state.State, cfg *state.Config) []any {
			return findDuplicateRefs(st, func(v *state.VersionRef) bool { return v.IsMajor || v.IsMinor })
		},
		// Being in the condition output means it is a duplicate.
		Check: func(item any, st *state.State, cfg *state.Config) bool { return false },
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			d := item.(*DuplicateRef)
			var issue *state.Issue
			if floatingUse(cfg) == "tags" {
				issue = state.NewIssue("floating_tag_and_branch_both_exist", state.SeverityError,
					fmt.Sprintf("Version '%s' exists as both tag and branch. Branch will be deleted (floating-versions-use: tags)", d.Version))
				issue.Action = actions.NewDeleteBranchAction(d.Version)
			} else {
				issue = state.NewIssue("floating_tag_and_branch_both_exist", state.SeverityError,
					fmt.Sprintf("Version '%s' exists as both tag and branch. Tag will be deleted (floating-versions-use: branches)", d.Version))
				issue.Action = actions.NewDeleteTagAction(d.Version)
			}
			issue.Version = d.Version
			return issue
		},
	})
}
