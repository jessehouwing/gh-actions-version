package rules

import (
	"testing"
	"time"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func TestIgnoreEntriesHygiene(t *testing.T) {
	st := state.New()
	st.CheckedAt = time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC)
	st.Tags = []*state.VersionRef{state.NewVersionRef("v1.0.0", "refs/tags/v1.0.0", "sha", state.TypeTag)}
	st.Releases = []*state.ReleaseInfo{{TagName: "v2.0.0"}}
	cfg := state.Config{IgnoreEntries: []state.IgnoreEntry{
		{Versions: []string{"v1.*"}, Until: "2028-02-29"},
		{Versions: []string{"v2.*"}},
		{Versions: []string{"v3.*"}, Until: "2028-02-28"},
		{Versions: []string{"v4.*"}},
	}}
	for _, rule := range All() {
		if rule.Name == "ignore_entries_valid" {
			Run(st, &cfg, []*Rule{rule})
			if len(st.Issues) != 2 || st.Issues[0].Finding != "ignore_expired" || st.Issues[1].Finding != "ignore_unused" {
				t.Fatalf("hygiene findings = %+v", st.Issues)
			}
			for _, issue := range st.Issues {
				if issue.Severity != state.SeverityWarning || issue.Version != "" {
					t.Fatalf("hygiene issue = %+v", issue)
				}
			}
			return
		}
	}
	t.Fatal("ignore_entries_valid rule not registered")
}
