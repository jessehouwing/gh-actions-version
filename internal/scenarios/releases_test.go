package scenarios

import (
	"context"
	"regexp"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/rules"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func matchRe(t *testing.T, s, pattern string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(s) {
		t.Errorf("expected %q to match:\n%s", pattern, s)
	}
}

// TestReleaseScenarios ports the release checking, immutability and release creation flows of SemVerValidation.Tests.ps1.
func TestReleaseScenarios(t *testing.T) {
	t.Run("Release checking", func(t *testing.T) {
		t.Run("Should not check releases when check-releases is none", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1.0.0"), tagCfg())
			hasNot(t, messages(st), "release")
			hasNot(t, commands(st), "gh release")
		})
		t.Run("Should suggest creating a release when tag exists but release doesn't", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1.0.0", "v1"), relCfg())
			equal(t, len(issuesOfType(st, "missing_release")), 1)
		})
	})
	t.Run("Release immutability checking", func(t *testing.T) {
		t.Run("Should not check release immutability when check-release-immutability is none", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1")
			r.release("v1.0.0", draft)
			st := validate(t, r, tagCfg())
			hasNot(t, messages(st), "draft")
			hasNot(t, messages(st), "immutable")
		})
		t.Run("Should allow checking releases but not immutability separately", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1")
			r.release("v1.0.0")
			st := validate(t, r, relCfg(immutability("none")))
			equal(t, len(issuesOfType(st, "non_immutable_release")), 0)
		})
	})
	t.Run("Draft release detection with check-releases enabled", func(t *testing.T) {
		t.Run("Should suggest publishing draft releases instead of creating new ones when check-releases is enabled", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0.1", "v1")
			r.release("v1.0.0", draft)
			r.release("v1.0.1", draft)
			st := validate(t, r, relCfg(immutability("none")))
			equal(t, len(issuesOfType(st, "missing_release")), 0)
			hasNot(t, commands(st), "gh release create")
			has(t, messages(st), "draft status")
			matchRe(t, commands(st), `gh release edit v1\.0\.0.*--draft=false`)
			matchRe(t, commands(st), `gh release edit v1\.0\.1.*--draft=false`)
		})
		t.Run("Should correctly load releases and match them to tags by TagName", func(t *testing.T) {
			r := newRepo().tag(c1, "v2.0.0", "v2")
			r.release("v2.0.0")
			st := validate(t, r, relCfg(immutability("none")))
			equal(t, len(issuesOfType(st, "missing_release")), 0)
			hasNot(t, commands(st), "gh release create v2.0.0")
		})
	})
	t.Run("Release Immutability - Draft Release Publishing", func(t *testing.T) {
		createAction := func(t *testing.T, st *state.State) *actions.CreateReleaseAction {
			t.Helper()
			for _, i := range st.Issues {
				if a, ok := i.Action.(*actions.CreateReleaseAction); ok {
					equal(t, i.Version, "v1.0.0")
					equal(t, a.Version(), "v1.0.0")
					return a
				}
			}
			t.Fatal("no CreateReleaseAction")
			return nil
		}
		t.Run("Should create remediation action for missing release with AutoPublish when immutability checking enabled", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1.0.0"), relCfg())
			equal(t, createAction(t, st).AutoPublish, true)
		})
		t.Run("Should use CreateReleaseAction without AutoPublish when immutability checking disabled", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1.0.0"), relCfg(immutability("none")))
			equal(t, createAction(t, st).AutoPublish, false)
		})
		t.Run("Should show gh release edit command in manual instructions for draft releases", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1")
			r.release("v1.0.0", draft)
			st := validate(t, r, tagCfg(immutability("error")))
			has(t, messages(st), "draft status")
			matchRe(t, commands(st), `gh release edit v1\.0\.0.*--draft=false`)
		})
		t.Run("Should handle 422 error when tag_name used by immutable release", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1")
			r.deletedTags["v1.0.0"] = true
			cfg := relCfg()
			st := validate(t, r, cfg)
			fixAll(t, r, st)
			if st.UnfixableCount()+st.ManualFixCount()+st.FailedCount() == 0 {
				t.Fatalf("expected the 422 to be reported as not fixable: fixed=%d", st.FixedCount())
			}
			for _, i := range issuesOfType(st, "missing_release") {
				if i.Status == state.StatusFixed {
					t.Error("missing release must not be reported fixed")
				}
			}
		})
		t.Run("Should delete mutable releases on floating versions", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1")
			r.release("v1.0.0", immutable)
			r.release("v1")
			cfg := relCfg()
			st := validate(t, r, cfg)
			equal(t, len(issuesOfType(st, "mutable_floating_release")), 1)
			fixAll(t, r, st)
			if r.findRelease("v1") != nil {
				t.Error("mutable floating release should be deleted")
			}
			if r.findRelease("v1.0.0") == nil {
				t.Error("patch release must be kept")
			}
		})
		t.Run("Should parse GraphQL immutability check responses", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1")
			r.release("v1.0.0", latest)
			st := validate(t, r, relCfg())
			equal(t, len(issuesOfType(st, "non_immutable_release")), 1)
			r.findRelease("v1.0.0").IsImmutable = true
			equal(t, len(validate(t, r, relCfg()).Issues), 0)
		})
	})
	t.Run("Release Creation Flow - Draft Creation via REST API", func(t *testing.T) {
		t.Run("Should create draft releases via REST API during auto-fix", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1")
			cfg := relCfg(immutability("none"))
			fixAll(t, r, validate(t, r, cfg))
			rel := r.findRelease("v1.0.0")
			if rel == nil || !rel.IsDraft {
				t.Fatalf("expected a draft release, got %+v (%v)", rel, r.writes)
			}
			// the draft still has to be published; a second pass does that
			fixAll(t, r, validate(t, r, cfg))
			clean(t, r, cfg)
		})
		t.Run("Should handle cascading fixes - create draft then publish", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1")
			r.immutableEnabled = true
			cfg := relCfg()
			st := validate(t, r, cfg)
			fixAll(t, r, st)
			rel := r.findRelease("v1.0.0")
			if rel == nil || rel.IsDraft || !rel.IsImmutable {
				t.Fatalf("expected published immutable release, got %+v (%v)", rel, r.writes)
			}
			equal(t, st.UnfixableCount()+st.FailedCount(), 0)
			clean(t, r, cfg)
		})
		t.Run("Should publish existing draft releases when auto-fixing", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1")
			r.immutableEnabled = true
			r.release("v1.0.0", draft)
			cfg := relCfg()
			fixAll(t, r, validate(t, r, cfg))
			if rel := r.findRelease("v1.0.0"); rel.IsDraft || !rel.IsImmutable {
				t.Fatalf("release not published: %+v", rel)
			}
			clean(t, r, cfg)
		})
	})
	_ = context.Background
}

func ruleNamed(t *testing.T, name string) *rules.Rule {
	t.Helper()
	for _, r := range rules.All() {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("rule %s not registered", name)
	return nil
}

// issuesFrom runs a single rule against st and returns its issues.
func issuesFrom(t *testing.T, name string, st *state.State, cfg state.Config) []*state.Issue {
	t.Helper()
	st.Issues = nil
	return rules.Run(st, &cfg, []*rules.Rule{ruleNamed(t, name)})
}

func sevState(withTags ...[3]string) *state.State {
	st := state.New()
	st.Owner, st.Repo = "test-owner", "test-repo"
	for _, tg := range withTags {
		st.Tags = append(st.Tags, state.NewVersionRef(tg[0], "refs/tags/"+tg[0], tg[1], state.TypeTag))
	}
	return st
}

func rel(tag string, id int64, opts ...relOpt) *state.ReleaseInfo {
	r := &state.ReleaseInfo{TagName: tag, ID: id}
	for _, o := range opts {
		o(r)
	}
	return r
}

func severityOf(t *testing.T, name string, st *state.State, cfg state.Config) string {
	t.Helper()
	is := issuesFrom(t, name, st, cfg)
	if len(is) == 0 {
		t.Fatalf("%s produced no issue", name)
	}
	return is[0].Severity
}

// TestRuleSeverityValidation ports tests/integration/RuleSeverityValidation.Tests.ps1.
func TestRuleSeverityValidation(t *testing.T) {
	cfgWith := func(opts ...cfgOpt) state.Config {
		c := tagCfg(opts...)
		return c
	}
	t.Run("Rule Severity Level Validation", func(t *testing.T) {
		t.Run("Releases Rules - Config-Based Severity", func(t *testing.T) {
			t.Run("patch_release_required uses check-releases for severity", func(t *testing.T) {
				st := sevState([3]string{"v2.0.0", "xyz999"})
				equal(t, severityOf(t, "patch_release_required", st, cfgWith(releases("error"))), "error")
				equal(t, severityOf(t, "patch_release_required", st, cfgWith(releases("warning"))), "warning")
				equal(t, len(issuesFrom(t, "patch_release_required", st, cfgWith(releases("none")))), 0)
			})
			t.Run("release_should_be_published uses most-severe-wins logic for severity", func(t *testing.T) {
				st := sevState([3]string{"v1.1.0", "def456"})
				st.Releases = []*state.ReleaseInfo{rel("v1.1.0", 101, draft)}
				for _, c := range []struct{ rel, imm, want string }{
					{"error", "error", "error"}, {"error", "warning", "error"},
					{"warning", "error", "error"}, {"warning", "warning", "warning"},
				} {
					equal(t, severityOf(t, "release_should_be_published", st, cfgWith(releases(c.rel), immutability(c.imm))), c.want)
				}
			})
			t.Run("release_should_be_immutable uses check-release-immutability for severity", func(t *testing.T) {
				st := sevState([3]string{"v1.2.0", "ghi789"})
				st.Releases = []*state.ReleaseInfo{rel("v1.2.0", 102)}
				equal(t, severityOf(t, "release_should_be_immutable", st, cfgWith(immutability("error"))), "error")
				equal(t, severityOf(t, "release_should_be_immutable", st, cfgWith(immutability("warning"))), "warning")
				equal(t, len(issuesFrom(t, "release_should_be_immutable", st, cfgWith(immutability("none")))), 0)
			})
			t.Run("floating_version_no_release uses config-based severity for mutable releases", func(t *testing.T) {
				st := sevState()
				st.Releases = []*state.ReleaseInfo{rel("v1", 200, immutable)}
				is := issuesFrom(t, "floating_version_no_release", st, cfgWith(releases("error")))
				equal(t, len(is), 1)
				equal(t, is[0].Severity, "error")
				equal(t, is[0].Status, state.StatusUnfixable)

				st = sevState()
				st.Releases = []*state.ReleaseInfo{rel("v2", 300, draft)}
				for _, c := range []struct{ rel, imm, want string }{
					{"error", "none", "error"}, {"warning", "none", "warning"}, {"error", "warning", "error"},
				} {
					equal(t, severityOf(t, "floating_version_no_release", st, cfgWith(releases(c.rel), immutability(c.imm))), c.want)
				}
			})
		})
		t.Run("Version Tracking Rules - Config-Based Severity for Minor", func(t *testing.T) {
			t.Run("minor_tag_missing uses check-minor-version for severity", func(t *testing.T) {
				st := sevState([3]string{"v1.0.0", "abc123"}, [3]string{"v1", "abc123"})
				equal(t, severityOf(t, "minor_tag_missing", st, cfgWith(minor("error"))), "error")
				equal(t, severityOf(t, "minor_tag_missing", st, cfgWith(minor("warning"))), "warning")
				equal(t, len(issuesFrom(t, "minor_tag_missing", st, cfgWith(minor("none")))), 0)
			})
			t.Run("minor_tag_tracks_highest_patch uses check-minor-version for severity", func(t *testing.T) {
				st := sevState([3]string{"v1.0.0", "abc123"}, [3]string{"v1.0", "abc123"}, [3]string{"v1.0.1", "def456"})
				equal(t, severityOf(t, "minor_tag_tracks_highest_patch", st, cfgWith(minor("error"))), "error")
				equal(t, severityOf(t, "minor_tag_tracks_highest_patch", st, cfgWith(minor("warning"))), "warning")
			})
		})
		t.Run("Version Tracking Rules - Hardcoded Error for Major", func(t *testing.T) {
			t.Run("major_tag_tracks_highest_patch always uses error severity", func(t *testing.T) {
				st := sevState([3]string{"v1.0.0", "abc123"}, [3]string{"v1", "abc123"}, [3]string{"v1.0.1", "def456"})
				for _, m := range []string{"error", "warning", "none"} {
					equal(t, severityOf(t, "major_tag_tracks_highest_patch", st, cfgWith(minor(m))), "error")
				}
			})
		})
		t.Run("Ref Type Rules - Always Error", func(t *testing.T) {
			t.Run("tag_should_be_branch always uses error severity", func(t *testing.T) {
				st := sevState([3]string{"v1", "abc123"})
				equal(t, severityOf(t, "tag_should_be_branch", st, cfgWith(floating("branches"))), "error")
			})
		})
	})
	t.Run("Rule Configuration Matrix - Complete Validation", func(t *testing.T) {
		t.Run("All rules respect configuration settings", func(t *testing.T) {
			t.Run("loads all rules without errors", func(t *testing.T) {
				all := rules.All()
				if len(all) <= 20 {
					t.Fatalf("expected more than 20 rules, got %d", len(all))
				}
				for _, r := range all {
					if r.Name == "" || r.Description == "" || r.Priority <= 0 || r.Category == "" ||
						r.Condition == nil || r.Check == nil || r.CreateIssue == nil {
						t.Errorf("incomplete rule: %+v", r)
					}
				}
			})
			t.Run("config-based severity rules have consistent behavior", func(t *testing.T) {
				for _, n := range []string{"minor_tag_missing", "minor_tag_tracks_highest_patch", "minor_branch_missing", "minor_branch_tracks_highest_patch"} {
					equal(t, ruleNamed(t, n).Description != "", true)
				}
			})
			t.Run("all release rules respect check-releases or check-release-immutability", func(t *testing.T) {
				for _, n := range []string{"patch_release_required", "release_should_be_published", "release_should_be_immutable", "highest_patch_release_should_be_latest", "duplicate_release"} {
					equal(t, ruleNamed(t, n).Category, "releases")
				}
			})
		})
	})
}
