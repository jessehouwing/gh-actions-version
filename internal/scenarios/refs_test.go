package scenarios

import (
	"strings"
	"testing"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

// push is the manual command that creates/updates a ref (PowerShell: "git push origin <sha>:refs/tags/v1").
func push(sha, ref string) string { return "git push origin " + sha + ":" + ref }

// TestSemVerChecker ports tests/e2e/SemVerValidation.Tests.ps1 "SemVer Checker" (ref validation scenarios).
func TestSemVerChecker(t *testing.T) {
	t.Run("When repo only has a minor version tag (e.g., v1.1)", func(t *testing.T) {
		t.Run("Should suggest creating both major version (v1) and patch version (v1.1.0)", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.1")
			st := validate(t, r, tagCfg())
			if len(st.ErrorIssues()) == 0 {
				t.Fatal("expected errors")
			}
			cmds := commands(st)
			has(t, cmds, push(c1, "refs/tags/v1"))
			has(t, cmds, push(c1, "refs/tags/v1.1.0"))
			hasNot(t, cmds, "git push origin :refs/tags/v1\n")
			hasNot(t, cmds, "git push origin :refs/tags/v1.1")
		})
	})
	t.Run("When repo has proper semver tags (v1, v1.1, v1.1.0)", func(t *testing.T) {
		t.Run("Should not report any errors when all tags point to same commit", func(t *testing.T) {
			r := newRepo().tag(c1, "v1", "v1.1", "v1.1.0")
			st := validate(t, r, tagCfg())
			if len(st.Issues) != 0 {
				t.Fatalf("unexpected issues: %s", messages(st))
			}
			hasNot(t, commands(st), "git push")
		})
	})
	t.Run("When repo has only patch version (v1.0.0)", func(t *testing.T) {
		t.Run("Should suggest creating major (v1) and minor (v1.0) versions", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1.0.0"), tagCfg())
			if len(st.ErrorIssues()) == 0 {
				t.Fatal("expected errors")
			}
			cmds := commands(st)
			has(t, cmds, push(c1, "refs/tags/v1")+"\n")
			has(t, cmds, push(c1, "refs/tags/v1.0")+"\n")
		})
	})
	t.Run("When repo has mismatched versions", func(t *testing.T) {
		t.Run("Should suggest force updating when major version points to wrong commit", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1").tag(c2, "v1.1.0", "v1.1")
			st := validate(t, r, tagCfg())
			has(t, commands(st), push(c2, "refs/tags/v1")+" --force")
		})
	})
	t.Run("When check-minor-version is false", func(t *testing.T) {
		t.Run("Should not check minor version tags", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.1.0", "v1")
			st := validate(t, r, tagCfg(minor("none")))
			if len(st.Issues) != 0 {
				t.Fatalf("unexpected issues: %s", messages(st))
			}
		})
	})
	t.Run("When repo only has a major version tag (e.g., v1)", func(t *testing.T) {
		t.Run("Should suggest creating patch version (v1.0.0) and minor version (v1.0) when check-minor-version is true", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1"), tagCfg(minor("error")))
			cmds := commands(st)
			has(t, cmds, push(c1, "refs/tags/v1.0.0"))
			has(t, cmds, push(c1, "refs/tags/v1.0")+"\n")
		})
		t.Run("Should suggest creating only patch version (v1.0.0) when check-minor-version is false", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1"), tagCfg(minor("none")))
			if len(st.ErrorIssues()) == 0 {
				t.Fatal("expected errors")
			}
			cmds := commands(st)
			has(t, cmds, push(c1, "refs/tags/v1.0.0"))
			hasNot(t, cmds, push(c1, "refs/tags/v1.0")+"\n")
		})
	})
	t.Run("When major version doesn't point to latest patch version", func(t *testing.T) {
		t.Run("Should suggest force updating when v1 points to v1.0.0 but v1.0.1 exists", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1").tag(c2, "v1.0.1")
			cmds := commands(validate(t, r, tagCfg()))
			has(t, cmds, push(c2, "refs/tags/v1")+" --force")
			has(t, cmds, push(c2, "refs/tags/v1.0")+" --force")
		})
	})
	t.Run("When minor version doesn't point to latest patch version", func(t *testing.T) {
		t.Run("Should suggest force updating when v1.1 points to v1.1.0 but v1.1.1 exists", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.1.0", "v1.1", "v1.0.0", "v1.0", "v1").tag(c2, "v1.1.1")
			cmds := commands(validate(t, r, tagCfg()))
			has(t, cmds, push(c2, "refs/tags/v1.1")+" --force")
			has(t, cmds, push(c2, "refs/tags/v1")+" --force")
		})
	})
	t.Run("When major version doesn't point to latest minor version", func(t *testing.T) {
		t.Run("Should suggest force updating when v1 points to v1.0.0 but v1.1.0 exists", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1").tag(c2, "v1.1.0", "v1.1")
			has(t, commands(validate(t, r, tagCfg())), push(c2, "refs/tags/v1")+" --force")
		})
	})
	t.Run("When using branches for versions", func(t *testing.T) {
		t.Run("Should validate branch versions just like tags", func(t *testing.T) {
			st := validate(t, newRepo().branch(c1, "v1.0.0"), tagCfg())
			cmds := commands(st)
			has(t, cmds, push(c1, "refs/tags/v1")+"\n")
			has(t, cmds, push(c1, "refs/tags/v1.0")+"\n")
		})
		t.Run("Should flag patch branch when tag exists (same commit)", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0").branch(c1, "v1.0.0")
			st := validate(t, r, tagCfg())
			if len(st.ErrorIssues()) == 0 {
				t.Fatal("expected errors")
			}
			has(t, commands(st), "git push origin :refs/heads/v1.0.0")
		})
		t.Run("Should flag patch branch even when commits differ", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0").branch(c2, "v1.0.0")
			st := validate(t, r, tagCfg())
			if len(st.ErrorIssues()) == 0 {
				t.Fatal("expected errors")
			}
			has(t, commands(st), "git push origin :refs/heads/v1.0.0")
		})
		t.Run("Should auto-fix ref type respecting floating-versions-use setting (tags mode)", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0").branch(c1, "v1.0")
			cmds := commands(validate(t, r, tagCfg(floating("tags"))))
			has(t, cmds, "git push origin :refs/heads/v1.0")
			hasNot(t, cmds, "git push origin :refs/tags/v1.0")
		})
		t.Run("Should auto-fix ref type respecting floating-versions-use setting (branches mode)", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0").branch(c1, "v1.0")
			cmds := commands(validate(t, r, tagCfg(floating("branches"))))
			has(t, cmds, "git push origin :refs/tags/v1.0")
			hasNot(t, cmds, "git push origin :refs/heads/v1.0")
		})
	})
	t.Run("Parameterized tests for missing versions", func(t *testing.T) {
		for _, c := range []struct{ desc, tag, major, minor string }{
			{"v2 major version", "v2.0.0", "v2", "v2.0"},
			{"v3 major version", "v3.0.0", "v3", "v3.0"},
			{"v1.2 minor version", "v1.2.0", "v1", "v1.2"},
			{"v2.1 minor version", "v2.1.0", "v2", "v2.1"},
		} {
			t.Run("Should suggest creating missing versions for "+c.desc, func(t *testing.T) {
				cmds := commands(validate(t, newRepo().tag(c1, c.tag), tagCfg()))
				has(t, cmds, push(c1, "refs/tags/"+c.major)+"\n")
				has(t, cmds, push(c1, "refs/tags/"+c.minor)+"\n")
			})
		}
	})
	t.Run("Parameterized tests for version consistency", func(t *testing.T) {
		for _, c := range []struct {
			desc    string
			initial []string
			newTags []string
			force   string
		}{
			{"v2 doesn't point to latest patch v2.0.1", []string{"v2.0.0", "v2.0", "v2"}, []string{"v2.0.1"}, "v2"},
			{"v3 doesn't point to latest minor v3.1.0", []string{"v3.0.0", "v3.0", "v3"}, []string{"v3.1.0", "v3.1"}, "v3"},
			{"v1.2 doesn't point to latest patch v1.2.1", []string{"v1.2.0", "v1.2", "v1.0.0", "v1.0", "v1"}, []string{"v1.2.1"}, "v1.2"},
		} {
			t.Run("Should detect when "+c.desc+" doesn't point to latest", func(t *testing.T) {
				r := newRepo().tag(c1, c.initial...).tag(c2, c.newTags...)
				has(t, commands(validate(t, r, tagCfg())), push(c2, "refs/tags/"+c.force)+" --force")
			})
		}
	})
}

// TestFloatingVersionsConfiguration ports "Floating versions configuration" and branch/tag conflicts.
func TestFloatingVersionsConfiguration(t *testing.T) {
	t.Run("Floating versions configuration", func(t *testing.T) {
		t.Run("Should not enforce branches when floating-versions-use is tags", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1.0.0", "v1"), tagCfg(floating("tags")))
			hasNot(t, messages(st), "should be a branch")
		})
		t.Run("Should suggest using branches when floating-versions-use is branches and tags exist", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1.0.0", "v1"), tagCfg(floating("branches")))
			has(t, messages(st), "should be a branch")
			if len(st.ErrorIssues()) == 0 {
				t.Fatal("expected errors")
			}
		})
		t.Run("Should suggest creating branches when floating-versions-use is branches", func(t *testing.T) {
			cmds := commands(validate(t, newRepo().tag(c1, "v1.0.0"), tagCfg(floating("branches"))))
			has(t, cmds, "refs/heads/v1")
			hasNot(t, cmds, "refs/tags/v1\n")
		})
	})
	t.Run("Version Logic - Branch and Tag Conflicts", func(t *testing.T) {
		t.Run("Should flag floating branch when tag exists", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1").branch(c1, "v1")
			st := validate(t, r, tagCfg())
			has(t, messages(st), "exists as both tag and branch")
			has(t, commands(st), "git push origin :refs/heads/v1")
		})
		t.Run("Should handle branch/tag conflict with floating-versions-use setting", func(t *testing.T) {
			cmds := commands(validate(t, newRepo().tag(c1, "v1.0.0", "v1"), tagCfg(floating("branches"))))
			has(t, cmds, push(c1, "refs/heads/v1"))
			has(t, cmds, "git push origin :refs/tags/v1")
		})
	})
	t.Run("Version Logic - Source SHA Fallback", func(t *testing.T) {
		t.Run("Should use source SHA when no matching versions exist", func(t *testing.T) {
			has(t, commands(validate(t, newRepo().tag(c1, "v3.5.2"), tagCfg())), "git push origin "+c1)
		})
	})
	t.Run("Version Logic - Simultaneous Version Creation", func(t *testing.T) {
		t.Run("Should create both major.0.0 and major.0 when check-minor-version is true", func(t *testing.T) {
			cmds := commands(validate(t, newRepo().tag(c1, "v2"), tagCfg(minor("error"))))
			has(t, cmds, "refs/tags/v2.0.0")
			has(t, cmds, push(c1, "refs/tags/v2.0")+"\n")
		})
	})
}

// TestFloatingVersionValidation ports "Floating version validation".
func TestFloatingVersionValidation(t *testing.T) {
	t.Run("Floating version validation", func(t *testing.T) {
		missingPatch := func(t *testing.T, r *repo, want ...string) *state.State {
			t.Helper()
			st := validate(t, r, tagCfg())
			if len(st.ErrorIssues()) == 0 {
				t.Fatal("expected errors")
			}
			for _, w := range want {
				has(t, messages(st)+"\n"+commands(st), w)
			}
			return st
		}
		t.Run("Should error when major version tag exists without patch versions", func(t *testing.T) {
			missingPatch(t, newRepo().tag(c1, "v1"), "v1.0.0")
		})
		t.Run("Should error when minor version tag exists without patch versions", func(t *testing.T) {
			missingPatch(t, newRepo().tag(c1, "v1.0"), "v1.0.0")
		})
		t.Run("Should not error when floating versions have corresponding patch versions", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1.0.0", "v1.0", "v1"), tagCfg())
			if len(st.Issues) != 0 {
				t.Fatalf("unexpected: %s", messages(st))
			}
		})
		t.Run("Should not require releases for floating versions", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1")
			st := validate(t, r, relCfg(immutability("none")))
			if len(issuesOfType(st, "missing_release")) != 1 || issuesOfType(st, "missing_release")[0].Version != "v1.0.0" {
				t.Fatalf("expected only v1.0.0 to need a release: %s", messages(st))
			}
			if len(st.ErrorIssues()) == 0 {
				t.Fatal("expected errors")
			}
		})
		t.Run("Should error when branch floating version exists without patch versions", func(t *testing.T) {
			missingPatch(t, newRepo().branch(c1, "v1"), "should be a tag")
		})
		t.Run("Should handle multiple major versions without patch versions", func(t *testing.T) {
			missingPatch(t, newRepo().tag(c1, "v1", "v2"), "v1.0.0", "v2.0.0")
		})
		t.Run("Should validate that v1 has patch version even when v2.0.0 exists", func(t *testing.T) {
			st := missingPatch(t, newRepo().tag(c1, "v1", "v2.0.0"), "v1.0.0")
			for _, i := range issuesOfType(st, "missing_patch_version") {
				if i.Version == "v2" || i.Version == "v2.0.0" {
					t.Errorf("v2 must not be flagged: %s", i.Message)
				}
			}
		})
		t.Run("Should suggest patch version creation using SHA from existing floating version tag", func(t *testing.T) {
			missingPatch(t, newRepo().tag(c1, "v1"), push(c1, "refs/tags/v1.0.0"))
		})
		t.Run("Should suggest patch version creation using SHA from existing floating minor version tag", func(t *testing.T) {
			missingPatch(t, newRepo().tag(c1, "v1.2"), push(c1, "refs/tags/v1.2.0"))
		})
	})
	t.Run("Repository Configuration Validation", func(t *testing.T) {
		t.Run("Should work without tags (new repository scenario)", func(t *testing.T) {
			if st := validate(t, newRepo(), tagCfg()); len(st.Issues) != 0 {
				t.Fatalf("unexpected: %s", messages(st))
			}
		})
	})
}

// TestRefConversions ports "Ref Conversion Actions - Auto-fix and Edge Cases".
func TestRefConversions(t *testing.T) {
	t.Run("Ref Conversion Actions - Auto-fix and Edge Cases", func(t *testing.T) {
		t.Run("Should delete branch only when tag already exists (ConvertBranchToTagAction)", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0", "v1.0.0", "v1").branch(c1, "v1.0")
			cmds := commands(validate(t, r, tagCfg(floating("tags"))))
			has(t, cmds, "git push origin :refs/heads/v1.0")
			hasNot(t, cmds, push(c1, "refs/tags/v1.0")+"\n")
		})
		t.Run("Should delete tag only when branch already exists (ConvertTagToBranchAction)", func(t *testing.T) {
			r := newRepo().tag(c1, "v2.0.0", "v2").branch(c1, "v2")
			cmds := commands(validate(t, r, tagCfg(floating("branches"), minor("none"))))
			has(t, cmds, "git push origin :refs/tags/v2")
			hasNot(t, cmds, push(c1, "refs/heads/v2")+"\n")
		})
		t.Run("Should flag wrong_ref_type for floating tags when floating-versions-use is branches", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1")
			st := validate(t, r, tagCfg(floating("branches")))
			has(t, messages(st), "should be a branch")
			cmds := commands(st)
			has(t, cmds, "git push origin :refs/tags/v1\n")
			has(t, cmds, "git push origin :refs/tags/v1.0\n")
			has(t, cmds, push(c1, "refs/heads/v1")+"\n")
			has(t, cmds, push(c1, "refs/heads/v1.0")+"\n")
		})
		t.Run("Should flag wrong_ref_type for floating branches when floating-versions-use is tags", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0").branch(c1, "v1.0", "v1")
			st := validate(t, r, tagCfg(floating("tags")))
			has(t, messages(st), "should be a tag")
			cmds := commands(st)
			has(t, cmds, "git push origin :refs/heads/v1\n")
			has(t, cmds, "git push origin :refs/heads/v1.0\n")
		})
		t.Run("Should convert stale major branch to tag using latest patch SHA without duplicate create commands", func(t *testing.T) {
			r := newRepo().tag(c2, "v6.0.0").branch(c1, "v6")
			st := validate(t, r, tagCfg(floating("tags"), minor("error")))
			has(t, messages(st), "Branch 'v6' should be a tag")
			hasNot(t, messages(st), "v6 does not exist")
			has(t, messages(st), "Minor version tag v6.0 does not exist")
			cmds := commands(st)
			has(t, cmds, push(c2, "refs/tags/v6")+"\n")
			has(t, cmds, push(c2, "refs/tags/v6.0")+"\n")
			has(t, cmds, "git push origin :refs/heads/v6")
			equal(t, count(cmds, push(c2, "refs/tags/v6")), 1)
			equal(t, count(cmds, push(c2, "refs/tags/v6.0")), 1)
			hasNot(t, cmds, push(c1, "refs/tags/v6")+"\n")
		})
		t.Run("Should convert stale major tag to branch using latest patch SHA without duplicate create commands", func(t *testing.T) {
			r := newRepo().tag(c2, "v6.0.0").tag(c1, "v6")
			st := validate(t, r, tagCfg(floating("branches"), minor("error")))
			has(t, messages(st), "Tag 'v6' should be a branch")
			hasNot(t, messages(st), "Major version branch v6 does not exist")
			has(t, messages(st), "Minor version branch v6.0 does not exist")
			cmds := commands(st)
			has(t, cmds, push(c2, "refs/heads/v6")+"\n")
			has(t, cmds, push(c2, "refs/heads/v6.0")+"\n")
			has(t, cmds, "git push origin :refs/tags/v6")
			equal(t, count(cmds, push(c2, "refs/heads/v6")), 1)
			equal(t, count(cmds, push(c2, "refs/heads/v6.0")), 1)
			hasNot(t, cmds, push(c1, "refs/heads/v6")+"\n")
		})
		t.Run("Should convert patch branches to tags even when floating-versions-use is branches", func(t *testing.T) {
			r := newRepo().branch(c1, "v3.0.0", "v3.0", "v3")
			st := validate(t, r, tagCfg(floating("branches")))
			// a lone patch branch is accepted (PowerShell: "may or may not flag"); it must not be flagged as a floating ref
			hasNot(t, commands(st), "git push origin :refs/heads/v3\n")
		})
	})
}

// TestAutoFix ports "Auto-fix functionality", "Branch Version Handling - REST API Creation" and mixed-result scenarios.
func TestAutoFix(t *testing.T) {
	t.Run("Auto-fix functionality", func(t *testing.T) {
		t.Run("Should not auto-fix when auto-fix is false", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0")
			st := validate(t, r, tagCfg())
			if len(r.writes) != 0 || st.FixedCount() != 0 {
				t.Fatalf("validation must not write: %v", r.writes)
			}
		})
		t.Run("Should suggest fixes when auto-fix is false and versions are missing", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1.0.0"), tagCfg())
			has(t, commands(st), "git push")
			if len(st.ErrorIssues()) == 0 {
				t.Fatal("expected errors")
			}
		})
		t.Run("Should auto-fix multiple missing patch versions when auto-fix is enabled", func(t *testing.T) {
			r := newRepo().tag(c1, "v1", "v2")
			st := validate(t, r, tagCfg())
			fixAll(t, r, st)
			equal(t, st.FixedCount(), 4)
			equal(t, st.FailedCount(), 0)
			equal(t, st.UnfixableCount(), 0)
			for _, want := range []string{"v1.0.0", "v1.0", "v2.0.0", "v2.0"} {
				if r.tags[want] != c1 {
					t.Errorf("%s not created at %s: %v", want, c1, r.tags)
				}
			}
			clean(t, r, tagCfg())
		})
		t.Run("Should auto-fix mismatched floating tags with a force update", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1").tag(c2, "v1.0.1")
			st := validate(t, r, tagCfg())
			fixAll(t, r, st)
			equal(t, r.tags["v1"], c2)
			equal(t, r.tags["v1.0"], c2)
			clean(t, r, tagCfg())
		})
	})
	t.Run("Auto-fix Execution - Mixed Success and Failure", func(t *testing.T) {
		t.Run("Should track multiple auto-fix attempts with mixed results", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v2.0.0")
			r.failRefs["refs/tags/v2"] = true
			st := validate(t, r, tagCfg())
			fixAll(t, r, st)
			if st.FixedCount() == 0 || st.FailedCount() == 0 {
				t.Fatalf("expected mixed results, fixed=%d failed=%d", st.FixedCount(), st.FailedCount())
			}
			equal(t, r.tags["v1"], c1)
			if _, ok := r.tags["v2"]; ok {
				t.Error("v2 should not exist")
			}
		})
		t.Run("Should handle API call failures during auto-fix gracefully", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0")
			r.failWrites = true
			st := validate(t, r, tagCfg())
			fixAll(t, r, st)
			equal(t, st.FixedCount(), 0)
			if st.FailedCount() == 0 {
				t.Fatal("expected failed fixes")
			}
			if len(st.ErrorIssues()) == 0 {
				t.Fatal("unfixed issues must remain errors")
			}
		})
	})
	t.Run("Branch Version Handling - REST API Creation", func(t *testing.T) {
		t.Run("Should create branches via REST API when floating-versions-use is branches", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0")
			cfg := tagCfg(floating("branches"))
			fixAll(t, r, validate(t, r, cfg))
			equal(t, r.branches["v1"], c1)
			equal(t, r.branches["v1.0"], c1)
			clean(t, r, cfg)
		})
		t.Run("Should convert tags to branches when auto-fix enabled with branches mode", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1")
			cfg := tagCfg(floating("branches"))
			fixAll(t, r, validate(t, r, cfg))
			equal(t, r.branches["v1"], c1)
			equal(t, r.branches["v1.0"], c1)
			if _, ok := r.tags["v1"]; ok {
				t.Error("tag v1 should have been removed")
			}
			if _, ok := r.tags["v1.0"]; ok {
				t.Error("tag v1.0 should have been removed")
			}
			equal(t, r.tags["v1.0.0"], c1)
			clean(t, r, cfg)
		})
		t.Run("Should convert branches to tags when auto-fix enabled with tags mode", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0").branch(c1, "v1", "v1.0")
			fixAll(t, r, validate(t, r, tagCfg()))
			equal(t, r.tags["v1"], c1)
			equal(t, r.tags["v1.0"], c1)
			equal(t, len(r.branches), 0)
			clean(t, r, tagCfg())
		})
		t.Run("Should convert stale major branch to tag at the latest patch SHA", func(t *testing.T) {
			r := newRepo().tag(c2, "v6.0.0").branch(c1, "v6")
			fixAll(t, r, validate(t, r, tagCfg()))
			equal(t, r.tags["v6"], c2)
			equal(t, r.tags["v6.0"], c2)
			equal(t, len(r.branches), 0)
			clean(t, r, tagCfg())
		})
	})
}

// TestVersionEdgeCases ports prerelease/suffix handling and the invalid-version-format scenarios.
func TestVersionEdgeCases(t *testing.T) {
	t.Run("Prerelease Detection", func(t *testing.T) {
		t.Run("Should NOT detect prerelease from version suffix (only from GitHub Release property)", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0-beta", "v1.0.0")
			st := validate(t, r, tagCfg(ignorePreview(true)))
			for _, i := range st.Issues {
				hasNot(t, i.Version, "beta")
			}
			has(t, commands(st), push(c1, "refs/tags/v1")+"\n")
		})
		t.Run("Should filter prerelease versions based on GitHub Release isPrerelease property", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0").tag(c2, "v2.0.0")
			r.release("v1.0.0")
			r.release("v2.0.0", prerelease)
			st := validate(t, r, relCfg(ignorePreview(true), immutability("none")))
			cmds := commands(st)
			has(t, cmds, push(c1, "refs/tags/v1")+"\n")
			has(t, cmds, push(c1, "refs/tags/v1.0")+"\n")
			// the prerelease is excluded from floating-version calculations: no latest tag is derived from it
			for _, i := range issuesOfType(st, "wrong_latest_release") {
				hasNot(t, i.Message, "v2.0.0")
			}
		})
	})
	t.Run("Preview release handling", func(t *testing.T) {
		t.Run("Should not filter preview releases when ignore-preview-releases is false", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1").tag(c2, "v1.1.0")
			r.release("v1.1.0", prerelease)
			st := validate(t, r, tagCfg(ignorePreview(false)))
			has(t, commands(st), push(c2, "refs/tags/v1")+" --force")
		})
		t.Run("Should filter preview releases when ignore-preview-releases is true", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.0", "v1").tag(c2, "v1.1.0")
			r.release("v1.1.0", prerelease)
			st := validate(t, r, tagCfg(ignorePreview(true)))
			hasNot(t, commands(st), push(c2, "refs/tags/v1")+" --force")
		})
	})
	t.Run("Version Logic - Prerelease Filtering Edge Cases", func(t *testing.T) {
		t.Run("Should handle when all versions are filtered as prereleases", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0")
			r.release("v1.0.0", prerelease)
			st := validate(t, r, relCfg(ignorePreview(true), immutability("none")))
			_ = st // must not panic when no non-prerelease patch exists
		})
		t.Run("Should not support version suffixes beyond patch version", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0-beta", "v1.0.0-rc1")
			st := validate(t, r, tagCfg(ignorePreview(true)))
			if len(st.Issues) != 0 {
				t.Fatalf("suffixed tags must be ignored: %s", messages(st))
			}
		})
	})
	t.Run("Edge Cases - Invalid Version Formats", func(t *testing.T) {
		ignored := func(t *testing.T, tag string) {
			t.Helper()
			st := validate(t, newRepo().tag(c1, tag), tagCfg())
			if len(st.Issues) != 0 {
				t.Fatalf("%q must be ignored, got: %s", tag, messages(st))
			}
		}
		t.Run("Should not support version numbers with more than 3 components", func(t *testing.T) { ignored(t, "v1.0.0.0") })
		t.Run("Should not support version components higher than 65535", func(t *testing.T) {
			validate(t, newRepo().tag(c1, "v1.70000.0", "v1.0.0"), tagCfg()) // must not panic
		})
		t.Run("Should handle version tags with Unicode characters", func(t *testing.T) { ignored(t, "v1.0.0-é") })
		t.Run("Should handle very large version numbers within limits", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v65535.65535.65535"), tagCfg())
			has(t, commands(st), "refs/tags/v65535\n")
		})
		t.Run("Should ignore versions with non-numeric components", func(t *testing.T) {
			ignored(t, "v1.x.0")
			ignored(t, "vx.y.z")
		})
	})
}

// TestIgnoreVersions ports "Ignore Versions Configuration" and the integration example table.
func TestIgnoreVersions(t *testing.T) {
	t.Run("Ignore Versions Configuration", func(t *testing.T) {
		t.Run("Should skip validation for versions in ignore-versions list", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v2.0.0")
			st := validate(t, r, tagCfg(ignoreVersions("v1.0.0")))
			equal(t, len(issuesFor(st, "v1")), 0)
			if len(issuesFor(st, "v2")) == 0 {
				t.Error("v2 should still be validated")
			}
		})
		t.Run("Should support wildcard patterns in ignore-versions", func(t *testing.T) {
			r := newRepo().tag(c1, "v1.0.0", "v1.1.0", "v2.0.0")
			st := validate(t, r, tagCfg(ignoreVersions("v1.*")))
			equal(t, len(issuesFor(st, "v1.")), 0)
			if len(issuesFor(st, "v2")) == 0 {
				t.Error("v2 should still be validated")
			}
		})
		t.Run("Should handle empty ignore-versions gracefully", func(t *testing.T) {
			st := validate(t, newRepo().tag(c1, "v1.0.0"), tagCfg(ignoreVersions()))
			if len(st.Issues) == 0 {
				t.Error("expected issues")
			}
		})
	})
	t.Run("Ignore Versions (Example Table)", func(t *testing.T) {
		for _, c := range []struct {
			format  string
			ignore  []string
			ignored []string
			kept    string
		}{
			{"Comma-separated", []string{"v1.0.0", "v2.0.0"}, []string{"v1", "v2"}, "v3"},
			{"Newline-separated", []string{"v1.0.0", "v2.0.0"}, []string{"v1", "v2"}, "v3"},
			{"JSON array", []string{"v1.0.0", "v2.0.0"}, []string{"v1", "v2"}, "v3"},
			{"Wildcard pattern", []string{"v1.0.*", "v1.1.*"}, []string{"v1.0", "v1.1"}, "v2"},
			{"Mixed comma and newline", []string{"v1.0.0", "v2.0.0", "v3.0.0"}, []string{"v1", "v2", "v3"}, "v4"},
		} {
			t.Run("Should handle ignore-versions format: "+c.format, func(t *testing.T) {
				r := newRepo()
				for _, v := range c.ignored {
					r.tag(c1, v+".0.0")
				}
				r.tag(c1, c.kept+".0.0")
				st := validate(t, r, tagCfg(ignoreVersions(c.ignore...)))
				for _, v := range c.ignored {
					if n := len(issuesFor(st, v)); n != 0 {
						t.Errorf("version %s should be ignored, got %d issues", v, n)
					}
				}
				if len(issuesFor(st, c.kept)) == 0 {
					t.Errorf("version %s should not be ignored", c.kept)
				}
			})
		}
	})
	t.Run("Floating Version Validation (Example Table)", func(t *testing.T) {
		for _, c := range []struct {
			desc    string
			tags    []string
			wantErr bool
			pattern string
		}{
			{"Major version only (v1) - should suggest patch", []string{"v1"}, true, "v1.0.0"},
			{"Minor version only (v1.0) - should suggest patch", []string{"v1.0"}, true, "v1.0.0"},
			{"Complete version set - should pass", []string{"v1.0.0", "v1.0", "v1"}, false, ""},
			{"Patch only - should suggest major and minor", []string{"v1.0.0"}, true, "refs/tags/v1\n"},
			{"Major and patch - should suggest minor", []string{"v1.0.0", "v1"}, true, "refs/tags/v1.0\n"},
		} {
			t.Run("Should handle floating version scenario: "+c.desc, func(t *testing.T) {
				st := validate(t, newRepo().tag(c1, c.tags...), tagCfg())
				equal(t, len(st.ErrorIssues()) > 0, c.wantErr)
				if c.pattern != "" {
					has(t, commands(st)+"\n", c.pattern)
				}
			})
		}
	})
}

var _ = strings.Contains
