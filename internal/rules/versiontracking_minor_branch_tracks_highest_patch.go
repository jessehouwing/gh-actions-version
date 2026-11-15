package rules

import "github.com/jessehouwing/gh-actions-version/internal/state"

func init() {
	Register(&Rule{
		Name:        "minor_branch_tracks_highest_patch",
		Description: "Minor version branches must point to the highest patch version within that minor series",
		Findings:    []FindingDef{{ID: "minor_branch_wrong_target", Summary: "A minor version branch points to the wrong patch version"}},
		Priority:    22,
		Category:    "version_tracking",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if !floatingIsBranches(cfg) || cfg.CheckMinorVersion == "none" {
				return nil
			}
			var out []any
			for _, v := range st.Branches {
				if v.IsMinor && !v.IsIgnored {
					out = append(out, v)
				}
			}
			return out
		},
		Check: minorTracksCheck,
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			return minorTracksIssue(item, st, cfg, true)
		},
	})
}
