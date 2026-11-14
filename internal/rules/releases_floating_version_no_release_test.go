package rules

import (
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func TestFloatingVersionNoReleaseRule(t *testing.T) {
	rule := rlRule(t, "floating_version_no_release")
	rel := rlRel
	cfg := func(r, i string) *state.Config { return &state.Config{CheckReleases: r, CheckImmutability: i} }

	t.Run("Condition - AppliesWhen checks enabled", func(t *testing.T) {
		cases := []struct {
			name string
			r, i string
			want int
		}{
			{"should return results when check-releases is enabled", "error", "none", 1},
			{"should return results when check-release-immutability is enabled", "none", "error", 1},
			{"should return empty when both checks are disabled", "none", "none", 0},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				if got := rule.Condition(rlState(rel("v1", 123, false, false, false)), cfg(c.r, c.i)); len(got) != c.want {
					t.Errorf("got %d, want %d", len(got), c.want)
				}
			})
		}
	})

	t.Run("Condition - Finding floating version releases", func(t *testing.T) {
		ign := rel("v1", 123, false, false, false)
		ign.IsIgnored = true
		cases := []struct {
			name string
			rel  *state.ReleaseInfo
			want int
		}{
			{"should return release for major version (v1)", rel("v1", 123, false, false, false), 1},
			{"should return release for minor version (v1.0)", rel("v1.0", 123, false, false, false), 1},
			{"should return release for 'latest'", rel("latest", 123, false, false, false), 1},
			{"should not return release for patch version (v1.0.0)", rel("v1.0.0", 123, false, false, false), 0},
			{"should skip ignored versions", ign, 0},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got := rule.Condition(rlState(c.rel), cfg("error", "none"))
				if len(got) != c.want {
					t.Fatalf("got %d, want %d", len(got), c.want)
				}
				if c.want == 1 && got[0].(*state.ReleaseInfo).TagName != c.rel.TagName {
					t.Errorf("wrong tag")
				}
			})
		}
	})

	t.Run("CreateIssue - Mutable releases", func(t *testing.T) {
		t.Run("should create error for mutable (draft) floating release when check-releases is error", func(t *testing.T) {
			issue := rule.CreateIssue(rel("v1", 123, true, false, false), state.New(), cfg("error", "none"))
			if _, ok := issue.Action.(*actions.DeleteReleaseAction); issue.Type != "mutable_floating_release" || issue.Severity != "error" ||
				!strings.Contains(issue.Message, "v1") || !ok || issue.Status == state.StatusUnfixable {
				t.Errorf("unexpected issue %+v", issue)
			}
		})
		t.Run("should create warning for mutable floating release when both checks are warning", func(t *testing.T) {
			issue := rule.CreateIssue(rel("v1", 123, true, false, false), state.New(), cfg("warning", "warning"))
			if issue.Type != "mutable_floating_release" || issue.Severity != "warning" || !strings.Contains(issue.Message, "v1") {
				t.Errorf("unexpected issue %+v", issue)
			}
		})
		t.Run("should use most-severe-wins logic (error + warning = error)", func(t *testing.T) {
			if s := rule.CreateIssue(rel("v1", 123, true, false, false), state.New(), cfg("error", "warning")).Severity; s != "error" {
				t.Errorf("got %s", s)
			}
		})
		t.Run("should configure DeleteReleaseAction with release ID", func(t *testing.T) {
			a, ok := rule.CreateIssue(rel("v2", 456, true, false, false), state.New(), cfg("error", "none")).Action.(*actions.DeleteReleaseAction)
			if !ok || a.ReleaseID != 456 || a.TagName != "v2" {
				t.Errorf("unexpected action %#v", a)
			}
		})
	})

	t.Run("CreateIssue - Immutable releases", func(t *testing.T) {
		t.Run("should create unfixable error for immutable floating release", func(t *testing.T) {
			issue := rule.CreateIssue(rel("v1", 123, false, false, true), state.New(), cfg("error", "error"))
			if issue.Type != "immutable_floating_release" || issue.Severity != "error" || !strings.Contains(issue.Message, "v1") ||
				issue.Status != state.StatusUnfixable || issue.Action != nil {
				t.Errorf("unexpected issue %+v", issue)
			}
		})
		t.Run("should create unfixable error for immutable latest release", func(t *testing.T) {
			issue := rule.CreateIssue(rel("latest", 789, false, false, true), state.New(), cfg("error", "error"))
			if issue.Type != "immutable_floating_release" || issue.Severity != "error" || issue.Status != state.StatusUnfixable {
				t.Errorf("unexpected issue %+v", issue)
			}
		})
	})
}
