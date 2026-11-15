package rules

import "testing"

func TestMinorTagTracksHighestPatch(t *testing.T) {
	const name = "minor_tag_tracks_highest_patch"
	vtMeta(t, name, 22)
	t.Run("Condition - AppliesWhen configuration", func(t *testing.T) {
		vtRunCond(t, name, []vtCond{
			{name: "should return minor tags when both settings are enabled", refs: []vtRef{tg("v1.0", "abc123")}, cfg: vtMinorCfg("tags", "error", false), want: 1},
			{name: "should return empty when floating-versions-use is 'branches'", refs: []vtRef{tg("v1.0", "abc123")}, cfg: vtMinorCfg("branches", "error", false), want: 0},
			{name: "should return empty when check-minor-version is 'none'", refs: []vtRef{tg("v1.0", "abc123")}, cfg: vtMinorCfg("tags", "none", false), want: 0},
			{name: "should skip ignored versions", refs: []vtRef{ign(tg("v1.0", "abc123"))}, cfg: vtMinorCfg("tags", "error", false), want: 0},
		})
	})
	t.Run("Check - SHA matching", func(t *testing.T) {
		vtRunCheck(t, name, []vtCheck{
			{name: "should pass when minor tag points to highest patch", refs: []vtRef{tg("v1.0", "abc123"), tg("v1.0.0", "abc123")}, item: "v1.0", ignorePre: true, want: true},
			{name: "should fail when minor tag points to wrong SHA", refs: []vtRef{tg("v1.0", "old123"), tg("v1.0.0", "abc123")}, item: "v1.0", ignorePre: true, want: false},
			{name: "should pass when no patches exist (nothing to track)", refs: []vtRef{tg("v1.0", "abc123")}, item: "v1.0", ignorePre: true, want: true},
			{name: "should track highest patch within minor series", refs: []vtRef{tg("v1.0", "abc123"), tg("v1.0.0", "old100"), tg("v1.0.1", "abc123"), tg("v1.1.0", "different")}, item: "v1.0", ignorePre: true, want: true},
		})
	})
	t.Run("CreateIssue", func(t *testing.T) {
		vtRunIssue(t, name, []vtIssue{
			{name: "should create issue with error severity", refs: []vtRef{tg("v1.0", "old123"), tg("v1.0.0", "abc123")}, item: "v1.0", cfg: vtMinorCfg("tags", "error", true),
				typ: "minor_tag_wrong_target", version: "v1.0", currentSha: "old123", expectedSha: "abc123", checkAction: wantUpdateTag("v1.0", "abc123"),
				message: "v1.0 points to old123 but should point to v1.0.0 at abc123"},
			{name: "should create issue with warning severity", refs: []vtRef{tg("v1.0", "old123"), tg("v1.0.0", "abc123")}, item: "v1.0", cfg: vtMinorCfg("tags", "warning", true),
				typ: "minor_tag_wrong_target", severity: "warning", currentSha: "old123"},
			{name: "should configure UpdateTagAction with force flag", refs: []vtRef{tg("v2.1", "old123"), tg("v2.1.5", "new456")}, item: "v2.1", cfg: vtMinorCfg("tags", "error", true),
				typ: "minor_tag_wrong_target", currentSha: "old123", checkAction: wantUpdateTag("v2.1", "new456")},
		})
	})
	t.Run("Prerelease Filtering", func(t *testing.T) {
		vtRunCheck(t, name, []vtCheck{
			{name: "should pass Check when minor tag exists but only prerelease patches exist in series and ignore-preview-releases is true", prerelease: []string{"v1.1.0"}, refs: []vtRef{tg("v1.1", "preview456"), tg("v1.1.0", "preview456"), tg("v1.0.0", "stable123")}, item: "v1.1", ignorePre: true, want: true},
			{name: "should exclude prerelease patch when calculating highest (ignore-preview-releases=true)", prerelease: []string{"v1.0.1"}, refs: []vtRef{tg("v1.0", "stable123"), tg("v1.0.0", "stable123"), tg("v1.0.1", "prerel456")}, item: "v1.0", ignorePre: true, want: true},
			{name: "should include prerelease patch when ignore-preview-releases is false", prerelease: []string{"v1.0.1"}, refs: []vtRef{tg("v1.0", "stable123"), tg("v1.0.0", "stable123"), tg("v1.0.1", "prerel456")}, item: "v1.0", ignorePre: false, want: false},
		})
		vtRunIssue(t, name, []vtIssue{
			{name: "should use non-prerelease SHA in CreateIssue when ignore-preview-releases is true", prerelease: []string{"v1.0.1"}, refs: []vtRef{tg("v1.0", "old123"), tg("v1.0.0", "stable456"), tg("v1.0.1", "prerel789")}, item: "v1.0", cfg: vtMinorCfg("tags", "error", true),
				typ: "minor_tag_wrong_target", currentSha: "old123", expectedSha: "stable456"},
		})
	})
}

func TestMinorBranchTracksHighestPatch(t *testing.T) {
	const name = "minor_branch_tracks_highest_patch"
	vtMeta(t, name, 22)
	t.Run("Condition - AppliesWhen configuration", func(t *testing.T) {
		vtRunCond(t, name, []vtCond{
			{name: "should return minor branches when both settings are enabled", refs: []vtRef{br("v1.0", "abc123"), br("v1.1", "def456")}, cfg: vtMinorCfg("branches", "error", false), want: 2},
			{name: "should return empty when floating-versions-use is 'tags'", refs: []vtRef{br("v1.0", "abc123")}, cfg: vtMinorCfg("tags", "error", false), want: 0},
			{name: "should return empty when check-minor-version is 'none'", refs: []vtRef{br("v1.0", "abc123")}, cfg: vtMinorCfg("branches", "none", false), want: 0},
			{name: "should skip ignored versions", refs: []vtRef{ign(br("v1.0", "abc123")), br("v1.1", "def456")}, cfg: vtMinorCfg("branches", "error", false), want: 1},
		})
	})
	t.Run("Check - SHA matching", func(t *testing.T) {
		vtRunCheck(t, name, []vtCheck{
			{name: "should pass when minor branch points to highest patch", refs: []vtRef{br("v1.0", "abc123"), tg("v1.0.0", "abc123")}, item: "v1.0", ignorePre: true, want: true},
			{name: "should fail when minor branch points to wrong SHA", refs: []vtRef{br("v1.0", "old123"), tg("v1.0.0", "abc123")}, item: "v1.0", ignorePre: true, want: false},
			{name: "should pass when no patches exist (nothing to track)", refs: []vtRef{br("v1.0", "abc123")}, item: "v1.0", ignorePre: true, want: true},
			{name: "should track highest patch within minor series", refs: []vtRef{br("v1.0", "latest"), tg("v1.0.0", "old123"), tg("v1.0.1", "latest")}, item: "v1.0", ignorePre: true, want: true},
		})
	})
	t.Run("CreateIssue", func(t *testing.T) {
		vtRunIssue(t, name, []vtIssue{
			{name: "should create issue with error severity", refs: []vtRef{br("v1.0", "old123"), tg("v1.0.0", "new456")}, item: "v1.0", cfg: vtMinorCfg("branches", "error", true),
				typ: "minor_branch_wrong_target", version: "v1.0", currentSha: "old123", expectedSha: "new456", checkAction: wantUpdateBranch("v1.0", "new456"),
				message: "v1.0 points to old123 but should point to v1.0.0 at new456"},
			{name: "should create issue with warning severity", refs: []vtRef{br("v1.0", "old123"), tg("v1.0.0", "new456")}, item: "v1.0", cfg: vtMinorCfg("branches", "warning", true),
				typ: "minor_branch_wrong_target", severity: "warning", currentSha: "old123"},
			{name: "should configure UpdateBranchAction with correct SHA", refs: []vtRef{br("v2.1", "old123"), tg("v2.1.0", "old456"), tg("v2.1.1", "new789")}, item: "v2.1", cfg: vtMinorCfg("branches", "error", true),
				typ: "minor_branch_wrong_target", currentSha: "old123", checkAction: wantUpdateBranch("v2.1", "new789")},
		})
	})
	t.Run("Prerelease Filtering", func(t *testing.T) {
		vtRunCheck(t, name, []vtCheck{
			{name: "should pass Check when minor branch exists but only prerelease patches exist in series and ignore-preview-releases is true", prerelease: []string{"v1.1.0"}, refs: []vtRef{br("v1.1", "preview456"), tg("v1.1.0", "preview456"), tg("v1.0.0", "stable123")}, item: "v1.1", ignorePre: true, want: true},
			{name: "should exclude prerelease patch when calculating highest (ignore-preview-releases=true)", prerelease: []string{"v1.0.1"}, refs: []vtRef{br("v1.0", "stable123"), tg("v1.0.0", "stable123"), tg("v1.0.1", "prerel456")}, item: "v1.0", ignorePre: true, want: true},
			{name: "should include prerelease patch when ignore-preview-releases is false", prerelease: []string{"v1.0.1"}, refs: []vtRef{br("v1.0", "stable123"), tg("v1.0.0", "stable123"), tg("v1.0.1", "prerel456")}, item: "v1.0", ignorePre: false, want: false},
		})
		vtRunIssue(t, name, []vtIssue{
			{name: "should use non-prerelease SHA in CreateIssue when ignore-preview-releases is true", prerelease: []string{"v1.0.1"}, refs: []vtRef{br("v1.0", "old123"), tg("v1.0.0", "stable456"), tg("v1.0.1", "prerel789")}, item: "v1.0", cfg: vtMinorCfg("branches", "error", true),
				typ: "minor_branch_wrong_target", currentSha: "old123", expectedSha: "stable456"},
		})
	})
}
