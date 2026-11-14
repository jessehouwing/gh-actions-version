package rules

import "github.com/jessehouwing/gh-actions-version/internal/state"

func init() {
	Register(&Rule{
		Name:        "major_tag_tracks_highest_patch",
		Description: "Major version tags must point to the highest patch version",
		Priority:    20,
		Category:    "version_tracking",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if floatingIsBranches(cfg) {
				return nil
			}
			var out []any
			for _, v := range st.Tags {
				if v.IsMajor && !v.IsIgnored {
					out = append(out, v)
				}
			}
			return out
		},
		Check: majorTracksCheck,
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			return majorTracksIssue(item, st, cfg, false)
		},
	})
}
