package rules

import (
	"testing"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

func TestDuplicatePatchVersionRef(t *testing.T) {
	rule := func(t *testing.T) *Rule { return rtRule(t, "duplicate_patch_version_ref") }
	cfg := func(use string) *state.Config { return &state.Config{FloatingVersionsUse: use} }
	item := &DuplicateRef{Version: "v1.0.0"}
	item.Tag, item.Branch = rtPair("v1.0.0")

	t.Run("Condition", func(t *testing.T) {
		t.Run("returns patch versions that exist as both tag and branch", func(t *testing.T) {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("v1.0.0", "abc123")}
			st.Branches = []*state.VersionRef{rtBranch("v1.0.0", "abc123")}
			res := rule(t).Condition(st, &state.Config{})
			if len(res) != 1 {
				t.Fatalf("got %d", len(res))
			}
			d := res[0].(*DuplicateRef)
			if d.Version != "v1.0.0" || d.Tag.Version != "v1.0.0" || d.Branch.Version != "v1.0.0" {
				t.Fatalf("unexpected %+v", d)
			}
		})
		cases := []struct {
			name     string
			tags     []*state.VersionRef
			branches []*state.VersionRef
			want     []string
		}{
			{"returns empty when only tag exists", []*state.VersionRef{rtTag("v1.0.0", "abc123")}, nil, nil},
			{"returns empty when only branch exists", nil, []*state.VersionRef{rtBranch("v1.0.0", "abc123")}, nil},
			{"excludes ignored versions", []*state.VersionRef{rtIgnored(rtTag("v1.0.0", "abc123"))}, []*state.VersionRef{rtBranch("v1.0.0", "abc123")}, nil},
			{"excludes ignored branches", []*state.VersionRef{rtTag("v1.0.0", "abc123")}, []*state.VersionRef{rtIgnored(rtBranch("v1.0.0", "abc123"))}, nil},
			{"finds multiple duplicate patches", []*state.VersionRef{rtTag("v1.0.0", "abc123"), rtTag("v2.0.0", "def456")}, []*state.VersionRef{rtBranch("v1.0.0", "abc123"), rtBranch("v2.0.0", "def456")}, []string{"v1.0.0", "v2.0.0"}},
			{"does not return floating versions", []*state.VersionRef{rtTag("v1", "abc123"), rtTag("v1.0", "def456")}, []*state.VersionRef{rtBranch("v1", "abc123"), rtBranch("v1.0", "def456")}, nil},
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
		t.Run("creates issue with DeleteBranchAction (always delete branch)", func(t *testing.T) {
			issue := rule(t).CreateIssue(item, state.New(), cfg("tags"))
			if issue == nil || issue.Type != "duplicate_patch_ref" || issue.Severity != "error" || issue.Version != "v1.0.0" {
				t.Fatalf("unexpected issue %+v", issue)
			}
			if rtActionType(issue.Action) != "DeleteBranchAction" || issue.Action.Version() != "v1.0.0" {
				t.Fatalf("unexpected action %T", issue.Action)
			}
			want := "Patch version 'v1.0.0' exists as both tag and branch. Branch will be deleted (patches must be tags for immutable releases)"
			if issue.Message != want {
				t.Fatal(issue.Message)
			}
		})
		t.Run("always deletes branch regardless of floating-versions-use setting", func(t *testing.T) {
			issue := rule(t).CreateIssue(item, state.New(), cfg("branches"))
			if issue == nil || rtActionType(issue.Action) != "DeleteBranchAction" {
				t.Fatalf("unexpected %+v", issue)
			}
		})
	})

	t.Run("Integration with Invoke-ValidationRule", func(t *testing.T) {
		t.Run("creates issues for all duplicate patches", func(t *testing.T) {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("v1.0.0", "abc123"), rtTag("v2.0.0", "def456")}
			st.Branches = []*state.VersionRef{rtBranch("v1.0.0", "abc123"), rtBranch("v2.0.0", "def456")}
			issues := Run(st, cfg("tags"), []*Rule{rule(t)})
			rtEqual(t, rtIssueVersions(issues), []string{"v1.0.0", "v2.0.0"})
		})
		t.Run("creates no issues when no duplicates exist", func(t *testing.T) {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("v1.0.0", "abc123"), rtTag("v2.0.0", "def456")}
			if issues := Run(st, &state.Config{}, []*Rule{rule(t)}); len(issues) != 0 {
				t.Fatalf("got %d", len(issues))
			}
		})
	})
}
