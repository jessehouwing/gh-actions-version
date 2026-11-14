package rules

import (
	"testing"

	"github.com/jessehouwing/gh-version-action/internal/actions"
	"github.com/jessehouwing/gh-version-action/internal/state"
)

func TestTagShouldBeBranch(t *testing.T) {
	rule := func(t *testing.T) *Rule { return rtRule(t, "tag_should_be_branch") }
	cfg := func(use string) *state.Config { return &state.Config{FloatingVersionsUse: use} }

	t.Run("Condition", func(t *testing.T) {
		cases := []struct {
			name string
			tags []*state.VersionRef
			cfg  *state.Config
			want []string
		}{
			{"returns major tags when floating-versions-use is 'branches'", []*state.VersionRef{rtTag("v1", "abc123")}, cfg("branches"), []string{"v1"}},
			{"returns minor tags when floating-versions-use is 'branches'", []*state.VersionRef{rtTag("v1.0", "abc123")}, cfg("branches"), []string{"v1.0"}},
			{"does not return patch tags when floating-versions-use is 'branches'", []*state.VersionRef{rtTag("v1.0.0", "abc123")}, cfg("branches"), nil},
			{"returns empty array when floating-versions-use is 'tags'", []*state.VersionRef{rtTag("v1", "abc123"), rtTag("v1.0", "abc123")}, cfg("tags"), nil},
			{"excludes ignored versions", []*state.VersionRef{rtIgnored(rtTag("v1", "abc123")), rtTag("v2", "def456")}, cfg("branches"), []string{"v2"}},
			{"uses 'tags' as default (returns empty) when config is not set", []*state.VersionRef{rtTag("v1", "abc123")}, &state.Config{}, nil},
			{"returns both major and minor tags when both exist", []*state.VersionRef{rtTag("v1", "abc123"), rtTag("v1.0", "abc123"), rtTag("v2", "def456")}, cfg("branches"), []string{"v1", "v1.0", "v2"}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				st := state.New()
				st.Tags = c.tags
				rtEqual(t, rtVersions(rule(t).Condition(st, c.cfg)), c.want)
			})
		}
	})

	t.Run("Check", func(t *testing.T) {
		cases := []struct {
			name     string
			branches []*state.VersionRef
			want     bool
		}{
			{"returns false when tag exists but branch does not", nil, false},
			{"returns true when both tag and branch exist (let duplicate rule handle)", []*state.VersionRef{rtBranch("v1", "abc123")}, true},
			{"returns false when tag exists, branch exists but is ignored", []*state.VersionRef{rtIgnored(rtBranch("v1", "abc123"))}, false},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				st := state.New()
				tag := rtTag("v1", "abc123")
				st.Tags = []*state.VersionRef{tag}
				st.Branches = c.branches
				if got := rule(t).Check(tag, st, &state.Config{}); got != c.want {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			})
		}
	})

	t.Run("CreateIssue", func(t *testing.T) {
		for _, c := range []struct{ name, version string }{
			{"creates issue with ConvertTagToBranchAction for major tag", "v1"},
			{"creates issue with ConvertTagToBranchAction for minor tag", "v1.0"},
		} {
			t.Run(c.name, func(t *testing.T) {
				st := state.New()
				issue := rule(t).CreateIssue(rtTag(c.version, "abc123"), st, cfg("branches"))
				if issue == nil || issue.Type != "wrong_ref_type" || issue.Severity != "error" || issue.Version != c.version {
					t.Fatalf("unexpected issue %+v", issue)
				}
				a, ok := issue.Action.(*actions.ConvertTagToBranchAction)
				if !ok || a.Version() != c.version {
					t.Fatalf("unexpected action %T", issue.Action)
				}
			})
		}

		t.Run("uses highest patch SHA for major tag conversion when tag SHA is stale and includes warning", func(t *testing.T) {
			st := state.New()
			stale := rtTag("v6", "767b16506b540fdce093ac79ebb8441f5f0a0e08")
			st.Tags = []*state.VersionRef{stale, rtTag("v6.0.0", "452e2d19b81542ae14b68e36469883b9499fca79")}
			issue := rule(t).CreateIssue(stale, st, cfg("branches"))
			a, ok := issue.Action.(*actions.ConvertTagToBranchAction)
			if !ok || a.Sha != "452e2d19b81542ae14b68e36469883b9499fca79" {
				t.Fatalf("unexpected action %+v", issue.Action)
			}
			want := "Tag 'v6' should be a branch (floating-versions-use: branches). WARNING: Conversion will change SHA from 767b16506b540fdce093ac79ebb8441f5f0a0e08 to 452e2d19b81542ae14b68e36469883b9499fca79."
			if issue.Message != want {
				t.Fatalf("message %q", issue.Message)
			}
		})
	})

	t.Run("Integration with Invoke-ValidationRule", func(t *testing.T) {
		t.Run("creates issues for floating version tags only", func(t *testing.T) {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("v1", "abc123"), rtTag("v1.0", "abc123"), rtTag("v1.0.0", "abc123")}
			issues := Run(st, cfg("branches"), []*Rule{rule(t)})
			rtEqual(t, rtIssueVersions(issues), []string{"v1", "v1.0"})
		})
		t.Run("creates no issues when floating-versions-use is 'tags'", func(t *testing.T) {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("v1", "abc123"), rtTag("v1.0", "abc123")}
			if issues := Run(st, cfg("tags"), []*Rule{rule(t)}); len(issues) != 0 {
				t.Fatalf("got %d issues", len(issues))
			}
		})
	})
}
