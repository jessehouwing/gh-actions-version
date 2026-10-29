package rules

import (
	"testing"

	"github.com/jessehouwing/gh-version-action/internal/actions"
	"github.com/jessehouwing/gh-version-action/internal/state"
)

func TestPatchReleaseRequiredRule(t *testing.T) {
	rule := rlRule(t, "patch_release_required")
	rel := rlRel
	tag := func(v, sha string) *state.VersionRef {
		return state.NewVersionRef(v, "refs/tags/"+v, sha, state.TypeTag)
	}
	stateWith := func(tags []*state.VersionRef, rels ...*state.ReleaseInfo) *state.State {
		st := rlState(rels...)
		st.Tags = tags
		return st
	}
	cfg := func(r, i string) *state.Config { return &state.Config{CheckReleases: r, CheckImmutability: i} }
	versions := func(items []any) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.(*state.VersionRef).Version)
		}
		return out
	}

	t.Run("Condition - AppliesWhen check-releases", func(t *testing.T) {
		cases := []struct {
			name, check string
			want        int
		}{
			{"should return results when check-releases is 'error'", "error", 1},
			{"should return results when check-releases is 'warning'", "warning", 1},
			{"should return empty when check-releases is 'none'", "none", 0},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				if got := rule.Condition(stateWith([]*state.VersionRef{tag("v1.0.0", "abc123")}), cfg(c.check, "none")); len(got) != c.want {
					t.Errorf("got %d, want %d", len(got), c.want)
				}
			})
		}
	})

	t.Run("Condition - Finding missing releases", func(t *testing.T) {
		ignored := tag("v1.0.0", "abc123")
		ignored.IsIgnored = true
		cases := []struct {
			name string
			st   *state.State
			want []string
			sha  string
		}{
			{"should return patch tag without release", stateWith([]*state.VersionRef{tag("v1.0.0", "abc123")}), []string{"v1.0.0"}, ""},
			{"should not return patch tag with release", stateWith([]*state.VersionRef{tag("v1.0.0", "abc123")}, rel("v1.0.0", 123, false, false, false)), nil, ""},
			{"should not return patch tag with draft release", stateWith([]*state.VersionRef{tag("v1.0.0", "abc123")}, rel("v1.0.0", 123, true, false, false)), nil, ""},
			{"should skip ignored versions", stateWith([]*state.VersionRef{ignored}), nil, ""},
			{"should return expected patch when v1 exists but v1.0.0 doesn't", stateWith([]*state.VersionRef{tag("v1", "abc123")}), []string{"v1.0.0"}, "abc123"},
			{"should return expected patch when v1.0 exists but v1.0.0 doesn't", stateWith([]*state.VersionRef{tag("v1.0", "def456")}), []string{"v1.0.0"}, "def456"},
			{"should not require v6.1.0 when v6.1 exists and v6.1.1 has a release", stateWith(
				[]*state.VersionRef{tag("v6.1", "sha611"), tag("v6.1.1", "sha611")}, rel("v6.1.1", 611, false, false, true)), nil, ""},
			{"should skip when draft release exists for expected patch", stateWith([]*state.VersionRef{tag("v1", "abc123")}, rel("v1.0.0", 123, true, false, false)), nil, ""},
			{"should not return duplicate results when both vX and vX.Y exist for same patch", stateWith([]*state.VersionRef{tag("v1", "abc123"), tag("v1.0", "abc123")}), []string{"v1.0.0"}, ""},
			{"should not return duplicate results when vX.Y.Z tag and vX floating both exist", stateWith([]*state.VersionRef{tag("v1.0.0", "abc123"), tag("v1", "abc123")}), []string{"v1.0.0"}, ""},
			{"should not return duplicate results when all v0, v0.0, v0.0.0 exist without releases", stateWith(
				[]*state.VersionRef{tag("v0", "sha0"), tag("v0.0", "sha0"), tag("v0.0.0", "sha0")}), []string{"v0.0.0"}, ""},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got := rule.Condition(c.st, cfg("error", "none"))
				v := versions(got)
				if len(v) != len(c.want) || (len(v) == 1 && v[0] != c.want[0]) {
					t.Fatalf("got %v, want %v", v, c.want)
				}
				if c.sha != "" && got[0].(*state.VersionRef).Sha != c.sha {
					t.Errorf("sha = %s, want %s", got[0].(*state.VersionRef).Sha, c.sha)
				}
			})
		}
	})

	t.Run("CreateIssue", func(t *testing.T) {
		v := tag("v1.0.0", "abc123")
		t.Run("should create issue with error severity", func(t *testing.T) {
			issue := rule.CreateIssue(v, state.New(), cfg("error", "none"))
			if _, ok := issue.Action.(*actions.CreateReleaseAction); issue.Type != "missing_release" || issue.Severity != "error" ||
				issue.Message != "Release required for patch version v1.0.0" || !ok {
				t.Errorf("unexpected issue %+v", issue)
			}
		})
		t.Run("should create issue with warning severity", func(t *testing.T) {
			if s := rule.CreateIssue(v, state.New(), cfg("warning", "none")).Severity; s != "warning" {
				t.Errorf("got %s", s)
			}
		})
		t.Run("should configure CreateReleaseAction to auto-publish when immutability check is error", func(t *testing.T) {
			a := rule.CreateIssue(v, state.New(), cfg("error", "error")).Action.(*actions.CreateReleaseAction)
			if !a.AutoPublish || a.IsDraft || a.TargetSha != "abc123" {
				t.Errorf("unexpected action %#v", a)
			}
		})
		t.Run("should configure CreateReleaseAction to NOT auto-publish when immutability check is none", func(t *testing.T) {
			a := rule.CreateIssue(v, state.New(), cfg("error", "none")).Action.(*actions.CreateReleaseAction)
			if a.AutoPublish || !a.IsDraft {
				t.Errorf("unexpected action %#v", a)
			}
		})
	})

	t.Run("CreateIssue - MakeLatest integration with Test-ShouldBeLatestRelease", func(t *testing.T) {
		latestRel := func(tagName string, id int64) *state.ReleaseInfo { return rlRel(tagName, id, false, false, false) }
		cases := []struct {
			name      string
			version   string
			rels      []*state.ReleaseInfo
			wantFalse bool
		}{
			{"should set MakeLatest=false when higher version release exists", "v1.0.0", []*state.ReleaseInfo{latestRel("v2.0.0", 200)}, true},
			{"should NOT set MakeLatest (let GitHub decide) when creating highest version", "v2.0.0", []*state.ReleaseInfo{latestRel("v1.0.0", 100)}, false},
			{"should NOT set MakeLatest when no releases exist (first release)", "v1.0.0", nil, false},
			{"should set MakeLatest=false when creating backport release", "v1.5.1", []*state.ReleaseInfo{latestRel("v2.5.0", 250)}, true},
			{"should NOT set MakeLatest when only prerelease exists at higher version", "v1.0.0", []*state.ReleaseInfo{rlRel("v2.0.0", 200, false, true, false)}, false},
			{"should handle multiple existing releases correctly", "v1.2.0", []*state.ReleaseInfo{latestRel("v1.0.0", 100), latestRel("v1.5.0", 150), latestRel("v2.0.0", 200)}, true},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got := makeLatestUnlessHighest(rlState(c.rels...), c.version, nil)
				if c.wantFalse && (got == nil || *got) {
					t.Errorf("expected MakeLatest=false, got %v", got)
				}
				if !c.wantFalse && got != nil {
					t.Errorf("expected MakeLatest unset, got %v", *got)
				}
			})
		}
	})
}
