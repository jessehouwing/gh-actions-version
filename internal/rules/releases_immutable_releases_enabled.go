package rules

import (
	"github.com/jessehouwing/gh-version-action/internal/actions"
	"github.com/jessehouwing/gh-version-action/internal/state"
)

func init() {
	Register(&Rule{
		Name:        "immutable_releases_enabled",
		Description: "The repository setting 'Release immutability' should be enabled so published releases become immutable",
		Priority:    11,
		Category:    "releases",
		Condition: func(st *state.State, cfg *state.Config) []any {
			// An unknown setting (no API access, local mode) is not reported.
			if !checkEnabled(cfg.CheckImmutability) || st.ImmutableReleasesEnabled == nil {
				return nil
			}
			return []any{st}
		},
		Check: func(item any, st *state.State, cfg *state.Config) bool {
			return *st.ImmutableReleasesEnabled
		},
		CreateIssue: func(item any, st *state.State, cfg *state.Config) *state.Issue {
			issue := state.NewIssue("immutable_releases_disabled", releaseSeverity(cfg.CheckImmutability),
				"The repository setting 'Release immutability' is disabled, so published releases are not immutable")
			issue.Action = actions.NewEnableImmutableReleasesAction()
			return issue
		},
	})
}
