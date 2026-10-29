// Package version ports lib/VersionParser.ps1 and the ignore-versions matching of lib/GitHubApi.ps1.
// Only numeric major.minor.patch versions are supported; suffixes such as -beta have no meaning.
package version

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a normalised 3-part version.
type Version struct {
	Major, Minor, Build int
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Build) }

// Compare returns -1, 0 or 1.
func (v Version) Compare(o Version) int {
	switch {
	case v.Major != o.Major:
		return cmp(v.Major, o.Major)
	case v.Minor != o.Minor:
		return cmp(v.Minor, o.Minor)
	}
	return cmp(v.Build, o.Build)
}

func cmp(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// ErrEmptyVersion is returned for empty or whitespace-only input.
var ErrEmptyVersion = errors.New("version value cannot be null or empty")

// ConvertTo normalises "1", "1.2", "1.2.3" (and truncates 4+ parts) to a 3-part Version
// (ConvertTo-Version).
func ConvertTo(value string) (Version, error) {
	if strings.TrimSpace(value) == "" {
		return Version{}, ErrEmptyVersion
	}
	parts := strings.Split(value, ".")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	var n [3]int
	for i, p := range parts {
		x, err := strconv.ParseUint(p, 10, 31)
		if err != nil {
			return Version{}, fmt.Errorf("invalid version %q: %w", value, err)
		}
		n[i] = int(x)
	}
	return Version{n[0], n[1], n[2]}, nil
}

var validPattern = regexp.MustCompile(`^v\d{1,10}(\.\d{1,10}){0,2}(\.\*)?$`)

// IsValidPattern validates an ignore-versions pattern (Test-ValidVersionPattern).
func IsValidPattern(pattern string) bool {
	if len(pattern) > 50 {
		return false
	}
	return validPattern.MatchString(pattern)
}

// Matches reports whether version matches pattern. Wildcards (* any run, ? one char) are
// case-insensitive like PowerShell's -like. "v1.*" does not match "v1".
func Matches(version, pattern string) bool {
	var sb strings.Builder
	sb.WriteString("(?is)^")
	for _, r := range pattern {
		switch r {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		default:
			sb.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	sb.WriteString("$")
	re, err := regexp.Compile(sb.String())
	return err == nil && re.MatchString(version)
}

// ShouldBeIgnored is Test-VersionShouldBeIgnored.
func ShouldBeIgnored(version string, ignore []string) bool {
	for _, p := range ignore {
		if Matches(version, p) {
			return true
		}
	}
	return false
}

// IsIgnored is Test-VersionIgnored (exact or '*' wildcard match, case-sensitive).
func IsIgnored(version string, ignore []string) bool {
	for _, p := range ignore {
		if version == p {
			return true
		}
		if strings.Contains(p, "*") {
			re := "^" + strings.ReplaceAll(regexp.QuoteMeta(p), `\*`, ".*") + "$"
			if regexp.MustCompile(re).MatchString(version) {
				return true
			}
		}
	}
	return false
}
