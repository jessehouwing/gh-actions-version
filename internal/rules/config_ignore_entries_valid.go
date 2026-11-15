package rules

import (
	"fmt"
	"strings"

	"github.com/jessehouwing/gh-actions-version/internal/state"
	versionutil "github.com/jessehouwing/gh-actions-version/internal/version"
)

type ignoreHygieneItem struct {
	entry   state.IgnoreEntry
	expired bool
}

func init() {
	Register(&Rule{
		Name:        "ignore_entries_valid",
		Description: "Ignore entries should not be expired or unused",
		Findings: []FindingDef{
			{ID: "ignore_expired", Summary: "An ignore entry has expired"},
			{ID: "ignore_unused", Summary: "An ignore entry matches no version"},
		},
		Priority: 100,
		Category: "config",
		Condition: func(st *state.State, cfg *state.Config) []any {
			var items []any
			date := st.CheckedAt.UTC().Format("2006-01-02")
			for _, entry := range cfg.IgnoreEntries {
				if entry.Until != "" && date > entry.Until {
					items = append(items, ignoreHygieneItem{entry: entry, expired: true})
					continue
				}
				matched := false
				for _, ref := range st.AllRefs() {
					matched = matched || versionutil.ShouldBeIgnored(ref.Version, entry.Versions)
				}
				for _, release := range st.Releases {
					matched = matched || versionutil.ShouldBeIgnored(release.TagName, entry.Versions)
				}
				if !matched {
					items = append(items, ignoreHygieneItem{entry: entry})
				}
			}
			return items
		},
		Check: func(any, *state.State, *state.Config) bool { return false },
		CreateIssue: func(item any, _ *state.State, _ *state.Config) *state.Issue {
			i := item.(ignoreHygieneItem)
			patterns := strings.Join(i.entry.Versions, ", ")
			if i.expired {
				return state.NewIssue("ignore_expired", state.SeverityWarning,
					fmt.Sprintf("Ignore entry %s expired after %s; review or remove it.", patterns, i.entry.Until))
			}
			return state.NewIssue("ignore_unused", state.SeverityWarning,
				fmt.Sprintf("Ignore entry %s matches no existing version; review or remove it.", patterns))
		},
	})
}
