package rules

import (
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

var (
	vtTags     = state.Config{FloatingVersionsUse: "tags"}
	vtBranches = state.Config{FloatingVersionsUse: "branches"}
	vtPreTags  = state.Config{FloatingVersionsUse: "tags", IgnorePreviewReleases: true}
	vtPreBr    = state.Config{FloatingVersionsUse: "branches", IgnorePreviewReleases: true}
)

func TestMajorTagMissing(t *testing.T) {
	vtMeta(t, "major_tag_missing", 21)
	t.Run("Condition - AppliesWhen floating-versions-use is tags", func(t *testing.T) {
		vtRunCond(t, "major_tag_missing", []vtCond{
			{name: "should return missing major when patch exists but major tag doesn't", refs: []vtRef{tg("v1.0.0", "abc123")}, cfg: vtTags, want: 1},
			{name: "should not return major when major tag exists", refs: []vtRef{tg("v1", "abc123"), tg("v1.0.0", "abc123")}, cfg: vtTags, want: 0},
			{name: "should not return missing major when same version branch exists", refs: []vtRef{tg("v1.0.0", "patch123"), br("v1", "stale456")}, cfg: vtTags, want: 0},
			{name: "should return empty when floating-versions-use is 'branches'", refs: []vtRef{tg("v1.0.0", "abc123")}, cfg: vtBranches, want: 0},
			{name: "should return empty when no patches exist", cfg: vtTags, want: 0},
			{name: "should return multiple missing majors", refs: []vtRef{tg("v1.0.0", "abc123"), tg("v2.0.0", "def456")}, cfg: vtTags, want: 2},
			{name: "should skip ignored patch versions", refs: []vtRef{ign(tg("v1.0.0", "abc123"))}, cfg: vtTags, want: 0},
			{name: "should find patches from both tags and branches", refs: []vtRef{tg("v1.0.0", "abc123"), br("v2.0.0", "def456")}, cfg: vtTags, want: 2},
			{name: "should NOT return missing major when only prerelease patches exist and ignore-preview-releases is true", prerelease: []string{"v2.0.0"}, refs: []vtRef{tg("v2.0.0", "preview123")}, cfg: vtPreTags, want: 0},
		})
	})
	t.Run("Condition - return value", func(t *testing.T) {
		st := vtState(nil, tg("v1.0.0", "abc123"))
		got := vtRule(t, "major_tag_missing").Condition(st, &vtTags)
		if len(got) != 1 || got[0].(majorItem).Major != 1 {
			t.Fatalf("got %#v", got)
		}
	})
	t.Run("CreateIssue", func(t *testing.T) {
		vtRunIssue(t, "major_tag_missing", []vtIssue{
			{name: "should create issue with correct details", refs: []vtRef{tg("v1.0.0", "abc123")}, missing: majorItem{1}, cfg: vtPreTags,
				typ: "major_tag_missing", version: "v1", expectedSha: "abc123", checkAction: wantCreateTag("v1", "abc123"),
				message: "v1 does not exist. It should point to v1.0.0 at abc123"},
			{name: "should configure CreateTagAction with highest patch SHA", refs: []vtRef{tg("v2.0.0", "old123"), tg("v2.1.0", "new456")}, missing: majorItem{2}, cfg: vtPreTags,
				typ: "major_tag_missing", checkAction: wantCreateTag("v2", "new456")},
		})
	})
	t.Run("Prerelease Filtering", func(t *testing.T) {
		refs := []vtRef{tg("v1.0.0", "stable123"), tg("v1.1.0", "prerel456")}
		vtRunIssue(t, "major_tag_missing", []vtIssue{
			{name: "should use non-prerelease SHA in CreateIssue when ignore-preview-releases is true", prerelease: []string{"v1.1.0"}, refs: refs, missing: majorItem{1}, cfg: vtPreTags,
				typ: "major_tag_missing", expectedSha: "stable123"},
			{name: "should use prerelease SHA in CreateIssue when ignore-preview-releases is false", prerelease: []string{"v1.1.0"}, refs: refs, missing: majorItem{1}, cfg: vtTags,
				typ: "major_tag_missing", expectedSha: "prerel456"},
		})
	})
}

func TestMajorBranchMissing(t *testing.T) {
	vtMeta(t, "major_branch_missing", 21)
	t.Run("Condition - AppliesWhen floating-versions-use is branches", func(t *testing.T) {
		vtRunCond(t, "major_branch_missing", []vtCond{
			{name: "should return missing major when patch exists but major branch doesn't", refs: []vtRef{tg("v1.0.0", "abc123")}, cfg: vtBranches, want: 1},
			{name: "should not return major when major branch exists", refs: []vtRef{br("v1", "abc123"), tg("v1.0.0", "abc123")}, cfg: vtBranches, want: 0},
			{name: "should not return missing major when same version tag exists", refs: []vtRef{tg("v1", "stale456"), tg("v1.0.0", "patch123")}, cfg: vtBranches, want: 0},
			{name: "should return empty when floating-versions-use is 'tags'", refs: []vtRef{tg("v1.0.0", "abc123")}, cfg: vtTags, want: 0},
			{name: "should return empty when no patches exist", cfg: vtBranches, want: 0},
			{name: "should return multiple missing majors", refs: []vtRef{tg("v1.0.0", "abc123"), tg("v2.0.0", "def456")}, cfg: vtBranches, want: 2},
			{name: "should skip ignored patch versions", refs: []vtRef{ign(tg("v1.0.0", "abc123"))}, cfg: vtBranches, want: 0},
			{name: "should find patches from both tags and branches", refs: []vtRef{tg("v1.0.0", "abc123"), br("v2.0.0", "def456")}, cfg: vtBranches, want: 2},
			{name: "should NOT return missing major when only prerelease patches exist and ignore-preview-releases is true", prerelease: []string{"v2.0.0"}, refs: []vtRef{tg("v2.0.0", "preview123")}, cfg: vtPreBr, want: 0},
		})
	})
	t.Run("CreateIssue", func(t *testing.T) {
		vtRunIssue(t, "major_branch_missing", []vtIssue{
			{name: "should create issue with correct details", refs: []vtRef{tg("v1.0.0", "abc123")}, missing: majorItem{1}, cfg: vtPreBr,
				typ: "major_branch_missing", version: "v1", expectedSha: "abc123", checkAction: wantCreateBranch("v1", "abc123"),
				message: "Major version branch v1 does not exist. It should point to v1.0.0 at abc123"},
			{name: "should configure CreateBranchAction with highest patch SHA", refs: []vtRef{tg("v2.0.0", "old123"), tg("v2.1.0", "new456")}, missing: majorItem{2}, cfg: vtPreBr,
				typ: "major_branch_missing", checkAction: wantCreateBranch("v2", "new456")},
		})
	})
	t.Run("Prerelease Filtering", func(t *testing.T) {
		refs := []vtRef{tg("v1.0.0", "stable123"), tg("v1.1.0", "prerel456")}
		vtRunIssue(t, "major_branch_missing", []vtIssue{
			{name: "should use non-prerelease SHA in CreateIssue when ignore-preview-releases is true", prerelease: []string{"v1.1.0"}, refs: refs, missing: majorItem{1}, cfg: vtPreBr,
				typ: "major_branch_missing", expectedSha: "stable123"},
			{name: "should use prerelease SHA in CreateIssue when ignore-preview-releases is false", prerelease: []string{"v1.1.0"}, refs: refs, missing: majorItem{1}, cfg: vtBranches,
				typ: "major_branch_missing", expectedSha: "prerel456"},
		})
	})
}
