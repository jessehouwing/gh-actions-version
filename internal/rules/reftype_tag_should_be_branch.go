package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "tag_should_be_branch",
		Description: "Floating version tags should be branches when floating-versions-use is 'branches'",
		Findings:    []FindingDef{{ID: "floating_ref_should_be_branch", Summary: "A floating version should be a branch"}},
		Priority:    5,
		Category:    "ref_type",
		// Only floating versions; patches must always be tags.
		Condition: func(st *state.State, cfg *state.Config) []any {
			if floatingUse(cfg) != "branches" {
				return nil
			}
			var out []any
			for _, t := range st.Tags {
				if !t.IsIgnored && (t.IsMajor || t.IsMinor) {
					out = append(out, t)
				}
			}
			return out
		},
		// A branch that already exists is left to the duplicate rules.
		Check: func(item any, st *state.State, cfg *state.Config) bool {
			t := item.(*state.VersionRef)
			for _, b := range st.Branches {
				if b.Version == t.Version && !b.IsIgnored {
					return true
				}
			}
			return false
		},
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			t := item.(*state.VersionRef)
			target := conversionTarget(t, st, cfg)
			issue := state.NewIssue("floating_ref_should_be_branch", state.SeverityError,
				fmt.Sprintf("Tag '%s' should be a branch (floating-versions-use: branches).%s", t.Version, conversionWarning(t, target)))
			issue.Version = t.Version
			issue.Action = actions.NewConvertTagToBranchAction(t.Version, target)
			return issue
		},
	})
}
