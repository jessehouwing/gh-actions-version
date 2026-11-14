package rules

import (
	"testing"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

func rtPair(v string) (*state.VersionRef, *state.VersionRef) {
	return rtTag(v, "abc123"), rtBranch(v, "abc123")
}

func TestDuplicateFloatingVersionRef(t *testing.T) {
	rule := func(t *testing.T) *Rule { return rtRule(t, "duplicate_floating_version_ref") }
	cfg := func(use string) *state.Config { return &state.Config{FloatingVersionsUse: use} }
	item := &DuplicateRef{Version: "v1"}
	item.Tag, item.Branch = rtPair("v1")

	t.Run("Condition", func(t *testing.T) {
		t.Run("returns versions that exist as both tag and branch", func(t *testing.T) {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("v1", "abc123")}
			st.Branches = []*state.VersionRef{rtBranch("v1", "abc123")}
			res := rule(t).Condition(st, &state.Config{})
			if len(res) != 1 {
				t.Fatalf("got %d", len(res))
			}
			d := res[0].(*DuplicateRef)
			if d.Version != "v1" || d.Tag.Version != "v1" || d.Branch.Version != "v1" {
				t.Fatalf("unexpected %+v", d)
			}
		})
		cases := []struct {
			name     string
			tags     []*state.VersionRef
			branches []*state.VersionRef
			want     []string
		}{
			{"returns empty when only tag exists", []*state.VersionRef{rtTag("v1", "abc123")}, nil, nil},
			{"returns empty when only branch exists", nil, []*state.VersionRef{rtBranch("v1", "abc123")}, nil},
			{"works for minor versions", []*state.VersionRef{rtTag("v1.0", "abc123")}, []*state.VersionRef{rtBranch("v1.0", "def456")}, []string{"v1.0"}},
			{"excludes ignored versions", []*state.VersionRef{rtIgnored(rtTag("v1", "abc123"))}, []*state.VersionRef{rtBranch("v1", "abc123")}, nil},
			{"finds multiple duplicates", []*state.VersionRef{rtTag("v1", "abc123"), rtTag("v2", "def456")}, []*state.VersionRef{rtBranch("v1", "abc123"), rtBranch("v2", "def456")}, []string{"v1", "v2"}},
			{"does not return patch versions", []*state.VersionRef{rtTag("v1.0.0", "abc123")}, []*state.VersionRef{rtBranch("v1.0.0", "abc123")}, nil},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				st := state.New()
				st.Tags, st.Branches = c.tags, c.branches
				rtEqual(t, rtVersions(rule(t).Condition(st, &state.Config{})), c.want)
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
			name, use, action string
		}{
			{"creates issue with DeleteBranchAction when floating-versions-use is 'tags'", "tags", "DeleteBranchAction"},
			{"creates issue with DeleteTagAction when floating-versions-use is 'branches'", "branches", "DeleteTagAction"},
			{"defaults to 'tags' mode when config not set", "", "DeleteBranchAction"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				issue := rule(t).CreateIssue(item, state.New(), cfg(c.use))
				if issue == nil || issue.Type != "duplicate_ref" || issue.Severity != "error" || issue.Version != "v1" {
					t.Fatalf("unexpected issue %+v", issue)
				}
				if rtActionType(issue.Action) != c.action || issue.Action.Version() != "v1" {
					t.Fatalf("unexpected action %T", issue.Action)
				}
			})
		}
		t.Run("message text", func(t *testing.T) {
			if m := rule(t).CreateIssue(item, state.New(), cfg("tags")).Message; m != "Version 'v1' exists as both tag and branch. Branch will be deleted (floating-versions-use: tags)" {
				t.Fatal(m)
			}
			if m := rule(t).CreateIssue(item, state.New(), cfg("branches")).Message; m != "Version 'v1' exists as both tag and branch. Tag will be deleted (floating-versions-use: branches)" {
				t.Fatal(m)
			}
		})
	})

	t.Run("Integration with Invoke-ValidationRule", func(t *testing.T) {
		cases := []struct {
			name, use, action string
			tags, branches    []*state.VersionRef
			want              int
		}{
			{"creates issues for all duplicate floating versions in tags mode", "tags", "DeleteBranchAction",
				[]*state.VersionRef{rtTag("v1", "abc123"), rtTag("v1.0", "def456")}, []*state.VersionRef{rtBranch("v1", "abc123"), rtBranch("v1.0", "def456")}, 2},
			{"creates issues for all duplicate floating versions in branches mode", "branches", "DeleteTagAction",
				[]*state.VersionRef{rtTag("v1", "abc123"), rtTag("v2", "def456")}, []*state.VersionRef{rtBranch("v1", "abc123"), rtBranch("v2", "def456")}, 2},
			{"creates no issues when no duplicates exist", "", "",
				[]*state.VersionRef{rtTag("v1", "abc123")}, []*state.VersionRef{rtBranch("v2", "def456")}, 0},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				st := state.New()
				st.Tags, st.Branches = c.tags, c.branches
				issues := Run(st, cfg(c.use), []*Rule{rule(t)})
				if len(issues) != c.want {
					t.Fatalf("got %d issues", len(issues))
				}
				for _, i := range issues {
					if rtActionType(i.Action) != c.action {
						t.Fatalf("got %T", i.Action)
					}
				}
			})
		}
	})
}
