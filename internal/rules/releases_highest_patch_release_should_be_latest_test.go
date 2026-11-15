package rules

import (
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func TestHighestPatchReleaseShouldBeLatestRule(t *testing.T) {
	rule := rlRule(t, "highest_patch_release_should_be_latest")
	rel := rlRel
	cfg := func(v string) *state.Config { return &state.Config{CheckReleases: v} }
	latest := func(r *state.ReleaseInfo) *state.ReleaseInfo { r.IsLatest = true; return r }
	expectedTag := func(t *testing.T, st *state.State) string {
		t.Helper()
		items := rule.Condition(st, cfg("error"))
		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(items))
		}
		return items[0].(*latestReleaseItem).ExpectedLatest.TagName
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
				if got := rule.Condition(rlState(rel("v1.0.0", 123, false, false, false)), cfg(c.check)); len(got) != c.want {
					t.Errorf("got %d, want %d", len(got), c.want)
				}
			})
		}
	})

	t.Run("Condition - Finding highest non-prerelease patch", func(t *testing.T) {
		ignored := rel("v2.0.0", 200, false, false, false)
		ignored.IsIgnored = true
		cases := []struct {
			name string
			rels []*state.ReleaseInfo
			want string
		}{
			{"should identify v2.0.0 as expected latest when v1.0.0 and v2.0.0 exist", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, false), rel("v2.0.0", 200, false, false, false)}, "v2.0.0"},
			{"should skip prereleases when determining latest", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, false), rel("v2.0.0", 200, false, true, false)}, "v1.0.0"},
			{"should skip draft releases when determining latest", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, false), rel("v2.0.0", 200, true, false, false)}, "v1.0.0"},
			{"should skip ignored versions when determining latest", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, false), ignored}, "v1.0.0"},
			{"should handle version sorting correctly (v10 > v9 > v2)", []*state.ReleaseInfo{rel("v2.0.0", 2, false, false, false), rel("v9.0.0", 9, false, false, false), rel("v10.0.0", 10, false, false, false)}, "v10.0.0"},
			{"should identify highest minor version correctly (v1.2.0 > v1.1.0)", []*state.ReleaseInfo{rel("v1.1.0", 100, false, false, false), rel("v1.2.0", 200, false, false, false)}, "v1.2.0"},
			{"should identify highest patch version correctly (v1.0.2 > v1.0.1)", []*state.ReleaseInfo{rel("v1.0.1", 100, false, false, false), rel("v1.0.2", 200, false, false, false)}, "v1.0.2"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				if got := expectedTag(t, rlState(c.rels...)); got != c.want {
					t.Errorf("got %s, want %s", got, c.want)
				}
			})
		}
		t.Run("should return empty when no published non-prerelease releases exist", func(t *testing.T) {
			if got := rule.Condition(rlState(rel("v1.0.0-beta", 100, false, true, false)), cfg("error")); len(got) != 0 {
				t.Errorf("got %d", len(got))
			}
		})
	})

	t.Run("Check - validates current latest against expected", func(t *testing.T) {
		t.Run("should return true when expected latest is already marked as latest", func(t *testing.T) {
			r := latest(rel("v1.0.0", 100, false, false, false))
			if !rule.Check(&latestReleaseItem{r, r}, rlState(r), cfg("error")) {
				t.Error("expected true")
			}
		})
		t.Run("should return false when different release is marked as latest", func(t *testing.T) {
			r1, r2 := latest(rel("v1.0.0", 100, false, false, false)), rel("v2.0.0", 200, false, false, false)
			if rule.Check(&latestReleaseItem{r2, r1}, rlState(r1, r2), cfg("error")) {
				t.Error("expected false")
			}
		})
		t.Run("should return false when no release is marked as latest", func(t *testing.T) {
			r := rel("v1.0.0", 100, false, false, false)
			if rule.Check(&latestReleaseItem{r, nil}, rlState(r), cfg("error")) {
				t.Error("expected false")
			}
		})
	})

	t.Run("CreateIssue - generates correct issue", func(t *testing.T) {
		t.Run("should create issue with SetLatestReleaseAction when wrong release is latest", func(t *testing.T) {
			r1, r2 := latest(rel("v1.0.0", 100, false, false, false)), rel("v2.0.0", 200, false, false, false)
			issue := rule.CreateIssue(&latestReleaseItem{r2, r1}, rlState(r1, r2), cfg("error"))
			a, ok := issue.Action.(*actions.SetLatestReleaseAction)
			if issue.Finding != "latest_release_wrong" || issue.Severity != "error" || issue.Version != "v2.0.0" ||
				!strings.Contains(issue.Message, "v2.0.0") || !strings.Contains(issue.Message, "latest") ||
				!ok || a.TagName != "v2.0.0" || a.ReleaseID != 200 {
				t.Errorf("unexpected issue %+v", issue)
			}
		})
		t.Run("should create issue with warning severity when check-releases is 'warning'", func(t *testing.T) {
			r := rel("v1.0.0", 100, false, false, false)
			if s := rule.CreateIssue(&latestReleaseItem{r, nil}, rlState(r), cfg("warning")).Severity; s != "warning" {
				t.Errorf("got %s", s)
			}
		})
		t.Run("should mention current latest release in message when one exists", func(t *testing.T) {
			r1, r2 := latest(rel("v1.0.0", 100, false, false, false)), rel("v2.0.0", 200, false, false, false)
			msg := rule.CreateIssue(&latestReleaseItem{r2, r1}, rlState(r1, r2), cfg("error")).Message
			if !strings.Contains(msg, "v1.0.0") || !strings.Contains(msg, "v2.0.0") {
				t.Errorf("got %q", msg)
			}
		})
		t.Run("should use the 'no release is currently marked as latest' message when none is latest", func(t *testing.T) {
			r := rel("v1.0.0", 100, false, false, false)
			msg := rule.CreateIssue(&latestReleaseItem{r, nil}, rlState(r), cfg("error")).Message
			want := "Release v1.0.0 should be marked as 'latest', but no release is currently marked as latest"
			if msg != want {
				t.Errorf("got %q", msg)
			}
		})
	})

	t.Run("Prerelease status - MUST come from GitHub Release API, not tag suffix", func(t *testing.T) {
		cases := []struct {
			name string
			rels []*state.ReleaseInfo
		}{
			{"should treat v2.0.0-beta as stable if release.prerelease is false (tag suffix ignored)", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, false), rel("v2.0.0-beta", 200, false, false, false)}},
			{"should use IsPrerelease from release API not from tag name when both match", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, false), rel("v2.0.0", 200, false, true, false)}},
			{"should skip releases with -rc suffix in tag name (not valid patch version)", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, false), rel("v2.0.0-rc1", 200, false, false, false)}},
			{"should skip releases with -preview suffix in tag name (not valid patch version)", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, false), rel("v2.0.0-preview", 200, false, false, false)}},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				if got := expectedTag(t, rlState(c.rels...)); got != "v1.0.0" {
					t.Errorf("got %s", got)
				}
			})
		}
	})

	t.Run("Integration - full validation flow", func(t *testing.T) {
		t.Run("should not create issue when correct release is already latest", func(t *testing.T) {
			r := latest(rel("v1.0.0", 100, false, false, false))
			for _, it := range rule.Condition(rlState(r), cfg("error")) {
				if !rule.Check(it, rlState(r), cfg("error")) {
					t.Error("expected valid")
				}
			}
		})
		t.Run("should create issue when prerelease is incorrectly marked as latest", func(t *testing.T) {
			st := rlState(rel("v1.0.0", 100, false, false, false), latest(rel("v2.0.0-beta", 200, false, true, false)))
			items := rule.Condition(st, cfg("error"))
			if len(items) != 1 || items[0].(*latestReleaseItem).ExpectedLatest.TagName != "v1.0.0" {
				t.Fatalf("unexpected items %v", items)
			}
			if rule.Check(items[0], st, cfg("error")) {
				t.Error("expected invalid")
			}
			issue := rule.CreateIssue(items[0], st, cfg("error"))
			if a := issue.Action.(*actions.SetLatestReleaseAction); issue.Version != "v1.0.0" || a.TagName != "v1.0.0" {
				t.Errorf("unexpected issue %+v", issue)
			}
		})
	})

	t.Run("Remediation - SetLatestReleaseAction should not create wrong latest", func(t *testing.T) {
		t.Run("should generate remediation that sets higher version as latest (not older)", func(t *testing.T) {
			r1, r2 := latest(rel("v1.0.0", 100, false, false, false)), rel("v2.0.0", 200, false, false, false)
			a := rule.CreateIssue(&latestReleaseItem{r2, r1}, rlState(r1, r2), cfg("error")).Action.(*actions.SetLatestReleaseAction)
			if a.TagName != "v2.0.0" || a.ReleaseID != 200 {
				t.Errorf("unexpected action %#v", a)
			}
		})
		t.Run("should not generate remediation to make a prerelease the latest", func(t *testing.T) {
			st := rlState(rel("v1.0.0", 100, false, false, false), latest(rel("v2.0.0", 200, false, true, false)))
			items := rule.Condition(st, cfg("error"))
			a := rule.CreateIssue(items[0], st, cfg("error")).Action.(*actions.SetLatestReleaseAction)
			if a.TagName != "v1.0.0" || a.ReleaseID != 100 {
				t.Errorf("unexpected action %#v", a)
			}
		})
	})
}
