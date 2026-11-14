package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "minor_tag_missing",
		Description: "Minor version tags should exist for all major.minor versions that have patches",
		Priority:    23,
		Category:    "version_tracking",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if floatingIsBranches(cfg) || cfg.CheckMinorVersion == "none" {
				return nil
			}
			var out []any
			for _, m := range missingMinorsFromPatches(st, cfg) {
				out = append(out, m)
			}
			// Major tags without any patches need a v{major}.0 minor tag.
			patches := eligiblePatches(st, cfg)
			for _, mt := range st.Tags {
				if !mt.IsMajor || mt.IsIgnored {
					continue
				}
				hasPatches := false
				for _, p := range patches {
					if p.Major == mt.Major {
						hasPatches = true
						break
					}
				}
				if hasPatches || refExists(st, fmt.Sprintf("v%d.0", mt.Major)) {
					continue
				}
				out = append(out, minorItem{Major: mt.Major, Minor: 0, SourceSha: mt.Sha})
			}
			return out
		},
		Check: func(any, *state.State, *state.Config) bool { return false },
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			it := item.(minorItem)
			version := fmt.Sprintf("v%d.%d", it.Major, it.Minor)
			var targetSha, targetVersion string
			if hp := HighestPatchForMinor(st, it.Major, it.Minor, cfg.IgnorePreviewReleases); hp != nil {
				targetSha, targetVersion = hp.Sha, hp.Version
			} else if it.SourceSha != "" {
				targetSha, targetVersion = it.SourceSha, fmt.Sprintf("v%d", it.Major)
			} else {
				for _, t := range st.Tags {
					if t.Version == fmt.Sprintf("v%d", it.Major) {
						targetSha, targetVersion = t.Sha, t.Version
						break
					}
				}
			}
			issue := state.NewIssue("missing_minor_version", minorSeverity(cfg),
				fmt.Sprintf("Minor version tag %s does not exist. It should point to %s at %s", version, targetVersion, targetSha))
			issue.Version = version
			issue.ExpectedSha = targetSha
			issue.Action = actions.NewCreateTagAction(version, targetSha)
			return issue
		},
	})
}
