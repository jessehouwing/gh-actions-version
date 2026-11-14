package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-version-action/internal/actions"
	"github.com/jessehouwing/gh-version-action/internal/state"
)

func latestHighest(st *state.State, cfg *state.Config) *state.VersionRef {
	return GlobalHighestPatch(st, cfg != nil && cfg.IgnorePreviewReleases)
}

func findLatest(refs []*state.VersionRef) []any {
	for _, r := range refs {
		if r.Version == "latest" && !r.IsIgnored {
			return []any{r}
		}
	}
	return nil
}

// latestCheck passes when there is nothing to track or latest points at the global highest patch.
func latestCheck(item any, st *state.State, cfg *state.Config) bool {
	h := latestHighest(st, cfg)
	return h == nil || item.(*state.VersionRef).Sha == h.Sha
}

func latestIssue(typ string, item any, st *state.State, cfg *state.Config) (*state.Issue, *state.VersionRef) {
	latest := item.(*state.VersionRef)
	h := latestHighest(st, cfg)
	if h == nil {
		return nil, nil
	}
	issue := state.NewIssue(typ, state.SeverityError,
		fmt.Sprintf("latest points to %s but should point to %s at %s", latest.Sha, h.Version, h.Sha))
	issue.Version = "latest"
	issue.CurrentSha = latest.Sha
	issue.ExpectedSha = h.Sha
	return issue, h
}

func init() {
	Register(&Rule{
		Name:        "latest_branch_tracks_global_highest",
		Description: "Latest branch must point to the global highest patch version",
		Priority:    30,
		Category:    "latest",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if cfg == nil || cfg.FloatingVersionsUse != "branches" {
				return nil
			}
			return findLatest(st.Branches)
		},
		Check: latestCheck,
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			issue, h := latestIssue("incorrect_latest_branch", item, st, cfg)
			if issue != nil {
				issue.Action = actions.NewUpdateBranchAction("latest", h.Sha, true)
			}
			return issue
		},
	})
}
