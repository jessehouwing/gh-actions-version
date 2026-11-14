package rules

import (
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func rlRule(t *testing.T, name string) *Rule {
	t.Helper()
	for _, r := range All() {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("rule %s not registered", name)
	return nil
}

func rlIDs(items []any) []int64 {
	var ids []int64
	for _, it := range items {
		ids = append(ids, it.(*state.ReleaseInfo).ID)
	}
	return ids
}

func TestDuplicateReleaseRule(t *testing.T) {
	rule := rlRule(t, "duplicate_release")
	rel := rlRel

	t.Run("Condition - Configuration checks", func(t *testing.T) {
		cases := []struct {
			name   string
			check  string
			imm    bool
			want   int
			wantID int64
		}{
			{"should return results when check-releases is 'error'", "error", true, 1, 456},
			{"should return results when check-releases is 'warning'", "warning", false, 1, 456},
			{"should return empty when check-releases is 'none'", "none", false, 0, 0},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				st := rlState(rel("v1.0.0", 123, false, false, c.imm), rel("v1.0.0", 456, true, false, false))
				got := rule.Condition(st, &state.Config{CheckReleases: c.check})
				if len(got) != c.want {
					t.Fatalf("got %d items, want %d", len(got), c.want)
				}
				if c.want > 0 && got[0].(*state.ReleaseInfo).ID != c.wantID {
					t.Errorf("got ID %d", got[0].(*state.ReleaseInfo).ID)
				}
			})
		}
	})

	t.Run("Condition - Finding duplicate releases", func(t *testing.T) {
		ign1, ign2 := rel("v1.0.0", 100, false, false, true), rel("v1.0.0", 200, true, false, false)
		ign1.IsIgnored, ign2.IsIgnored = true, true
		cases := []struct {
			name string
			rels []*state.ReleaseInfo
			want []int64
		}{
			{"should return draft duplicate when published release exists", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, true), rel("v1.0.0", 200, true, false, false)}, []int64{200}},
			{"should delete newer draft when two drafts exist for same tag", []*state.ReleaseInfo{rel("v1.0.0", 100, true, false, false), rel("v1.0.0", 200, true, false, false)}, []int64{200}},
			{"should delete multiple draft duplicates", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, true), rel("v1.0.0", 200, true, false, false), rel("v1.0.0", 300, true, false, false)}, []int64{200, 300}},
			{"should not return anything when no duplicates exist", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, true), rel("v2.0.0", 200, false, false, true)}, nil},
			{"should not return published duplicate (cannot be deleted)", []*state.ReleaseInfo{rel("v1.0.0", 100, false, false, true), rel("v1.0.0", 200, false, false, true)}, nil},
			{"should skip ignored versions", []*state.ReleaseInfo{ign1, ign2}, nil},
			{"should only match patch versions (not floating versions)", []*state.ReleaseInfo{
				rel("v1", 100, false, false, false), rel("v1", 200, true, false, false), rel("v1.0", 300, false, false, false), rel("v1.0", 400, true, false, false)}, nil},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				got := rlIDs(rule.Condition(rlState(c.rels...), &state.Config{CheckReleases: "error"}))
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

	t.Run("Check - should return false for duplicate releases (they are invalid)", func(t *testing.T) {
		if rule.Check(rel("v1.0.0", 200, true, false, false), state.New(), &state.Config{CheckReleases: "error"}) {
			t.Error("expected false")
		}
	})

	t.Run("CreateIssue", func(t *testing.T) {
		t.Run("should create issue with error severity when check-releases is 'error'", func(t *testing.T) {
			issue := rule.CreateIssue(rel("v1.0.0", 200, true, false, false), state.New(), &state.Config{CheckReleases: "error"})
			if issue.Severity != "error" || issue.Type != "duplicate_release" || issue.Version != "v1.0.0" ||
				!strings.Contains(issue.Message, "Duplicate draft release") || !strings.Contains(issue.Message, "200") {
				t.Errorf("unexpected issue %+v", issue)
			}
		})
		t.Run("should create issue with warning severity when check-releases is 'warning'", func(t *testing.T) {
			issue := rule.CreateIssue(rel("v1.0.0", 200, true, false, false), state.New(), &state.Config{CheckReleases: "warning"})
			if issue.Severity != "warning" {
				t.Errorf("got %s", issue.Severity)
			}
		})
		t.Run("should create DeleteReleaseAction with correct release ID", func(t *testing.T) {
			issue := rule.CreateIssue(rel("v1.0.0", 12345, true, false, false), state.New(), &state.Config{CheckReleases: "error"})
			a, ok := issue.Action.(*actions.DeleteReleaseAction)
			if !ok || a.ReleaseID != 12345 || a.TagName != "v1.0.0" {
				t.Errorf("unexpected action %#v", issue.Action)
			}
		})
	})

	t.Run("Priority ordering with other release rules", func(t *testing.T) {
		t.Run("should have priority lower than release_should_be_published", func(t *testing.T) {
			if rule.Priority >= rlRule(t, "release_should_be_published").Priority {
				t.Error("priority not lower")
			}
		})
		t.Run("should have priority lower than patch_release_required", func(t *testing.T) {
			if rule.Priority >= rlRule(t, "patch_release_required").Priority {
				t.Error("priority not lower")
			}
		})
	})
}
