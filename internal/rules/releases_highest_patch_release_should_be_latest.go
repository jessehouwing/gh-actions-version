package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// latestReleaseItem carries the expected and current "latest" releases.
type latestReleaseItem struct {
	ExpectedLatest *state.ReleaseInfo
	CurrentLatest  *state.ReleaseInfo
}

func init() {
	Register(&Rule{
		Name:        "highest_patch_release_should_be_latest",
		Description: "Ensures the correct release is marked as 'latest' (highest non-prerelease patch)",
		Priority:    15,
		Category:    "releases",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if !checkEnabled(cfg.CheckReleases) {
				return nil
			}
			var expected *state.ReleaseInfo
			var eMaj, eMin, ePat int
			for _, r := range st.Releases {
				if r.IsDraft || r.IsPrerelease || r.IsIgnored {
					continue
				}
				maj, min, pat, ok := parsePatchTag(r.TagName)
				if !ok {
					continue
				}
				if expected == nil || maj > eMaj || (maj == eMaj && min > eMin) || (maj == eMaj && min == eMin && pat > ePat) {
					expected, eMaj, eMin, ePat = r, maj, min, pat
				}
			}
			if expected == nil {
				return nil
			}
			var current *state.ReleaseInfo
			for _, r := range st.Releases {
				if r.IsLatest {
					current = r
					break
				}
			}
			return []any{&latestReleaseItem{ExpectedLatest: expected, CurrentLatest: current}}
		},
		Check: func(item any, st *state.State, cfg *state.Config) bool {
			it := item.(*latestReleaseItem)
			if it.CurrentLatest != nil && it.ExpectedLatest.TagName == it.CurrentLatest.TagName {
				return true
			}
			return it.ExpectedLatest.IsLatest
		},
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			it := item.(*latestReleaseItem)
			version := it.ExpectedLatest.TagName
			var msg string
			if it.CurrentLatest != nil {
				msg = fmt.Sprintf("Release %s should be marked as 'latest', but %s is currently marked as latest", version, it.CurrentLatest.TagName)
			} else {
				msg = fmt.Sprintf("Release %s should be marked as 'latest', but no release is currently marked as latest", version)
			}
			issue := state.NewIssue("wrong_latest_release", releaseSeverity(cfg.CheckReleases), msg)
			issue.Version = version
			issue.Action = actions.NewSetLatestReleaseAction(version, it.ExpectedLatest.ID)
			return issue
		},
	})
}
