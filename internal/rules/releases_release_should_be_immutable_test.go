package rules

import (
	"strings"
	"testing"

	"github.com/jessehouwing/gh-version-action/internal/actions"
	"github.com/jessehouwing/gh-version-action/internal/state"
)

func TestReleaseShouldBeImmutableRule(t *testing.T) {
	rule := rlRule(t, "release_should_be_immutable")
	rel := rlRel
	cfg := func(v string) *state.Config { return &state.Config{CheckImmutability: v} }

	t.Run("Condition - AppliesWhen check-release-immutability", func(t *testing.T) {
		cases := []struct {
			name, check string
			want        int
		}{
			{"should return results when check-release-immutability is 'error'", "error", 1},
			{"should return results when check-release-immutability is 'warning'", "warning", 1},
			{"should return empty when check-release-immutability is 'none'", "none", 0},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				if got := rule.Condition(rlState(rel("v1.0.0", 123, false, false, false)), cfg(c.check)); len(got) != c.want {
					t.Errorf("got %d, want %d", len(got), c.want)
				}
			})
		}
	})

	t.Run("Condition - Finding published releases", func(t *testing.T) {
		ign := rel("v1.0.0", 123, false, false, false)
		ign.IsIgnored = true
		cases := []struct {
			name string
			rel  *state.ReleaseInfo
			want int
		}{
			{"should return published release for patch version", rel("v1.0.0", 123, false, false, false), 1},
			{"should not return draft release", rel("v1.0.0", 123, true, false, false), 0},
			{"should skip ignored versions", ign, 0},
			{"should not return published release for floating version (major)", rel("v1", 123, false, false, false), 0},
			{"should not return published release for floating version (minor)", rel("v1.0", 123, false, false, false), 0},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got := rule.Condition(rlState(c.rel), cfg("error"))
				if len(got) != c.want {
					t.Fatalf("got %d, want %d", len(got), c.want)
				}
				if c.want == 1 && got[0].(*state.ReleaseInfo).TagName != "v1.0.0" {
					t.Error("wrong tag")
				}
			})
		}
		t.Run("should exclude duplicate published releases that duplicate_release skips", func(t *testing.T) {
			got := rlIDs(rule.Condition(rlState(rel("v1.0.0", 100, false, false, true), rel("v1.0.0", 200, false, false, false)), cfg("error")))
			if len(got) != 1 || got[0] != 100 {
				t.Errorf("got %v", got)
			}
		})
	})

	t.Run("Check - Immutability verification", func(t *testing.T) {
		t.Run("should return true when release is immutable", func(t *testing.T) {
			if !rule.Check(rel("v1.0.0", 123, false, false, true), state.New(), cfg("error")) {
				t.Error("expected true")
			}
		})
		t.Run("should return false when release is not immutable", func(t *testing.T) {
			if rule.Check(rel("v1.0.0", 123, false, false, false), state.New(), cfg("error")) {
				t.Error("expected false")
			}
		})
	})

	t.Run("CreateIssue", func(t *testing.T) {
		t.Run("should create issue with error severity when configured as error", func(t *testing.T) {
			issue := rule.CreateIssue(rel("v1.0.0", 123, false, false, false), state.New(), cfg("error"))
			if _, ok := issue.Action.(*actions.RepublishReleaseAction); issue.Type != "non_immutable_release" || issue.Severity != "error" ||
				!strings.Contains(issue.Message, "v1.0.0") || !ok {
				t.Errorf("unexpected issue %+v", issue)
			}
		})
		t.Run("should create issue with warning severity when configured as warning", func(t *testing.T) {
			issue := rule.CreateIssue(rel("v1.0.1", 124, false, false, false), state.New(), cfg("warning"))
			if issue.Type != "non_immutable_release" || issue.Severity != "warning" {
				t.Errorf("unexpected issue %+v", issue)
			}
		})
		t.Run("should configure RepublishReleaseAction with tag name", func(t *testing.T) {
			a := rule.CreateIssue(rel("v2.5.3", 456, false, false, false), state.New(), cfg("warning")).Action.(*actions.RepublishReleaseAction)
			if a.TagName != "v2.5.3" {
				t.Errorf("got %s", a.TagName)
			}
		})
		t.Run("should set MakeLatest=false when higher version release exists", func(t *testing.T) {
			r := rel("v2.5.3", 456, false, false, false)
			if republishMakeLatest(rlState(rel("v3.0.0", 500, false, false, false)), r) {
				t.Error("expected false")
			}
		})
		t.Run("should set MakeLatest=true when release is currently latest", func(t *testing.T) {
			r := rel("v1.0.8", 456, false, false, false)
			r.IsLatest = true
			if !republishMakeLatest(rlState(r), r) {
				t.Error("expected true")
			}
		})
		t.Run("should set MakeLatest=true when release is the highest version", func(t *testing.T) {
			r := rel("v1.0.8", 456, false, false, false)
			if !republishMakeLatest(rlState(rel("v1.0.7", 455, false, false, true)), r) {
				t.Error("expected true")
			}
		})
	})
}
