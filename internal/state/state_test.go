package state

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

type fakeAction struct{ prio int }

func (fakeAction) Description() string                                { return "fake" }
func (fakeAction) Version() string                                    { return "v1" }
func (a fakeAction) Priority() int                                    { return a.prio }
func (fakeAction) ManualCommandsLanguage() string                     { return "bash" }
func (fakeAction) Execute(_ context.Context, _ *State, _ Client) bool { return true }
func (fakeAction) ManualCommands(*State) []string                     { return []string{"git push"} }

func issue(sev, status string) *Issue {
	i := NewIssue("t", sev, "m")
	i.Status = status
	return i
}

// Ported from tests/unit/StateModel.Tests.ps1; subtest names are the PowerShell It names.
func TestVersionRef(t *testing.T) {
	cases := []struct {
		name, version       string
		patch, minor, major bool
		maj, min, pat       int
	}{
		{"parses patch version (v1.0.0) correctly", "v1.0.0", true, false, false, 1, 0, 0},
		{"parses minor version (v1.2) correctly", "v1.2", false, true, false, 1, 2, 0},
		{"parses major version (v2) correctly", "v2", false, false, true, 2, 0, 0},
		{"parses high version numbers correctly", "v123.456.789", true, false, false, 123, 456, 789},
		{"handles version without 'v' prefix", "1.2.3", true, false, false, 1, 2, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := NewVersionRef(c.version, "refs/tags/"+c.version, "abc", TypeTag)
			if v.IsPatch != c.patch || v.IsMinor != c.minor || v.IsMajor != c.major || v.Major != c.maj || v.Minor != c.min || v.Patch != c.pat {
				t.Errorf("unexpected parse: %+v", v)
			}
		})
	}
	for _, name := range []string{"latest", "2-proxy", "v1-preview", "v2-proxy", "v1.0.0-beta", "v1.2.3.4"} {
		t.Run("handles non-semver version "+name, func(t *testing.T) {
			v := NewVersionRef(name, "refs/tags/"+name, "abc", TypeTag)
			if v.IsPatch || v.IsMinor || v.IsMajor {
				t.Errorf("%q must not be a version: %+v", name, v)
			}
		})
	}
	t.Run("handles non-semver versions like 'latest'", func(t *testing.T) {
		v := NewVersionRef("latest", "refs/tags/latest", "abc", TypeTag)
		if v.IsPatch || v.IsMinor || v.IsMajor {
			t.Errorf("unexpected: %+v", v)
		}
	})
	t.Run("handles version-like strings with non-numeric suffixes (e.g., '2-proxy', 'v1-preview')", func(t *testing.T) {
		for _, n := range []string{"2-proxy", "v1-preview"} {
			if v := NewVersionRef(n, "refs/tags/"+n, "abc", TypeTag); v.IsMajor {
				t.Errorf("%q parsed as major", n)
			}
		}
	})
	t.Run("handles v-prefixed version with non-numeric suffix (e.g., 'v2-proxy')", func(t *testing.T) {
		if NewVersionRef("v2-proxy", "refs/tags/v2-proxy", "abc", TypeTag).IsMajor {
			t.Error("parsed as major")
		}
	})
	t.Run("stores all constructor parameters correctly", func(t *testing.T) {
		v := NewVersionRef("v1.0.0", "refs/tags/v1.0.0", "abc123", TypeTag)
		if v.Version != "v1.0.0" || v.Ref != "refs/tags/v1.0.0" || v.Sha != "abc123" || v.Type != TypeTag {
			t.Errorf("%+v", v)
		}
	})
	t.Run("supports branch type", func(t *testing.T) {
		if NewVersionRef("v1", "refs/heads/v1", "abc", TypeBranch).Type != TypeBranch {
			t.Error("type lost")
		}
	})
	t.Run("formats output correctly", func(t *testing.T) {
		if got := NewVersionRef("v1.0.0", "refs/tags/v1.0.0", "abc", TypeTag).String(); got != "v1.0.0 -> abc (tag)" {
			t.Error(got)
		}
	})
}

func TestReleaseInfo(t *testing.T) {
	str := func(r ReleaseInfo) string { return r.String() }
	t.Run("shows draft status", func(t *testing.T) {
		if got := str(ReleaseInfo{TagName: "v1", IsDraft: true, IsImmutable: true}); got != "v1 [draft]" {
			t.Error(got)
		}
	})
	t.Run("shows prerelease status", func(t *testing.T) {
		if got := str(ReleaseInfo{TagName: "v1", IsPrerelease: true, IsImmutable: true}); got != "v1 [prerelease]" {
			t.Error(got)
		}
	})
	t.Run("shows mutable status when not immutable", func(t *testing.T) {
		if got := str(ReleaseInfo{TagName: "v1"}); got != "v1 [mutable]" {
			t.Error(got)
		}
	})
	t.Run("shows latest status", func(t *testing.T) {
		if got := str(ReleaseInfo{TagName: "v1", IsImmutable: true, IsLatest: true}); got != "v1 [latest]" {
			t.Error(got)
		}
	})
	t.Run("shows multiple statuses", func(t *testing.T) {
		got := str(ReleaseInfo{TagName: "v1", IsDraft: true, IsPrerelease: true})
		for _, w := range []string{"draft", "prerelease", "mutable"} {
			if !strings.Contains(got, w) {
				t.Errorf("%q missing %q", got, w)
			}
		}
	})
	t.Run("shows no status brackets when immutable and not draft/prerelease/latest", func(t *testing.T) {
		if got := str(ReleaseInfo{TagName: "v1", IsImmutable: true}); got != "v1" {
			t.Error(got)
		}
	})
}

func TestIssue(t *testing.T) {
	t.Run("creates issue with all parameters", func(t *testing.T) {
		i := NewIssue("patch_tag_missing", SeverityError, "msg")
		if i.Finding != "patch_tag_missing" || i.Severity != SeverityError || i.Message != "msg" {
			t.Errorf("%+v", i)
		}
	})
	t.Run("defaults IsAutoFixable to false", func(t *testing.T) {
		if NewIssue("t", SeverityError, "m").IsAutoFixable() {
			t.Error("fixable")
		}
	})
	t.Run("defaults Status to pending", func(t *testing.T) {
		if NewIssue("t", SeverityError, "m").Status != StatusPending {
			t.Error("not pending")
		}
	})
	t.Run("sets action and marks as auto-fixable", func(t *testing.T) {
		i := NewIssue("t", SeverityError, "m")
		i.Action = fakeAction{prio: 10}
		if !i.IsAutoFixable() || i.Priority() != 10 {
			t.Error("not fixable / wrong priority")
		}
	})
	t.Run("sets auto-fixable to false when action is null", func(t *testing.T) {
		i := NewIssue("t", SeverityError, "m")
		i.Action = fakeAction{}
		i.Action = nil
		if i.IsAutoFixable() || i.Priority() != 100 {
			t.Error("still fixable")
		}
	})
	t.Run("formats error correctly", func(t *testing.T) {
		if got := NewIssue("t", SeverityError, "boom").String(); got != "ERROR: boom" {
			t.Error(got)
		}
	})
	t.Run("formats warning correctly", func(t *testing.T) {
		if got := NewIssue("t", SeverityWarning, "careful").String(); got != "WARNING: careful" {
			t.Error(got)
		}
	})
	for _, to := range []string{StatusFixed, StatusFailed, StatusUnfixable, StatusManualFixRequired} {
		t.Run("can transition from pending to "+to, func(t *testing.T) {
			i := NewIssue("t", SeverityError, "m")
			i.Status = to
			if i.Status != to {
				t.Error("status not set")
			}
		})
	}
}

func TestState(t *testing.T) {
	t.Run("initializes with empty collections", func(t *testing.T) {
		s := New()
		if len(s.Tags)+len(s.Branches)+len(s.Releases)+len(s.Issues) != 0 || s.Marketplace == nil {
			t.Errorf("%+v", s)
		}
	})
	t.Run("adds issue to collection", func(t *testing.T) {
		s := New()
		s.AddIssue(issue(SeverityError, StatusPending))
		if len(s.Issues) != 1 {
			t.Error("not added")
		}
	})
	t.Run("can add multiple issues", func(t *testing.T) {
		s := New()
		s.AddIssue(issue(SeverityError, StatusPending))
		s.AddIssue(issue(SeverityWarning, StatusPending))
		if len(s.Issues) != 2 {
			t.Error("not added")
		}
	})
	t.Run("returns only error issues", func(t *testing.T) {
		s := New()
		s.Issues = []*Issue{issue(SeverityError, ""), issue(SeverityWarning, ""), issue(SeverityError, "")}
		if len(s.ErrorIssues()) != 2 {
			t.Error("wrong count")
		}
	})
	t.Run("returns only warning issues", func(t *testing.T) {
		s := New()
		s.Issues = []*Issue{issue(SeverityError, ""), issue(SeverityWarning, "")}
		if len(s.WarningIssues()) != 1 {
			t.Error("wrong count")
		}
	})
	for _, c := range []struct {
		name   string
		status string
		count  func(*State) int
	}{
		{"counts issues with fixed status", StatusFixed, (*State).FixedCount},
		{"counts issues with failed status", StatusFailed, (*State).FailedCount},
		{"counts issues with unfixable status", StatusUnfixable, (*State).UnfixableCount},
		{"counts issues with manual_fix_required status", StatusManualFixRequired, (*State).ManualFixCount},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := New()
			s.Issues = []*Issue{issue(SeverityError, c.status), issue(SeverityError, StatusPending), issue(SeverityError, c.status)}
			if c.count(s) != 2 {
				t.Errorf("got %d", c.count(s))
			}
		})
	}
	rc := func(name string, want int, is ...*Issue) {
		t.Run(name, func(t *testing.T) {
			s := New()
			s.Issues = is
			if got := s.ReturnCode(); got != want {
				t.Errorf("got %d, want %d", got, want)
			}
		})
	}
	rc("returns 0 when all issues are fixed", 0, issue(SeverityError, StatusFixed))
	rc("returns 0 when no issues exist", 0)
	rc("returns 1 when there are failed issues", 1, issue(SeverityError, StatusFailed))
	rc("returns 1 when there are unfixable issues", 1, issue(SeverityError, StatusUnfixable))
	rc("returns 1 when there are manual_fix_required issues", 1, issue(SeverityError, StatusManualFixRequired))
	rc("returns 0 when pending issues exist but nothing failed", 0, issue(SeverityError, StatusPending))
	rc("returns 0 when warning-severity issues are manual_fix_required", 0, issue(SeverityWarning, StatusManualFixRequired))
	rc("returns 0 when warning-severity issues are unfixable", 0, issue(SeverityWarning, StatusUnfixable))
	rc("returns 0 when warning-severity issues are failed", 0, issue(SeverityWarning, StatusFailed))
	rc("returns 1 when there are mixed error and warning issues with error issues unresolved", 1,
		issue(SeverityWarning, StatusFailed), issue(SeverityError, StatusUnfixable))
	rc("returns 0 when there are mixed error and warning issues with only warning issues unresolved", 0,
		issue(SeverityWarning, StatusFailed), issue(SeverityError, StatusFixed))

	refs := func() *State {
		s := New()
		s.Tags = []*VersionRef{NewVersionRef("v1.0.0", "refs/tags/v1.0.0", "a", TypeTag), NewVersionRef("v1.0", "refs/tags/v1.0", "a", TypeTag), NewVersionRef("v1", "refs/tags/v1", "a", TypeTag)}
		s.Branches = []*VersionRef{NewVersionRef("v2.0.0", "refs/heads/v2.0.0", "b", TypeBranch), NewVersionRef("v2.0", "refs/heads/v2.0", "b", TypeBranch), NewVersionRef("v2", "refs/heads/v2", "b", TypeBranch), NewVersionRef("v1", "refs/heads/v1", "c", TypeBranch)}
		return s
	}
	t.Run("returns patch versions from both tags and branches", func(t *testing.T) {
		if n := len(refs().PatchVersions()); n != 2 {
			t.Error(n)
		}
	})
	t.Run("returns minor versions from both tags and branches", func(t *testing.T) {
		if n := len(refs().MinorVersions()); n != 2 {
			t.Error(n)
		}
	})
	t.Run("returns major versions from both tags and branches", func(t *testing.T) {
		if n := len(refs().MajorVersions()); n != 3 {
			t.Error(n)
		}
	})
	t.Run("finds version in tags when type is tag", func(t *testing.T) {
		if v := refs().FindVersion("v1", TypeTag); v == nil || v.Sha != "a" {
			t.Error(v)
		}
	})
	t.Run("finds version in branches when type is branch", func(t *testing.T) {
		if v := refs().FindVersion("v1", TypeBranch); v == nil || v.Sha != "c" {
			t.Error(v)
		}
	})
	t.Run("searches tags first when type is not specified", func(t *testing.T) {
		if v := refs().FindVersion("v1", ""); v == nil || v.Type != TypeTag {
			t.Error(v)
		}
	})
	t.Run("falls back to branches when not found in tags", func(t *testing.T) {
		if v := refs().FindVersion("v2", ""); v == nil || v.Type != TypeBranch {
			t.Error(v)
		}
	})
	t.Run("returns null when version not found", func(t *testing.T) {
		if refs().FindVersion("v9", "") != nil {
			t.Error("found")
		}
	})
	t.Run("finds release by tag name", func(t *testing.T) {
		s := New()
		s.Releases = []*ReleaseInfo{{TagName: "v1.0.0", ID: 7}}
		if r := s.FindRelease("v1.0.0"); r == nil || r.ID != 7 {
			t.Error(r)
		}
	})
	t.Run("returns null when release not found", func(t *testing.T) {
		if New().FindRelease("v1.0.0") != nil {
			t.Error("found")
		}
	})
}

func TestMarketplaceMetadata(t *testing.T) {
	full := MarketplaceMetadata{ActionFileExists: true, HasName: true, HasDescription: true, HasBrandingIcon: true, HasBrandingColor: true, ReadmeExists: true}
	t.Run("should return true when all requirements are met", func(t *testing.T) {
		if !full.IsValid() {
			t.Error("invalid")
		}
	})
	t.Run("should return false when action file is missing", func(t *testing.T) {
		m := full
		m.ActionFileExists = false
		if m.IsValid() {
			t.Error("valid")
		}
	})
	t.Run("should return false when name is missing", func(t *testing.T) {
		m := full
		m.HasName = false
		if m.IsValid() {
			t.Error("valid")
		}
	})
	t.Run("should return false when README is missing", func(t *testing.T) {
		m := full
		m.ReadmeExists = false
		if m.IsValid() {
			t.Error("valid")
		}
	})
	t.Run("should return empty array when all requirements are met", func(t *testing.T) {
		if got := full.MissingRequirements(); len(got) != 0 {
			t.Error(got)
		}
	})
	t.Run("should return all missing requirements", func(t *testing.T) {
		got := (&MarketplaceMetadata{}).MissingRequirements()
		if !reflect.DeepEqual(len(got), 5) {
			t.Error(got)
		}
	})
}
