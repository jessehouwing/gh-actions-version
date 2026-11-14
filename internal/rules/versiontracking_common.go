package rules

import (
	"fmt"
	"sort"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// Shared helpers for the version_tracking rules.

type majorItem struct{ Major int }

type minorItem struct {
	Major, Minor int
	SourceSha    string // set when the minor is derived from a major ref without patches
}

type patchItem struct {
	Floating *state.VersionRef
	Expected string
}

// floatingIsBranches mirrors `floating-versions-use -eq 'branches'`; anything else behaves like tags.
func floatingIsBranches(cfg *state.Config) bool { return cfg.FloatingVersionsUse == "branches" }

func minorSeverity(cfg *state.Config) string {
	if cfg.CheckMinorVersion == "warning" {
		return state.SeverityWarning
	}
	return state.SeverityError
}

// eligiblePatches returns non-ignored patch refs, excluding prereleases when configured.
func eligiblePatches(st *state.State, cfg *state.Config) []*state.VersionRef {
	var out []*state.VersionRef
	for _, v := range st.AllRefs() {
		if v.IsPatch && !v.IsIgnored && (!cfg.IgnorePreviewReleases || !IsPrerelease(st, v)) {
			out = append(out, v)
		}
	}
	return out
}

func refExists(st *state.State, version string) bool {
	for _, v := range st.AllRefs() {
		if v.Version == version && !v.IsIgnored {
			return true
		}
	}
	return false
}

func missingMajors(st *state.State, cfg *state.Config) []any {
	seen := map[int]bool{}
	var majors []int
	for _, p := range eligiblePatches(st, cfg) {
		if !seen[p.Major] {
			seen[p.Major] = true
			majors = append(majors, p.Major)
		}
	}
	sort.Ints(majors)
	var out []any
	for _, m := range majors {
		if !refExists(st, fmt.Sprintf("v%d", m)) {
			out = append(out, majorItem{Major: m})
		}
	}
	return out
}

func missingMinorsFromPatches(st *state.State, cfg *state.Config) []minorItem {
	seen := map[[2]int]bool{}
	var keys [][2]int
	for _, p := range eligiblePatches(st, cfg) {
		k := [2]int{p.Major, p.Minor}
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	var out []minorItem
	for _, k := range keys {
		if !refExists(st, fmt.Sprintf("v%d.%d", k[0], k[1])) {
			out = append(out, minorItem{Major: k[0], Minor: k[1]})
		}
	}
	return out
}

func shaOf(v *state.VersionRef) string {
	if v == nil {
		return ""
	}
	return v.Sha
}

func versionOf(v *state.VersionRef) string {
	if v == nil {
		return ""
	}
	return v.Version
}

func majorTracksCheck(item any, st *state.State, cfg *state.Config) bool {
	v := item.(*state.VersionRef)
	hp := HighestPatchForMajor(st, v.Major, cfg.IgnorePreviewReleases)
	return hp == nil || v.Sha == hp.Sha
}

func minorTracksCheck(item any, st *state.State, cfg *state.Config) bool {
	v := item.(*state.VersionRef)
	hp := HighestPatchForMinor(st, v.Major, v.Minor, cfg.IgnorePreviewReleases)
	return hp == nil || v.Sha == hp.Sha
}

func majorTracksIssue(item any, st *state.State, cfg *state.Config, branch bool) *state.Issue {
	v := item.(*state.VersionRef)
	hp := HighestPatchForMajor(st, v.Major, cfg.IgnorePreviewReleases)
	return trackIssue("incorrect_version", state.SeverityError, v, hp, branch)
}

func minorTracksIssue(item any, st *state.State, cfg *state.Config, branch bool) *state.Issue {
	v := item.(*state.VersionRef)
	hp := HighestPatchForMinor(st, v.Major, v.Minor, cfg.IgnorePreviewReleases)
	return trackIssue("incorrect_minor_version", minorSeverity(cfg), v, hp, branch)
}

func trackIssue(typ, severity string, v, hp *state.VersionRef, branch bool) *state.Issue {
	issue := state.NewIssue(typ, severity,
		fmt.Sprintf("%s points to %s but should point to %s at %s", v.Version, v.Sha, versionOf(hp), shaOf(hp)))
	issue.Version = v.Version
	issue.CurrentSha = v.Sha
	issue.ExpectedSha = shaOf(hp)
	if branch {
		issue.Action = actions.NewUpdateBranchAction(v.Version, shaOf(hp), true)
	} else {
		issue.Action = actions.NewUpdateTagAction(v.Version, shaOf(hp), true)
	}
	return issue
}

func majorMissingIssue(item any, st *state.State, cfg *state.Config, branch bool) *state.Issue {
	major := item.(majorItem).Major
	version := fmt.Sprintf("v%d", major)
	hp := HighestPatchForMajor(st, major, cfg.IgnorePreviewReleases)
	prefix := ""
	if branch {
		prefix = "Major version branch "
	}
	issue := state.NewIssue("missing_major_version", state.SeverityError,
		fmt.Sprintf("%s%s does not exist. It should point to %s at %s", prefix, version, versionOf(hp), shaOf(hp)))
	issue.Version = version
	issue.ExpectedSha = shaOf(hp)
	if branch {
		issue.Action = actions.NewCreateBranchAction(version, shaOf(hp))
	} else {
		issue.Action = actions.NewCreateTagAction(version, shaOf(hp))
	}
	return issue
}
