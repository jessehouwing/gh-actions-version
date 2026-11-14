package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "patch_release_required",
		Description: "Patch versions must have GitHub Releases when check-releases is enabled",
		Findings: []FindingDef{
			{ID: "release_missing", Summary: "A patch version does not have a GitHub Release"},
			{ID: "patch_tag_locked_by_deleted_release", Summary: "A patch tag is locked by a deleted immutable release"},
		},
		Priority: 10,
		Category: "releases",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if !checkEnabled(cfg.CheckReleases) {
				return nil
			}
			seen := map[string]bool{}
			var results []any

			refs := append(append([]*state.VersionRef(nil), st.Tags...), st.Branches...)
			var allPatches []*state.VersionRef
			for _, r := range refs {
				if r.IsPatch {
					allPatches = append(allPatches, r)
				}
			}

			for _, p := range allPatches {
				if p.IsIgnored || st.FindRelease(p.Version) != nil || seen[p.Version] {
					continue
				}
				seen[p.Version] = true
				results = append(results, p)
			}

			for _, f := range refs {
				if f.IsPatch || f.Version == "latest" || f.IsIgnored {
					continue
				}
				var expected string
				switch {
				case f.IsMajor:
					expected = fmt.Sprintf("v%d.0.0", f.Major)
				case f.IsMinor:
					expected = fmt.Sprintf("v%d.%d.0", f.Major, f.Minor)
				default:
					continue
				}
				if seen[expected] {
					continue
				}

				// Any existing patch in the floating scope satisfies it; those are validated above.
				hasPatch := false
				for _, p := range allPatches {
					if p.IsIgnored {
						continue
					}
					if (f.IsMajor && p.Major == f.Major) || (f.IsMinor && p.Major == f.Major && p.Minor == f.Minor) {
						hasPatch = true
						break
					}
				}
				if hasPatch {
					continue
				}

				// A draft release only needs publishing, which release_should_be_published handles.
				if rel := st.FindRelease(expected); rel != nil && rel.IsDraft {
					continue
				}
				seen[expected] = true
				results = append(results, state.NewVersionRef(expected, "refs/tags/"+expected, f.Sha, state.TypeTag))
			}
			return results
		},
		Check: func(item any, st *state.State, cfg *state.Config) bool { return false },
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			v := item.(*state.VersionRef)
			autoPublish := checkEnabled(cfg.CheckImmutability)
			issue := state.NewIssue("release_missing", releaseSeverity(cfg.CheckReleases),
				fmt.Sprintf("Release required for patch version %s", v.Version))
			issue.Version = v.Version
			action := actions.NewCreateReleaseAction(v.Version, !autoPublish, autoPublish, v.Sha)
			applyMakeLatest(action, makeLatestUnlessHighest(st, v.Version, nil))
			issue.Action = action
			return issue
		},
	})
}
