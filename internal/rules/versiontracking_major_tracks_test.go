package rules

import "testing"

func TestMajorTagTracksHighestPatch(t *testing.T) {
	const name = "major_tag_tracks_highest_patch"
	vtMeta(t, name, 20)
	t.Run("Condition - AppliesWhen floating-versions-use is tags", func(t *testing.T) {
		vtRunCond(t, name, []vtCond{
			{name: "should return major tags when floating-versions-use is 'tags'", refs: []vtRef{tg("v1", "abc123")}, cfg: vtTags, want: 1},
			{name: "should return empty when floating-versions-use is 'branches'", refs: []vtRef{tg("v1", "abc123")}, cfg: vtBranches, want: 0},
			{name: "should skip ignored versions", refs: []vtRef{ign(tg("v1", "abc123"))}, cfg: vtTags, want: 0},
			{name: "should return multiple major tags", refs: []vtRef{tg("v1", "abc123"), tg("v2", "def456")}, cfg: vtTags, want: 2},
		})
	})
	t.Run("Check - SHA matching", func(t *testing.T) {
		vtRunCheck(t, name, []vtCheck{
			{name: "should pass when major tag points to highest patch", refs: []vtRef{tg("v1", "abc123"), tg("v1.0.0", "abc123")}, item: "v1", ignorePre: true, want: true},
			{name: "should fail when major tag points to wrong SHA", refs: []vtRef{tg("v1", "old123"), tg("v1.0.0", "abc123")}, item: "v1", ignorePre: true, want: false},
			{name: "should pass when no patches exist (nothing to track)", refs: []vtRef{tg("v1", "abc123")}, item: "v1", ignorePre: true, want: true},
			{name: "should track highest patch across minor versions", refs: []vtRef{tg("v1", "abc123"), tg("v1.0.0", "old100"), tg("v1.1.0", "abc123")}, item: "v1", ignorePre: true, want: true},
		})
	})
	t.Run("CreateIssue", func(t *testing.T) {
		vtRunIssue(t, name, []vtIssue{
			{name: "should create issue with correct details", refs: []vtRef{tg("v1", "old123"), tg("v1.0.0", "abc123")}, item: "v1", cfg: vtPreTags,
				typ: "major_tag_wrong_target", version: "v1", currentSha: "old123", expectedSha: "abc123", checkAction: wantUpdateTag("v1", "abc123"),
				message: "v1 points to old123 but should point to v1.0.0 at abc123"},
			{name: "should configure UpdateTagAction with force flag", refs: []vtRef{tg("v2", "old123"), tg("v2.5.3", "new456")}, item: "v2", cfg: vtPreTags,
				typ: "major_tag_wrong_target", currentSha: "old123", checkAction: wantUpdateTag("v2", "new456")},
		})
	})
	t.Run("Prerelease Filtering", func(t *testing.T) {
		vtRunCheck(t, name, []vtCheck{
			{name: "should pass Check when major tag exists but only prerelease patches exist and ignore-preview-releases is true", prerelease: []string{"v2.0.0"}, refs: []vtRef{tg("v2", "preview123"), tg("v2.0.0", "preview123")}, item: "v2", ignorePre: true, want: true},
			{name: "should exclude prerelease patch when calculating highest (ignore-preview-releases=true)", prerelease: []string{"v1.1.0"}, refs: []vtRef{tg("v1", "stable123"), tg("v1.0.0", "stable123"), tg("v1.1.0", "prerel456")}, item: "v1", ignorePre: true, want: true},
			{name: "should include prerelease patch when ignore-preview-releases is false", prerelease: []string{"v1.1.0"}, refs: []vtRef{tg("v1", "stable123"), tg("v1.0.0", "stable123"), tg("v1.1.0", "prerel456")}, item: "v1", ignorePre: false, want: false},
		})
		vtRunIssue(t, name, []vtIssue{
			{name: "should use non-prerelease SHA in CreateIssue when ignore-preview-releases is true", prerelease: []string{"v1.1.0"}, refs: []vtRef{tg("v1", "old123"), tg("v1.0.0", "stable456"), tg("v1.1.0", "prerel789")}, item: "v1", cfg: vtPreTags,
				typ: "major_tag_wrong_target", currentSha: "old123", expectedSha: "stable456"},
		})
	})
}

func TestMajorBranchTracksHighestPatch(t *testing.T) {
	const name = "major_branch_tracks_highest_patch"
	vtMeta(t, name, 20)
	t.Run("Condition - AppliesWhen floating-versions-use is branches", func(t *testing.T) {
		vtRunCond(t, name, []vtCond{
			{name: "should return major branches when floating-versions-use is 'branches'", refs: []vtRef{br("v1", "abc123"), br("v2", "def456")}, cfg: vtBranches, want: 2},
			{name: "should return empty when floating-versions-use is 'tags'", refs: []vtRef{br("v1", "abc123")}, cfg: vtTags, want: 0},
			{name: "should skip ignored versions", refs: []vtRef{ign(br("v1", "abc123")), br("v2", "def456")}, cfg: vtBranches, want: 1},
			{name: "should return multiple major branches", refs: []vtRef{br("v1", "abc123"), br("v2", "def456"), br("v3", "ghi789")}, cfg: vtBranches, want: 3},
		})
		st := vtState(nil, ign(br("v1", "abc123")), br("v2", "def456"))
		got := vtRule(t, name).Condition(st, &vtBranches)
		if len(got) != 1 || vtFindRef(t, st, "v2") != got[0] {
			t.Fatalf("expected only v2, got %v", got)
		}
	})
	t.Run("Check - SHA matching", func(t *testing.T) {
		vtRunCheck(t, name, []vtCheck{
			{name: "should pass when major branch points to highest patch", refs: []vtRef{br("v1", "abc123"), tg("v1.0.0", "abc123")}, item: "v1", ignorePre: true, want: true},
			{name: "should fail when major branch points to wrong SHA", refs: []vtRef{br("v1", "old123"), tg("v1.0.0", "abc123")}, item: "v1", ignorePre: true, want: false},
			{name: "should pass when no patches exist (nothing to track)", refs: []vtRef{br("v1", "abc123")}, item: "v1", ignorePre: true, want: true},
			{name: "should track highest patch across minor versions", refs: []vtRef{br("v1", "latest"), tg("v1.0.0", "old123"), tg("v1.1.0", "latest")}, item: "v1", ignorePre: true, want: true},
		})
	})
	t.Run("CreateIssue", func(t *testing.T) {
		vtRunIssue(t, name, []vtIssue{
			{name: "should create issue with correct details", refs: []vtRef{br("v1", "old123"), tg("v1.0.0", "new456")}, item: "v1", cfg: vtPreBr,
				typ: "major_branch_wrong_target", version: "v1", currentSha: "old123", expectedSha: "new456", checkAction: wantUpdateBranch("v1", "new456"),
				message: "v1 points to old123 but should point to v1.0.0 at new456"},
			{name: "should configure UpdateBranchAction with correct SHA", refs: []vtRef{br("v2", "old123"), tg("v2.0.0", "old456"), tg("v2.1.0", "new789")}, item: "v2", cfg: vtPreBr,
				typ: "major_branch_wrong_target", currentSha: "old123", checkAction: wantUpdateBranch("v2", "new789")},
		})
	})
	t.Run("Prerelease Filtering", func(t *testing.T) {
		vtRunCheck(t, name, []vtCheck{
			{name: "should pass Check when major branch exists but only prerelease patches exist and ignore-preview-releases is true", prerelease: []string{"v2.0.0"}, refs: []vtRef{br("v2", "preview123"), tg("v2.0.0", "preview123")}, item: "v2", ignorePre: true, want: true},
			{name: "should exclude prerelease patch when calculating highest (ignore-preview-releases=true)", prerelease: []string{"v1.1.0"}, refs: []vtRef{br("v1", "stable123"), tg("v1.0.0", "stable123"), tg("v1.1.0", "prerel456")}, item: "v1", ignorePre: true, want: true},
			{name: "should include prerelease patch when ignore-preview-releases is false", prerelease: []string{"v1.1.0"}, refs: []vtRef{br("v1", "stable123"), tg("v1.0.0", "stable123"), tg("v1.1.0", "prerel456")}, item: "v1", ignorePre: false, want: false},
		})
		vtRunIssue(t, name, []vtIssue{
			{name: "should use non-prerelease SHA in CreateIssue when ignore-preview-releases is true", prerelease: []string{"v1.1.0"}, refs: []vtRef{br("v1", "old123"), tg("v1.0.0", "stable456"), tg("v1.1.0", "prerel789")}, item: "v1", cfg: vtPreBr,
				typ: "major_branch_wrong_target", currentSha: "old123", expectedSha: "stable456"},
		})
	})
}
