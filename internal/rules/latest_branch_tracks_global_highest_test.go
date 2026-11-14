package rules

import (
	"testing"

	"github.com/jessehouwing/gh-version-action/internal/actions"
	"github.com/jessehouwing/gh-version-action/internal/state"
)

// latestKind parametrises the shared latest_* test suite for the tag and branch variants.
type latestKind struct {
	rule      string
	isBranch  bool
	matchUse  string // floating-versions-use value under which the rule applies
	otherUse  string // value under which it does not apply
	issueType string
}

func (k latestKind) latest(sha string) *state.VersionRef {
	if k.isBranch {
		return rtBranch("latest", sha)
	}
	return rtTag("latest", sha)
}

func (k latestKind) place(st *state.State, r *state.VersionRef) {
	if k.isBranch {
		st.Branches = append(st.Branches, r)
	} else {
		st.Tags = append(st.Tags, r)
	}
}

func runLatestRuleTests(t *testing.T, k latestKind) {
	rule := func(t *testing.T) *Rule { return rtRule(t, k.rule) }
	cfg := func(use string) *state.Config { return &state.Config{FloatingVersionsUse: use} }
	preview := func(ignore bool) *state.Config { return &state.Config{IgnorePreviewReleases: ignore} }

	t.Run("Condition", func(t *testing.T) {
		kind := "tag"
		if k.isBranch {
			kind = "branch"
		}
		t.Run("should return latest "+kind+" when it exists", func(t *testing.T) {
			st := state.New()
			k.place(st, k.latest("abc123"))
			rtEqual(t, rtVersions(rule(t).Condition(st, cfg(k.matchUse))), []string{"latest"})
		})
		t.Run("should return empty when latest "+kind+" doesn't exist", func(t *testing.T) {
			if res := rule(t).Condition(state.New(), cfg(k.matchUse)); len(res) != 0 {
				t.Fatalf("got %d", len(res))
			}
		})
		t.Run("should return empty when floating-versions-use is "+k.otherUse, func(t *testing.T) {
			st := state.New()
			k.place(st, k.latest("abc123"))
			if res := rule(t).Condition(st, cfg(k.otherUse)); len(res) != 0 {
				t.Fatalf("got %d", len(res))
			}
		})
		t.Run("should skip ignored latest "+kind, func(t *testing.T) {
			st := state.New()
			k.place(st, rtIgnored(k.latest("abc123")))
			st.Config.IgnoreVersions = []string{"latest"}
			if res := rule(t).Condition(st, cfg(k.matchUse)); len(res) != 0 {
				t.Fatalf("got %d", len(res))
			}
		})
	})

	t.Run("Check - SHA matching", func(t *testing.T) {
		cases := []struct {
			name           string
			tags, branches []*state.VersionRef
			latestSha      string
			want           bool
		}{
			{"should pass when latest points to highest patch", []*state.VersionRef{rtTag("v1.0.0", "abc123"), rtTag("v1.1.0", "def456")}, nil, "def456", true},
			{"should fail when latest points to wrong SHA", []*state.VersionRef{rtTag("v1.0.0", "abc123"), rtTag("v1.1.0", "def456")}, nil, "abc123", false},
			{"should pass when no patches exist", nil, nil, "abc123", true},
			{"should find patches from both tags and branches", []*state.VersionRef{rtTag("v1.0.0", "abc123")}, []*state.VersionRef{rtBranch("v2.0.0", "ghi789")}, "ghi789", true},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				st := state.New()
				st.Tags, st.Branches = c.tags, c.branches
				if got := rule(t).Check(k.latest(c.latestSha), st, preview(true)); got != c.want {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			})
		}
	})

	t.Run("CreateIssue", func(t *testing.T) {
		t.Run("should create issue with correct details", func(t *testing.T) {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("v1.1.0", "def456")}
			issue := rule(t).CreateIssue(k.latest("abc123"), st, preview(true))
			if issue.Type != k.issueType || issue.Severity != "error" || issue.Version != "latest" || issue.CurrentSha != "abc123" || issue.ExpectedSha != "def456" {
				t.Fatalf("unexpected issue %+v", issue)
			}
			if want := "latest points to abc123 but should point to v1.1.0 at def456"; issue.Message != want {
				t.Fatal(issue.Message)
			}
			want := "UpdateTagAction"
			if k.isBranch {
				want = "UpdateBranchAction"
			}
			if rtActionType(issue.Action) != want {
				t.Fatalf("got %T", issue.Action)
			}
		})
		name := "should configure UpdateTagAction with force flag"
		if k.isBranch {
			name = "should configure UpdateBranchAction with correct SHA"
		}
		t.Run(name, func(t *testing.T) {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("v2.0.0", "new789")}
			issue := rule(t).CreateIssue(k.latest("old123"), st, preview(true))
			switch a := issue.Action.(type) {
			case *actions.UpdateBranchAction:
				if !k.isBranch || a.BranchName != "latest" || a.Sha != "new789" || !a.Force {
					t.Fatalf("unexpected %+v", a)
				}
			case *actions.UpdateTagAction:
				if k.isBranch || a.TagName != "latest" || a.Sha != "new789" || !a.Force {
					t.Fatalf("unexpected %+v", a)
				}
			default:
				t.Fatalf("unexpected action %T", a)
			}
		})
	})

	t.Run("Prerelease Filtering", func(t *testing.T) {
		withPrerelease := func(latestSha, stableSha, preSha string) *state.State {
			st := state.New()
			st.Tags = []*state.VersionRef{rtTag("v1.0.0", stableSha), rtTag("v2.0.0", preSha)}
			st.Releases = []*state.ReleaseInfo{{TagName: "v2.0.0", ID: 2, IsPrerelease: true, Sha: preSha, HtmlUrl: "https://github.com/test/test/releases/tag/v2.0.0"}}
			return st
		}
		t.Run("should exclude prerelease patch when calculating global highest (ignore-preview-releases=true)", func(t *testing.T) {
			st := withPrerelease("stable123", "stable123", "prerel456")
			l := k.latest("stable123")
			k.place(st, l)
			if !rule(t).Check(l, st, preview(true)) {
				t.Fatal("expected pass")
			}
		})
		t.Run("should include prerelease patch when ignore-preview-releases is false", func(t *testing.T) {
			st := withPrerelease("stable123", "stable123", "prerel456")
			l := k.latest("stable123")
			k.place(st, l)
			if rule(t).Check(l, st, preview(false)) {
				t.Fatal("expected fail")
			}
		})
		t.Run("should use non-prerelease SHA in CreateIssue when ignore-preview-releases is true", func(t *testing.T) {
			st := withPrerelease("old123", "stable456", "prerel789")
			l := k.latest("old123")
			k.place(st, l)
			if issue := rule(t).CreateIssue(l, st, preview(true)); issue.ExpectedSha != "stable456" {
				t.Fatalf("got %s", issue.ExpectedSha)
			}
		})
	})
}

func TestLatestBranchTracksGlobalHighest(t *testing.T) {
	runLatestRuleTests(t, latestKind{"latest_branch_tracks_global_highest", true, "branches", "tags", "incorrect_latest_branch"})
}
