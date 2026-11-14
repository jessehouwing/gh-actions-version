package rules

import (
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func TestDuplicateLatestRef(t *testing.T) {
	rule := func(t *testing.T) *Rule { return rtRule(t, "duplicate_latest_ref") }
	cfg := func(use string) *state.Config { return &state.Config{FloatingVersionsUse: use} }
	item := &DuplicateRef{Version: "latest"}
	item.Tag, item.Branch = rtPair("latest")

	t.Run("Condition", func(t *testing.T) {
		t.Run("returns 'latest' when it exists as both tag and branch", func(t *testing.T) {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("latest", "abc123")}
			st.Branches = []*state.VersionRef{rtBranch("latest", "abc123")}
			res := rule(t).Condition(st, &state.Config{})
			if len(res) != 1 {
				t.Fatalf("got %d", len(res))
			}
			d := res[0].(*DuplicateRef)
			if d.Version != "latest" || d.Tag.Version != "latest" || d.Branch.Version != "latest" {
				t.Fatalf("unexpected %+v", d)
			}
		})
		cases := []struct {
			name     string
			tags     []*state.VersionRef
			branches []*state.VersionRef
		}{
			{"returns empty when only tag exists", []*state.VersionRef{rtTag("latest", "abc123")}, nil},
			{"returns empty when only branch exists", nil, []*state.VersionRef{rtBranch("latest", "abc123")}},
			{"returns empty when neither exists", nil, nil},
			{"excludes ignored latest tag", []*state.VersionRef{rtIgnored(rtTag("latest", "abc123"))}, []*state.VersionRef{rtBranch("latest", "abc123")}},
			{"excludes ignored latest branch", []*state.VersionRef{rtTag("latest", "abc123")}, []*state.VersionRef{rtIgnored(rtBranch("latest", "abc123"))}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				st := state.New()
				st.Tags, st.Branches = c.tags, c.branches
				if res := rule(t).Condition(st, &state.Config{}); len(res) != 0 {
					t.Fatalf("got %d", len(res))
				}
			})
		}
	})

	t.Run("Check", func(t *testing.T) {
		t.Run("always returns false since duplicate existence is the issue", func(t *testing.T) {
			if rule(t).Check(item, state.New(), &state.Config{}) {
				t.Fatal("expected false")
			}
		})
	})

	t.Run("CreateIssue", func(t *testing.T) {
		cases := []struct {
			name, use, action, msg string
		}{
			{"creates issue with DeleteBranchAction when floating-versions-use is 'tags'", "tags", "DeleteBranchAction", "'latest' exists as both tag and branch. Branch will be deleted (floating-versions-use: tags)"},
			{"creates issue with DeleteTagAction when floating-versions-use is 'branches'", "branches", "DeleteTagAction", "'latest' exists as both tag and branch. Tag will be deleted (floating-versions-use: branches)"},
			{"defaults to 'tags' mode when config not set", "", "DeleteBranchAction", "'latest' exists as both tag and branch. Branch will be deleted (floating-versions-use: tags)"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				issue := rule(t).CreateIssue(item, state.New(), cfg(c.use))
				if issue == nil || issue.Type != "duplicate_latest_ref" || issue.Severity != "error" || issue.Version != "latest" || issue.Message != c.msg {
					t.Fatalf("unexpected issue %+v", issue)
				}
				if rtActionType(issue.Action) != c.action || issue.Action.Version() != "latest" {
					t.Fatalf("unexpected action %T", issue.Action)
				}
			})
		}
	})

	t.Run("Integration with Invoke-ValidationRule", func(t *testing.T) {
		both := func() *state.State {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("latest", "abc123")}
			st.Branches = []*state.VersionRef{rtBranch("latest", "abc123")}
			return st
		}
		for _, c := range []struct{ name, use, action string }{
			{"creates issue when latest exists as both tag and branch in tags mode", "tags", "DeleteBranchAction"},
			{"creates issue when latest exists as both tag and branch in branches mode", "branches", "DeleteTagAction"},
		} {
			t.Run(c.name, func(t *testing.T) {
				issues := Run(both(), cfg(c.use), []*Rule{rule(t)})
				if len(issues) != 1 || issues[0].Version != "latest" || rtActionType(issues[0].Action) != c.action {
					t.Fatalf("unexpected %+v", issues)
				}
			})
		}
		t.Run("creates no issues when only one ref type exists", func(t *testing.T) {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("latest", "abc123")}
			if issues := Run(st, &state.Config{}, []*Rule{rule(t)}); len(issues) != 0 {
				t.Fatalf("got %d", len(issues))
			}
		})
	})
}
