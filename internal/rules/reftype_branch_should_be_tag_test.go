package rules

import (
	"reflect"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// Helpers shared by the ref_type and latest rule tests.

func rtTag(v, sha string) *state.VersionRef {
	return state.NewVersionRef(v, "refs/tags/"+v, sha, state.TypeTag)
}

func rtBranch(v, sha string) *state.VersionRef {
	return state.NewVersionRef(v, "refs/heads/"+v, sha, state.TypeBranch)
}

func rtIgnored(r *state.VersionRef) *state.VersionRef {
	r.IsIgnored = true
	return r
}

func rtRule(t *testing.T, name string) *Rule {
	t.Helper()
	for _, r := range All() {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("rule %s not registered", name)
	return nil
}

func rtVersions(items []any) []string {
	var out []string
	for _, i := range items {
		switch v := i.(type) {
		case *state.VersionRef:
			out = append(out, v.Version)
		case *DuplicateRef:
			out = append(out, v.Version)
		}
	}
	return out
}

func rtActionType(a state.Action) string {
	if a == nil {
		return ""
	}
	return reflect.TypeOf(a).Elem().Name()
}

func rtIssueVersions(issues []*state.Issue) []string {
	var out []string
	for _, i := range issues {
		out = append(out, i.Version)
	}
	return out
}

func rtEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestBranchShouldBeTag(t *testing.T) {
	rule := func(t *testing.T) *Rule { return rtRule(t, "branch_should_be_tag") }
	cfg := func(use string) *state.Config { return &state.Config{FloatingVersionsUse: use} }

	t.Run("Condition", func(t *testing.T) {
		cases := []struct {
			name     string
			branches []*state.VersionRef
			cfg      *state.Config
			want     []string
		}{
			{"returns patch branches when floating-versions-use is 'tags'", []*state.VersionRef{rtBranch("v1.0.0", "abc123")}, cfg("tags"), []string{"v1.0.0"}},
			{"returns major branches when floating-versions-use is 'tags'", []*state.VersionRef{rtBranch("v1", "abc123")}, cfg("tags"), []string{"v1"}},
			{"returns minor branches when floating-versions-use is 'tags'", []*state.VersionRef{rtBranch("v1.0", "abc123")}, cfg("tags"), []string{"v1.0"}},
			{"returns empty array when floating-versions-use is 'branches'", []*state.VersionRef{rtBranch("v1", "abc123"), rtBranch("v1.0", "abc123"), rtBranch("v1.0.0", "abc123")}, cfg("branches"), nil},
			{"excludes ignored versions", []*state.VersionRef{rtIgnored(rtBranch("v1.0.0", "abc123")), rtBranch("v2.0.0", "def456")}, cfg("tags"), []string{"v2.0.0"}},
			{"uses 'tags' as default when config is not set", []*state.VersionRef{rtBranch("v1", "abc123")}, &state.Config{}, []string{"v1"}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				st := state.New()
				st.Branches = c.branches
				rtEqual(t, rtVersions(rule(t).Condition(st, c.cfg)), c.want)
			})
		}
	})

	t.Run("Check", func(t *testing.T) {
		cases := []struct {
			name string
			tags []*state.VersionRef
			want bool
		}{
			{"returns false when branch exists but tag does not", nil, false},
			{"returns true when both branch and tag exist (let duplicate rule handle)", []*state.VersionRef{rtTag("v1.0.0", "abc123")}, true},
			{"returns false when branch exists, tag exists but is ignored", []*state.VersionRef{rtIgnored(rtTag("v1.0.0", "abc123"))}, false},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				st := state.New()
				b := rtBranch("v1.0.0", "abc123")
				st.Branches = []*state.VersionRef{b}
				st.Tags = c.tags
				if got := rule(t).Check(b, st, &state.Config{}); got != c.want {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			})
		}
	})

	t.Run("CreateIssue", func(t *testing.T) {
		for _, c := range []struct{ name, version string }{
			{"creates issue with ConvertBranchToTagAction for patch branch", "v1.0.0"},
			{"creates issue with ConvertBranchToTagAction for major branch", "v1"},
			{"creates issue with ConvertBranchToTagAction for minor branch", "v1.0"},
		} {
			t.Run(c.name, func(t *testing.T) {
				st := state.New()
				b := rtBranch(c.version, "abc123")
				issue := rule(t).CreateIssue(b, st, cfg("tags"))
				if issue == nil || issue.Finding != "floating_ref_should_be_tag" || issue.Severity != "error" || issue.Version != c.version {
					t.Fatalf("unexpected issue %+v", issue)
				}
				a, ok := issue.Action.(*actions.ConvertBranchToTagAction)
				if !ok || a.Version() != c.version {
					t.Fatalf("unexpected action %T", issue.Action)
				}
			})
		}

		t.Run("uses highest patch SHA for major branch conversion when branch SHA is stale", func(t *testing.T) {
			st := state.New()
			stale := rtBranch("v6", "767b16506b540fdce093ac79ebb8441f5f0a0e08")
			st.Branches = []*state.VersionRef{stale}
			st.Tags = []*state.VersionRef{rtTag("v6.0.0", "452e2d19b81542ae14b68e36469883b9499fca79")}
			issue := rule(t).CreateIssue(stale, st, cfg("tags"))
			a, ok := issue.Action.(*actions.ConvertBranchToTagAction)
			if !ok || a.Sha != "452e2d19b81542ae14b68e36469883b9499fca79" {
				t.Fatalf("unexpected action %+v", issue.Action)
			}
			want := "Branch 'v6' should be a tag (floating-versions-use: tags). WARNING: Conversion will change SHA from 767b16506b540fdce093ac79ebb8441f5f0a0e08 to 452e2d19b81542ae14b68e36469883b9499fca79."
			if issue.Message != want {
				t.Fatalf("message %q", issue.Message)
			}
		})
	})

	t.Run("Integration with Invoke-ValidationRule", func(t *testing.T) {
		t.Run("creates issues for all problematic branches", func(t *testing.T) {
			st := state.New()
			st.Branches = []*state.VersionRef{rtBranch("v1", "abc123"), rtBranch("v1.0", "abc123"), rtBranch("v1.0.0", "abc123")}
			issues := Run(st, cfg("tags"), []*Rule{rule(t)})
			rtEqual(t, rtIssueVersions(issues), []string{"v1", "v1.0", "v1.0.0"})
		})
		t.Run("creates no issues when floating-versions-use is 'branches'", func(t *testing.T) {
			st := state.New()
			st.Branches = []*state.VersionRef{rtBranch("v1", "abc123"), rtBranch("v1.0", "abc123")}
			if issues := Run(st, cfg("branches"), []*Rule{rule(t)}); len(issues) != 0 {
				t.Fatalf("got %d issues", len(issues))
			}
		})
	})
}
