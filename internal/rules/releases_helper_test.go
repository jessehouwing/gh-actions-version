package rules

import (
	"reflect"
	"testing"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

func rlRel(tag string, id int64, draft, pre, immutable bool) *state.ReleaseInfo {
	return &state.ReleaseInfo{TagName: tag, ID: id, IsDraft: draft, IsPrerelease: pre, IsImmutable: immutable}
}

func rlState(rs ...*state.ReleaseInfo) *state.State {
	st := state.New()
	st.Releases = rs
	return st
}

func TestShouldBeLatestRelease(t *testing.T) {
	ignored := rlRel("v2.0.0", 200, false, false, false)
	ignored.IsIgnored = true
	cases := []struct {
		name    string
		version string
		rels    []*state.ReleaseInfo
		info    *state.ReleaseInfo
		want    bool
	}{
		{"should return false for non-patch versions (major only)", "v1", nil, nil, false},
		{"should return false for non-patch versions (minor only)", "v1.0", nil, nil, false},
		{"should return false for 'latest' version", "latest", nil, nil, false},
		{"should return false for versions with suffixes", "v1.0.0-beta", nil, nil, false},
		{"should return true when no releases exist", "v1.0.0", nil, nil, true},
		{"should return true when only draft releases exist", "v1.0.0", []*state.ReleaseInfo{rlRel("v2.0.0", 200, true, false, false)}, nil, true},
		{"should return true when only prerelease releases exist", "v1.0.0", []*state.ReleaseInfo{rlRel("v2.0.0", 200, false, true, false)}, nil, true},
		{"should return true when only ignored releases exist", "v1.0.0", []*state.ReleaseInfo{ignored}, nil, true},
		{"should return true when target is higher major version", "v2.0.0", []*state.ReleaseInfo{rlRel("v1.0.0", 100, false, false, false)}, nil, true},
		{"should return true when target is higher minor version", "v1.1.0", []*state.ReleaseInfo{rlRel("v1.0.0", 100, false, false, false)}, nil, true},
		{"should return true when target is higher patch version", "v1.0.1", []*state.ReleaseInfo{rlRel("v1.0.0", 100, false, false, false)}, nil, true},
		{"should handle numeric sorting correctly (v10 > v9)", "v10.0.0", []*state.ReleaseInfo{rlRel("v9.0.0", 900, false, false, false)}, nil, true},
		{"should return false when target is lower major version", "v1.0.0", []*state.ReleaseInfo{rlRel("v2.0.0", 200, false, false, false)}, nil, false},
		{"should return false when target is lower minor version", "v1.1.0", []*state.ReleaseInfo{rlRel("v1.2.0", 120, false, false, false)}, nil, false},
		{"should return false when target is lower patch version", "v1.0.1", []*state.ReleaseInfo{rlRel("v1.0.2", 102, false, false, false)}, nil, false},
		{"should return false when target equals existing version", "v1.0.0", []*state.ReleaseInfo{rlRel("v1.0.0", 100, false, false, false)}, nil, false},
		{"should return false when ReleaseInfo indicates prerelease", "v2.0.0", nil, rlRel("v2.0.0", 200, false, true, false), false},
		{"should return true when ReleaseInfo indicates non-prerelease and version is highest", "v2.0.0",
			[]*state.ReleaseInfo{rlRel("v1.0.0", 100, false, false, false)}, rlRel("v2.0.0", 200, true, false, false), true},
		{"should correctly identify highest among multiple releases (v2.1.0)", "v2.1.0", []*state.ReleaseInfo{
			rlRel("v1.0.0", 100, false, false, false), rlRel("v1.5.0", 150, false, false, false), rlRel("v2.0.0", 200, false, false, false)}, nil, true},
		{"should correctly identify highest among multiple releases (v1.6.0)", "v1.6.0", []*state.ReleaseInfo{
			rlRel("v1.0.0", 100, false, false, false), rlRel("v1.5.0", 150, false, false, false), rlRel("v2.0.0", 200, false, false, false)}, nil, false},
		{"should ignore prerelease releases when finding highest", "v1.1.0", []*state.ReleaseInfo{
			rlRel("v1.0.0", 100, false, false, false), rlRel("v2.0.0", 200, false, true, false)}, nil, true},
		{"should handle backporting older version (should NOT become latest)", "v1.0.1", []*state.ReleaseInfo{rlRel("v2.0.0", 200, false, false, false)}, nil, false},
		{"should handle new major version (should become latest)", "v2.0.0", []*state.ReleaseInfo{rlRel("v1.5.3", 153, false, false, false)}, nil, true},
		{"should handle prerelease for next version (should NOT become latest)", "v2.0.0", []*state.ReleaseInfo{rlRel("v1.0.0", 100, false, false, false)}, rlRel("v2.0.0", 200, false, true, false), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldBeLatestRelease(rlState(c.rels...), c.version, c.info); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestDuplicateReleaseIDs(t *testing.T) {
	cases := []struct {
		name string
		rels []*state.ReleaseInfo
		want []int64
	}{
		{"should return empty array when no releases", nil, nil},
		{"should return empty array when no duplicates exist", []*state.ReleaseInfo{rlRel("v1.0.0", 100, false, false, false), rlRel("v2.0.0", 200, false, false, false)}, nil},
		{"should return duplicate IDs keeping published over draft", []*state.ReleaseInfo{rlRel("v1.0.0", 100, false, false, false), rlRel("v1.0.0", 101, true, false, false)}, []int64{101}},
		{"should ignore non-patch versions", []*state.ReleaseInfo{rlRel("v1", 100, false, false, false), rlRel("v1", 101, true, false, false)}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DuplicateReleaseIDs(rlState(c.rels...))
			if len(got) != len(c.want) || (len(got) > 0 && !reflect.DeepEqual(got, c.want)) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestDuplicateDraftReleases(t *testing.T) {
	t.Run("should return empty array when no releases", func(t *testing.T) {
		if got := DuplicateDraftReleases(rlState()); len(got) != 0 {
			t.Errorf("got %d", len(got))
		}
	})
	t.Run("should only return draft duplicates (not published)", func(t *testing.T) {
		got := DuplicateDraftReleases(rlState(rlRel("v1.0.0", 100, false, false, false), rlRel("v1.0.0", 101, true, false, false)))
		if len(got) != 1 || got[0].ID != 101 || !got[0].IsDraft {
			t.Errorf("got %v", got)
		}
	})
	t.Run("should not return published duplicates even if they would be deleted", func(t *testing.T) {
		got := DuplicateDraftReleases(rlState(rlRel("v1.0.0", 100, false, false, true), rlRel("v1.0.0", 101, false, false, false)))
		if len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
}
