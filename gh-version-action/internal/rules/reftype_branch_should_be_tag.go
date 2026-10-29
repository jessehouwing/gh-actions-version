package rules

import (
	"fmt"

	"github.com/jessehouwing/gh-version-action/internal/actions"
	"github.com/jessehouwing/gh-version-action/internal/state"
)

// floatingUse returns floating-versions-use, defaulting to "tags".
func floatingUse(cfg *state.Config) string {
	if cfg == nil || cfg.FloatingVersionsUse == "" {
		return "tags"
	}
	return cfg.FloatingVersionsUse
}

// conversionTarget returns the SHA a converted floating ref should point to (highest patch for floating refs).
func conversionTarget(ref *state.VersionRef, st *state.State, cfg *state.Config) string {
	exclude := cfg != nil && cfg.IgnorePreviewReleases
	var highestPatch *state.VersionRef
	switch {
	case ref.IsMajor:
		highestPatch = HighestPatchForMajor(st, ref.Major, exclude)
	case ref.IsMinor:
		highestPatch = HighestPatchForMinor(st, ref.Major, ref.Minor, exclude)
	}
	if highestPatch != nil {
		return highestPatch.Sha
	}
	return ref.Sha
}

func conversionWarning(ref *state.VersionRef, target string) string {
	if target == ref.Sha {
		return ""
	}
	return fmt.Sprintf(" WARNING: Conversion will change SHA from %s to %s.", ref.Sha, target)
}

func init() {
	Register(&Rule{
		Name:        "branch_should_be_tag",
		Description: "Floating version and patch version branches should be tags when floating-versions-use is 'tags'",
		Priority:    5,
		Category:    "ref_type",
		Condition: func(st *state.State, cfg *state.Config) []any {
			if floatingUse(cfg) != "tags" {
				return nil
			}
			var out []any
			for _, b := range st.Branches {
				if !b.IsIgnored && (b.IsPatch || b.IsMajor || b.IsMinor) {
					out = append(out, b)
				}
			}
			return out
		},
		// A tag that already exists is left to the duplicate rules.
		Check: func(item any, st *state.State, cfg *state.Config) bool {
			b := item.(*state.VersionRef)
			for _, t := range st.Tags {
				if t.Version == b.Version && !t.IsIgnored {
					return true
				}
			}
			return false
		},
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			b := item.(*state.VersionRef)
			target := conversionTarget(b, st, cfg)
			issue := state.NewIssue("wrong_ref_type", state.SeverityError,
				fmt.Sprintf("Branch '%s' should be a tag (floating-versions-use: tags).%s", b.Version, conversionWarning(b, target)))
			issue.Version = b.Version
			issue.Action = actions.NewConvertBranchToTagAction(b.Version, target)
			return issue
		},
	})
}
