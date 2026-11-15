package rules

import (
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func TestReleaseShouldBePublishedRule(t *testing.T) {
	rule := rlRule(t, "release_should_be_published")
	rel := rlRel
	cfg := func(r, i string) *state.Config { return &state.Config{CheckReleases: r, CheckImmutability: i} }
	draft := func(tag string, id int64) *state.ReleaseInfo { return rel(tag, id, true, false, false) }

	t.Run("Condition - AppliesWhen check-release-immutability", func(t *testing.T) {
		cases := []struct {
			name string
			r, i string
			want int
		}{
			{"should return results when check-release-immutability is 'error'", "", "error", 1},
			{"should return results when check-release-immutability is 'warning'", "", "warning", 1},
			{"should return empty when both check-release-immutability and check-releases are 'none'", "none", "none", 0},
			{"should return results when check-releases is 'error' even if check-release-immutability is 'none'", "error", "none", 1},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				if got := rule.Condition(rlState(draft("v1.0.0", 123)), cfg(c.r, c.i)); len(got) != c.want {
					t.Errorf("got %d, want %d", len(got), c.want)
				}
			})
		}
	})

	t.Run("Condition - Finding draft releases", func(t *testing.T) {
		ign := draft("v1.0.0", 123)
		ign.IsIgnored = true
		cases := []struct {
			name string
			rels []*state.ReleaseInfo
			want []int64
		}{
			{"should return draft release for patch version", []*state.ReleaseInfo{draft("v1.0.0", 123)}, []int64{123}},
			{"should not return published release", []*state.ReleaseInfo{rel("v1.0.0", 123, false, false, false)}, nil},
			{"should skip ignored versions", []*state.ReleaseInfo{ign}, nil},
			{"should not return draft release for floating version (major)", []*state.ReleaseInfo{draft("v1", 123)}, nil},
			{"should not return draft release for floating version (minor)", []*state.ReleaseInfo{draft("v1.0", 123)}, nil},
			{"should return only the primary draft release when duplicates exist for same tag", []*state.ReleaseInfo{draft("v1.0.0", 101), draft("v1.0.0", 102), draft("v1.0.0", 103)}, []int64{101}},
			{"should return one draft per version when duplicates exist across different versions", []*state.ReleaseInfo{draft("v1.0.0", 101), draft("v1.0.0", 102), draft("v2.0.0", 201)}, []int64{101, 201}},
			{"should not return draft when published release already exists for same tag", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, true), draft("v1.0.0", 200)}, nil},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got := rlIDs(rule.Condition(rlState(c.rels...), cfg("none", "error")))
				if len(got) != len(c.want) {
					t.Fatalf("got %v, want %v", got, c.want)
				}
				for i := range got {
					if got[i] != c.want[i] {
						t.Errorf("got %v, want %v", got, c.want)
					}
				}
			})
		}
	})

	t.Run("CreateIssue", func(t *testing.T) {
		t.Run("should create issue with error severity", func(t *testing.T) {
			issue := rule.CreateIssue(draft("v1.0.0", 123), state.New(), cfg("", "error"))
			if _, ok := issue.Action.(*actions.PublishReleaseAction); issue.Finding != "release_is_draft" || issue.Severity != "error" ||
				!strings.Contains(issue.Message, "v1.0.0") || !ok || issue.Message != "Release v1.0.0 is still in draft status and should be published" {
				t.Errorf("unexpected issue %+v", issue)
			}
		})
		t.Run("should create issue with warning severity when both are warning", func(t *testing.T) {
			if s := rule.CreateIssue(draft("v1.0.0", 123), state.New(), cfg("warning", "warning")).Severity; s != "warning" {
				t.Errorf("got %s", s)
			}
		})
		t.Run("should create issue with error severity when one is error and one is warning (most severe wins)", func(t *testing.T) {
			if s := rule.CreateIssue(draft("v1.0.0", 123), state.New(), cfg("error", "warning")).Severity; s != "error" {
				t.Errorf("got %s", s)
			}
			if s := rule.CreateIssue(draft("v1.0.0", 123), state.New(), cfg("warning", "error")).Severity; s != "error" {
				t.Errorf("got %s", s)
			}
		})
		t.Run("should configure PublishReleaseAction with release ID", func(t *testing.T) {
			a := rule.CreateIssue(draft("v1.0.0", 456), state.New(), cfg("", "error")).Action.(*actions.PublishReleaseAction)
			if a.ReleaseID != 456 || a.TagName != "v1.0.0" {
				t.Errorf("unexpected action %#v", a)
			}
		})
	})

	t.Run("CreateIssue - MakeLatest integration with Test-ShouldBeLatestRelease", func(t *testing.T) {
		pub := func(tag string, id int64) *state.ReleaseInfo { return rel(tag, id, false, false, false) }
		d := draft("v1.0.0", 100)
		pre := rel("v2.0.0", 200, true, true, false)
		cases := []struct {
			name      string
			target    *state.ReleaseInfo
			others    []*state.ReleaseInfo
			wantFalse bool
		}{
			{"should set MakeLatest=false when higher version release exists", draft("v1.0.0", 100), []*state.ReleaseInfo{pub("v2.0.0", 200)}, true},
			{"should NOT set MakeLatest (let GitHub decide) when publishing highest version", draft("v2.0.0", 200), []*state.ReleaseInfo{pub("v1.0.0", 100)}, false},
			{"should NOT set MakeLatest when no other releases exist (first publish)", d, nil, false},
			{"should set MakeLatest=false when publishing prerelease", pre, []*state.ReleaseInfo{pub("v1.0.0", 100)}, true},
			{"should set MakeLatest=false when publishing backport release", draft("v1.5.1", 151), []*state.ReleaseInfo{pub("v2.5.0", 250)}, true},
			{"should NOT set MakeLatest when only prereleases exist at higher versions", draft("v1.0.0", 100), []*state.ReleaseInfo{rel("v2.0.0", 200, false, true, false)}, false},
			{"should handle multiple existing releases correctly", draft("v1.2.0", 120), []*state.ReleaseInfo{pub("v1.0.0", 100), pub("v1.5.0", 150), pub("v2.0.0", 200)}, true},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got := makeLatestUnlessHighest(rlState(append(c.others, c.target)...), c.target.TagName, c.target)
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
