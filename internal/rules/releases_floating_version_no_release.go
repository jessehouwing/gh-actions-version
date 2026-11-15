package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "floating_version_no_release",
		Description: "Floating versions (vX, vX.Y, latest) should not have GitHub Releases",
		Findings: []FindingDef{
			{ID: "immutable_floating_release", Summary: "An immutable release exists for a floating version"},
			{ID: "mutable_floating_release", Summary: "A release exists for a floating version"},
		},
		Priority: 15,
		Category: "releases",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if cfg.CheckReleases == "none" && cfg.CheckImmutability == "none" {
				return nil
			}
			var items []any
			for _, r := range st.Releases {
				if r.IsIgnored {
					continue
				}
				if majorTagRe.MatchString(r.TagName) || minorTagRe.MatchString(r.TagName) || r.TagName == "latest" {
					items = append(items, r)
				}
			}
			return items
		},
		Check: func(item any, st *state.State, cfg *state.Config) bool { return false },
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			r := item.(*state.ReleaseInfo)
			if r.IsImmutable {
				issue := state.NewIssue("immutable_floating_release", state.SeverityError,
					fmt.Sprintf("Floating version %s has an immutable release that cannot be deleted. Consider using ignore-versions.", r.TagName))
				issue.Version = r.TagName
				issue.Status = state.StatusUnfixable
				return issue
			}
			issue := state.NewIssue("mutable_floating_release", mostSevere(cfg.CheckImmutability, cfg.CheckReleases),
				fmt.Sprintf("Floating version %s has a mutable release that should be removed", r.TagName))
			issue.Version = r.TagName
			issue.Action = actions.NewDeleteReleaseAction(r.TagName, r.ID)
			return issue
		},
	})
}
