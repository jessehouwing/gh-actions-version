package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-version-action/internal/actions"
	"github.com/jessehouwing/gh-version-action/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "patch_tag_missing",
		Description: "Patch version tags should exist for all floating versions",
		Priority:    25,
		Category:    "version_tracking",
		Condition: func(st *state.State, cfg *state.Config) []any {
			// Only when check-releases is 'none'; otherwise release creation handles tags.
			if cfg.CheckReleases != "none" {
				return nil
			}
			refs := st.Branches
			if cfg.FloatingVersionsUse == "tags" {
				refs = st.Tags
			}
			var floating []*state.VersionRef
			for _, v := range refs {
				if (v.IsMajor || v.IsMinor) && !v.IsIgnored {
					floating = append(floating, v)
				}
			}
			for _, v := range refs {
				if v.Version == "latest" {
					floating = append(floating, v)
				}
			}

			all := st.AllRefs()
			hasPatch := func(match func(*state.VersionRef) bool) bool {
				for _, v := range all {
					if v.IsPatch && !v.IsIgnored && match(v) {
						return true
					}
				}
				return false
			}
			var out []any
			for _, f := range floating {
				switch {
				case f.Version == "latest":
					if !hasPatch(func(*state.VersionRef) bool { return true }) {
						out = append(out, patchItem{Floating: f, Expected: "v1.0.0"})
					}
				case f.IsMajor:
					if !hasPatch(func(v *state.VersionRef) bool { return v.Major == f.Major }) {
						out = append(out, patchItem{Floating: f, Expected: fmt.Sprintf("v%d.0.0", f.Major)})
					}
				case f.IsMinor:
					if !hasPatch(func(v *state.VersionRef) bool { return v.Major == f.Major && v.Minor == f.Minor }) {
						out = append(out, patchItem{Floating: f, Expected: fmt.Sprintf("v%d.%d.0", f.Major, f.Minor)})
					}
				}
			}
			return out
		},
		Check: func(any, *state.State, *state.Config) bool { return false },
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			it := item.(patchItem)
			issue := state.NewIssue("missing_patch_version", state.SeverityError,
				fmt.Sprintf("Floating version %s exists but no corresponding patch version found. Expected: %s", it.Floating.Version, it.Expected))
			issue.Version = it.Expected
			issue.Action = actions.NewCreateTagAction(it.Expected, it.Floating.Sha)
			return issue
		},
	})
}
