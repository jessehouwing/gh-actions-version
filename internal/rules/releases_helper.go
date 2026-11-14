package rules

import (
	"regexp"
	"sort"
	"strconv"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

var (
	patchTagStrictRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
	patchTagPrefixRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)`)
	majorTagRe       = regexp.MustCompile(`^v(\d+)$`)
	minorTagRe       = regexp.MustCompile(`^v(\d+)\.(\d+)$`)
)

func parsePatchTag(tag string) (major, minor, patch int, ok bool) {
	m := patchTagStrictRe.FindStringSubmatch(tag)
	if m == nil {
		return 0, 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	patch, _ = strconv.Atoi(m[3])
	return major, minor, patch, true
}

func checkEnabled(v string) bool { return v == "error" || v == "warning" }

// ShouldBeLatestRelease reports whether version should become the "latest" release (port of Test-ShouldBeLatestRelease).
// info is optional and used for prerelease detection.
func ShouldBeLatestRelease(st *state.State, version string, info *state.ReleaseInfo) bool {
	tMaj, tMin, tPat, ok := parsePatchTag(version)
	if !ok {
		return false
	}
	if info != nil && info.IsPrerelease {
		return false
	}

	found := false
	var hMaj, hMin, hPat int
	for _, r := range st.Releases {
		if r.IsDraft || r.IsPrerelease || r.IsIgnored {
			continue
		}
		maj, min, pat, ok := parsePatchTag(r.TagName)
		if !ok {
			continue
		}
		if !found || maj > hMaj || (maj == hMaj && min > hMin) || (maj == hMaj && min == hMin && pat > hPat) {
			found, hMaj, hMin, hPat = true, maj, min, pat
		}
	}
	if !found {
		return true
	}
	return tMaj > hMaj || (tMaj == hMaj && tMin > hMin) || (tMaj == hMaj && tMin == hMin && tPat > hPat)
}

// duplicateReleases returns the releases that duplicate another release of the same patch tag
// (all but the best candidate: published over draft, immutable over mutable, lowest ID first).
func duplicateReleases(st *state.State) []*state.ReleaseInfo {
	var tags []string
	groups := map[string][]*state.ReleaseInfo{}
	for _, r := range st.Releases {
		if r.IsIgnored || !patchTagStrictRe.MatchString(r.TagName) {
			continue
		}
		if _, seen := groups[r.TagName]; !seen {
			tags = append(tags, r.TagName)
		}
		groups[r.TagName] = append(groups[r.TagName], r)
	}
	var dups []*state.ReleaseInfo
	for _, tag := range tags {
		g := groups[tag]
		if len(g) < 2 {
			continue
		}
		sorted := append([]*state.ReleaseInfo(nil), g...)
		sort.SliceStable(sorted, func(i, j int) bool {
			a, b := sorted[i], sorted[j]
			if a.IsDraft != b.IsDraft {
				return !a.IsDraft
			}
			if a.IsImmutable != b.IsImmutable {
				return a.IsImmutable
			}
			return a.ID < b.ID
		})
		dups = append(dups, sorted[1:]...)
	}
	return dups
}

// DuplicateReleaseIDs returns the IDs of duplicate releases (port of Get-DuplicateReleaseId).
func DuplicateReleaseIDs(st *state.State) []int64 {
	var ids []int64
	for _, r := range duplicateReleases(st) {
		ids = append(ids, r.ID)
	}
	return ids
}

// DuplicateDraftReleases returns the duplicate releases that are drafts (port of Get-DuplicateDraftRelease).
func DuplicateDraftReleases(st *state.State) []*state.ReleaseInfo {
	var out []*state.ReleaseInfo
	for _, r := range duplicateReleases(st) {
		if r.IsDraft {
			out = append(out, r)
		}
	}
	return out
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func releaseSeverity(v string) string {
	if v == "warning" {
		return state.SeverityWarning
	}
	return state.SeverityError
}

// mostSevere returns error if either setting is "error", else warning.
func mostSevere(a, b string) string {
	if a == "error" || b == "error" {
		return state.SeverityError
	}
	return state.SeverityWarning
}

// makeLatestSetter is implemented by release actions that can control the "latest" flag.
// The shared action contract doesn't expose MakeLatest yet, so rules set it when the action supports it.
type makeLatestSetter interface{ SetMakeLatest(v *bool) }

func applyMakeLatest(a any, v *bool) {
	if s, ok := a.(makeLatestSetter); ok {
		s.SetMakeLatest(v)
	}
}

// makeLatestUnlessHighest returns nil (let GitHub decide) when version should become latest,
// otherwise a pointer to false so an existing correct latest release is not overwritten.
func makeLatestUnlessHighest(st *state.State, version string, info *state.ReleaseInfo) *bool {
	if ShouldBeLatestRelease(st, version, info) {
		return nil
	}
	f := false
	return &f
}
