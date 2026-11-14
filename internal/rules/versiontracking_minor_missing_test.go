package rules

import (
	"testing"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

func vtMinorCfg(use, check string, pre bool) state.Config {
	return state.Config{FloatingVersionsUse: use, CheckMinorVersion: check, IgnorePreviewReleases: pre}
}

func vtMinorMissingConds(use, other string) []vtCond {
	cfg := vtMinorCfg(use, "error", false)
	return []vtCond{
		{name: "should return missing minor when patch exists but minor " + "ref doesn't", refs: []vtRef{tg("v1.0.0", "abc123")}, cfg: cfg, want: 1},
		{name: "should return empty when no patches exist", cfg: cfg, want: 0},
		{name: "should return multiple missing minors", refs: []vtRef{tg("v1.0.0", "abc123"), tg("v1.1.0", "def456"), tg("v2.0.0", "ghi789")}, cfg: cfg, want: 3},
		{name: "should skip ignored patch versions", refs: []vtRef{ign(tg("v1.0.0", "abc123"))}, cfg: cfg, want: 0},
		{name: "should find patches from both tags and branches", refs: []vtRef{tg("v1.0.0", "abc123"), br("v2.0.0", "def456")}, cfg: cfg, want: 2},
		{name: "should handle multiple patches in same minor series correctly", refs: []vtRef{tg("v1.0.0", "abc123"), tg("v1.0.1", "abc124"), tg("v1.0.2", "abc125")}, cfg: cfg, want: 1},
		{name: "should return empty when floating-versions-use is '" + other + "'", refs: []vtRef{tg("v1.0.0", "abc123")}, cfg: vtMinorCfg(other, "error", false), want: 0},
		{name: "should return empty when check-minor-version is 'none'", refs: []vtRef{tg("v1.0.0", "abc123")}, cfg: vtMinorCfg(use, "none", false), want: 0},
	}
}

func TestMinorTagMissing(t *testing.T) {
	const name = "minor_tag_missing"
	vtMeta(t, name, 23)
	tags := vtMinorCfg("tags", "error", false)
	t.Run("Condition - AppliesWhen floating-versions-use is tags and check-minor-version is enabled", func(t *testing.T) {
		conds := vtMinorMissingConds("tags", "branches")
		conds[0].name = "should return missing minor when patch exists but minor tag doesn't"
		conds = append(conds, []vtCond{
			{name: "should not return minor when minor tag exists", refs: []vtRef{tg("v1.0", "abc123"), tg("v1.0.0", "abc123")}, cfg: tags, want: 0},
			{name: "should not return missing minor when same version branch exists", refs: []vtRef{tg("v1.0.0", "patch123"), br("v1.0", "stale456")}, cfg: tags, want: 0},
			{name: "should NOT return missing minor when only prerelease patches exist in that series and ignore-preview-releases is true", prerelease: []string{"v1.1.0"},
				refs: []vtRef{tg("v1.0.0", "stable123"), tg("v1.0", "stable123"), tg("v1", "stable123"), tg("v1.1.0", "preview456")}, cfg: vtMinorCfg("tags", "error", true), want: 0},
			{name: "should return v{major}.0 when major tag exists without patches", refs: []vtRef{tg("v1", "major123")}, cfg: tags, want: 1},
			{name: "should not return v{major}.0 when major tag has patches", refs: []vtRef{tg("v1", "major123"), tg("v1.0.0", "patch456")}, cfg: tags, want: 1},
			{name: "should not return v{major}.0 when minor tag already exists", refs: []vtRef{tg("v1", "major123"), tg("v1.0", "minor456")}, cfg: tags, want: 0},
			{name: "should handle multiple major tags without patches", refs: []vtRef{tg("v1", "major1"), tg("v2", "major2")}, cfg: tags, want: 2},
			{name: "should skip ignored major tags", refs: []vtRef{ign(tg("v1", "major123"))}, cfg: tags, want: 0},
		}...)
		vtRunCond(t, name, conds)
	})
	t.Run("Condition - item details", func(t *testing.T) {
		rule := vtRule(t, name)
		st := vtState(nil, tg("v1.0.0", "abc123"))
		if got := rule.Condition(st, &tags); len(got) != 1 || got[0].(minorItem) != (minorItem{Major: 1, Minor: 0}) {
			t.Fatalf("should return missing minor, got %#v", got)
		}
		st = vtState(nil, tg("v1.0.0", "abc123"), tg("v1.0.1", "abc124"), tg("v1.0.2", "abc125"))
		if got := rule.Condition(st, &tags); len(got) != 1 || got[0].(minorItem) != (minorItem{Major: 1, Minor: 0}) {
			t.Fatalf("should only report v1.0 once, got %#v", got)
		}
		st = vtState(nil, tg("v1", "major123"))
		if got := rule.Condition(st, &tags); len(got) != 1 || got[0].(minorItem) != (minorItem{Major: 1, Minor: 0, SourceSha: "major123"}) {
			t.Fatalf("should return v{major}.0 with SourceSha, got %#v", got)
		}
		st = vtState(nil, tg("v1", "major123"), tg("v1.0.0", "patch456"))
		got := rule.Condition(st, &tags)
		if len(got) != 1 || got[0].(minorItem).SourceSha != "" {
			t.Fatalf("patch-derived item must not have SourceSha, got %#v", got)
		}
		cfg := vtMinorCfg("tags", "error", true)
		issue := rule.CreateIssue(got[0], st, &cfg)
		if issue.ExpectedSha != "patch456" {
			t.Fatalf("ExpectedSha = %s", issue.ExpectedSha)
		}
		wantCreateTag("v1.0", "patch456")(t, issue.Action)
		st = vtState(nil, tg("v1", "major1"), tg("v2", "major2"))
		got = rule.Condition(st, &tags)
		if len(got) != 2 || got[0].(minorItem) != (minorItem{Major: 1, SourceSha: "major1"}) || got[1].(minorItem) != (minorItem{Major: 2, SourceSha: "major2"}) {
			t.Fatalf("multiple major tags without patches: %#v", got)
		}
	})
	t.Run("CreateIssue", func(t *testing.T) {
		pre := vtMinorCfg("tags", "error", true)
		warn := vtMinorCfg("tags", "warning", true)
		vtRunIssue(t, name, []vtIssue{
			{name: "should create issue with correct details", refs: []vtRef{tg("v1.0.0", "abc123")}, missing: minorItem{Major: 1}, cfg: pre,
				typ: "missing_minor_version", version: "v1.0", checkAction: wantCreateTag("v1.0", "abc123"),
				message: "Minor version tag v1.0 does not exist. It should point to v1.0.0 at abc123"},
			{name: "should create warning severity when check-minor-version is warning", refs: []vtRef{tg("v1.0.0", "abc123")}, missing: minorItem{Major: 1}, cfg: warn,
				typ: "missing_minor_version", severity: "warning"},
			{name: "should configure CreateTagAction with highest patch SHA in minor series", refs: []vtRef{tg("v2.1.0", "old123"), tg("v2.1.1", "new456")}, missing: minorItem{Major: 2, Minor: 1}, cfg: pre,
				typ: "missing_minor_version", checkAction: wantCreateTag("v2.1", "new456")},
			{name: "should use SourceSha when major tag exists without patches", refs: []vtRef{tg("v1", "major123")}, missing: minorItem{Major: 1, SourceSha: "major123"}, cfg: pre,
				typ: "missing_minor_version", version: "v1.0", expectedSha: "major123", checkAction: wantCreateTag("v1.0", "major123"),
				message: "Minor version tag v1.0 does not exist. It should point to v1 at major123"},
			{name: "should fallback to major tag lookup when no SourceSha and no patches", refs: []vtRef{tg("v1", "major123")}, missing: minorItem{Major: 1}, cfg: pre,
				typ: "missing_minor_version", version: "v1.0", expectedSha: "major123", checkAction: wantCreateTag("v1.0", "major123")},
		})
	})
	t.Run("Prerelease Filtering", func(t *testing.T) {
		refs := []vtRef{tg("v1.0.0", "stable123"), tg("v1.0.1", "prerel456")}
		vtRunIssue(t, name, []vtIssue{
			{name: "should use non-prerelease SHA in CreateIssue when ignore-preview-releases is true", prerelease: []string{"v1.0.1"}, refs: refs, missing: minorItem{Major: 1}, cfg: vtMinorCfg("tags", "error", true),
				typ: "missing_minor_version", expectedSha: "stable123"},
			{name: "should use prerelease SHA in CreateIssue when ignore-preview-releases is false", prerelease: []string{"v1.0.1"}, refs: refs, missing: minorItem{Major: 1}, cfg: vtMinorCfg("tags", "error", false),
				typ: "missing_minor_version", expectedSha: "prerel456"},
		})
	})
}

func TestMinorBranchMissing(t *testing.T) {
	const name = "minor_branch_missing"
	vtMeta(t, name, 23)
	br0 := vtMinorCfg("branches", "error", false)
	t.Run("Condition - AppliesWhen floating-versions-use is branches and check-minor-version is enabled", func(t *testing.T) {
		conds := vtMinorMissingConds("branches", "tags")
		conds[0].name = "should return missing minor when patch exists but minor branch doesn't"
		conds = append(conds, []vtCond{
			{name: "should not return minor when minor branch exists", refs: []vtRef{br("v1.0", "abc123"), tg("v1.0.0", "abc123")}, cfg: br0, want: 0},
			{name: "should not return missing minor when same version tag exists", refs: []vtRef{tg("v1.0", "stale456"), tg("v1.0.0", "patch123")}, cfg: br0, want: 0},
			{name: "should NOT return missing minor when only prerelease patches exist in that series and ignore-preview-releases is true", prerelease: []string{"v1.1.0"},
				refs: []vtRef{tg("v1.0.0", "stable123"), br("v1.0", "stable123"), br("v1", "stable123"), tg("v1.1.0", "preview456")}, cfg: vtMinorCfg("branches", "error", true), want: 0},
		}...)
		vtRunCond(t, name, conds)
		got := vtRule(t, name).Condition(vtState(nil, tg("v1.0.0", "abc123")), &br0)
		if len(got) != 1 || got[0].(minorItem) != (minorItem{Major: 1, Minor: 0}) {
			t.Fatalf("got %#v", got)
		}
	})
	t.Run("CreateIssue", func(t *testing.T) {
		pre := vtMinorCfg("branches", "error", true)
		vtRunIssue(t, name, []vtIssue{
			{name: "should create issue with correct details", refs: []vtRef{tg("v1.0.0", "abc123")}, missing: minorItem{Major: 1}, cfg: pre,
				typ: "missing_minor_version", version: "v1.0", checkAction: wantCreateBranch("v1.0", "abc123"),
				message: "Minor version branch v1.0 does not exist. It should point to v1.0.0 at abc123"},
			{name: "should create warning severity when check-minor-version is warning", refs: []vtRef{tg("v1.0.0", "abc123")}, missing: minorItem{Major: 1}, cfg: vtMinorCfg("branches", "warning", true),
				typ: "missing_minor_version", severity: "warning"},
			{name: "should configure CreateBranchAction with highest patch SHA in minor series", refs: []vtRef{tg("v2.1.0", "old123"), tg("v2.1.1", "new456")}, missing: minorItem{Major: 2, Minor: 1}, cfg: pre,
				typ: "missing_minor_version", checkAction: wantCreateBranch("v2.1", "new456")},
		})
	})
	t.Run("Prerelease Filtering", func(t *testing.T) {
		refs := []vtRef{tg("v1.0.0", "stable123"), tg("v1.0.1", "prerel456")}
		vtRunIssue(t, name, []vtIssue{
			{name: "should use non-prerelease SHA in CreateIssue when ignore-preview-releases is true", prerelease: []string{"v1.0.1"}, refs: refs, missing: minorItem{Major: 1}, cfg: vtMinorCfg("branches", "error", true),
				typ: "missing_minor_version", expectedSha: "stable123"},
			{name: "should use prerelease SHA in CreateIssue when ignore-preview-releases is false", prerelease: []string{"v1.0.1"}, refs: refs, missing: minorItem{Major: 1}, cfg: vtMinorCfg("branches", "error", false),
				typ: "missing_minor_version", expectedSha: "prerel456"},
		})
	})
}
