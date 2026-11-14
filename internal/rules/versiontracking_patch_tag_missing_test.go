package rules

import (
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func TestPatchTagMissing(t *testing.T) {
	const name = "patch_tag_missing"
	vtMeta(t, name, 25)
	cfg := func(use, rel string) state.Config {
		return state.Config{FloatingVersionsUse: use, CheckReleases: rel}
	}
	t.Run("Condition - Only applies when check-releases is 'none'", func(t *testing.T) {
		vtRunCond(t, name, []vtCond{
			{name: "should return empty when check-releases is 'error'", refs: []vtRef{tg("v1", "abc123")}, cfg: cfg("tags", "error"), want: 0},
			{name: "should return empty when check-releases is 'warning'", refs: []vtRef{tg("v1", "abc123")}, cfg: cfg("tags", "warning"), want: 0},
			{name: "should apply when check-releases is 'none'", refs: []vtRef{tg("v1", "abc123")}, cfg: cfg("tags", "none"), want: 1},
		})
	})
	t.Run("Condition - Major version tags without patches", func(t *testing.T) {
		vtRunCond(t, name, []vtCond{
			{name: "should detect missing patch for major tag", refs: []vtRef{tg("v1", "abc123")}, cfg: cfg("tags", "none"), want: 1},
			{name: "should pass when patch exists for major version", refs: []vtRef{tg("v1", "abc123"), tg("v1.0.0", "abc123")}, cfg: cfg("tags", "none"), want: 0},
			{name: "should handle major branch when floating-versions-use is branches", refs: []vtRef{br("v1", "abc123")}, cfg: cfg("branches", "none"), want: 1},
		})
		for _, c := range []struct {
			refs []vtRef
			cfg  state.Config
			v, e string
		}{
			{[]vtRef{tg("v1", "abc123")}, cfg("tags", "none"), "v1", "v1.0.0"},
			{[]vtRef{br("v1", "abc123")}, cfg("branches", "none"), "v1", "v1.0.0"},
			{[]vtRef{tg("v1.0", "abc123")}, cfg("tags", "none"), "v1.0", "v1.0.0"},
			{[]vtRef{tg("latest", "abc123")}, cfg("tags", "none"), "latest", "v1.0.0"},
		} {
			c := c
			got := vtRule(t, name).Condition(vtState(nil, c.refs...), &c.cfg)
			it := got[0].(patchItem)
			if it.Floating.Version != c.v || it.Expected != c.e {
				t.Fatalf("got %s/%s, want %s/%s", it.Floating.Version, it.Expected, c.v, c.e)
			}
		}
	})
	t.Run("Condition - Minor version tags without patches", func(t *testing.T) {
		vtRunCond(t, name, []vtCond{
			{name: "should detect missing patch for minor tag", refs: []vtRef{tg("v1.0", "abc123")}, cfg: cfg("tags", "none"), want: 1},
			{name: "should pass when patch exists for minor version", refs: []vtRef{tg("v1.0", "abc123"), tg("v1.0.0", "abc123")}, cfg: cfg("tags", "none"), want: 0},
			{name: "should handle different minor versions separately", refs: []vtRef{tg("v1.0", "abc123"), tg("v1.1", "def456")}, cfg: cfg("tags", "none"), want: 2},
		})
	})
	t.Run("Condition - Latest tag without patches", func(t *testing.T) {
		vtRunCond(t, name, []vtCond{
			{name: "should detect missing patch for latest tag", refs: []vtRef{tg("latest", "abc123")}, cfg: cfg("tags", "none"), want: 1},
			{name: "should pass when at least one patch exists for latest", refs: []vtRef{tg("latest", "abc123"), tg("v2.5.7", "abc123")}, cfg: cfg("tags", "none"), want: 0},
		})
	})
	t.Run("Condition - Ignored versions", func(t *testing.T) {
		vtRunCond(t, name, []vtCond{
			{name: "should skip ignored floating versions", refs: []vtRef{ign(tg("v1", "abc123"))}, cfg: cfg("tags", "none"), want: 0},
			{name: "should skip ignored patch versions when checking if patches exist", refs: []vtRef{tg("v1", "abc123"), ign(tg("v1.0.0", "abc123"))}, cfg: cfg("tags", "none"), want: 1},
		})
	})
	t.Run("Condition - Patches in branches", func(t *testing.T) {
		vtRunCond(t, name, []vtCond{
			{name: "should find patches that exist as branches", refs: []vtRef{tg("v1", "abc123"), br("v1.0.0", "abc123")}, cfg: cfg("tags", "none"), want: 0},
		})
	})
	t.Run("CreateIssue", func(t *testing.T) {
		c := cfg("tags", "none")
		vtRunIssue(t, name, []vtIssue{
			{name: "should create issue with correct details for major version", missing: patchItem{Floating: state.NewVersionRef("v1", "refs/tags/v1", "abc123", state.TypeTag), Expected: "v1.0.0"}, cfg: c,
				typ: "missing_patch_version", version: "v1.0.0", checkAction: wantCreateTag("v1.0.0", "abc123"),
				message: "Floating version v1 exists but no corresponding patch version found. Expected: v1.0.0"},
			{name: "should configure CreateTagAction with floating version SHA", missing: patchItem{Floating: state.NewVersionRef("v2", "refs/tags/v2", "def456", state.TypeTag), Expected: "v2.0.0"}, cfg: c,
				typ: "missing_patch_version", checkAction: wantCreateTag("v2.0.0", "def456")},
		})
	})
}
