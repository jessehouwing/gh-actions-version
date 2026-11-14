package options

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/output"
	"github.com/jessehouwing/gh-actions-version/internal/remediation"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func TestConvertToIgnoreVersionsList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"Should return empty array for empty string", "", nil},
		{"Should return empty array for whitespace-only string", "   ", nil},
		{"Should parse single version", "v1.0.0", []string{"v1.0.0"}},
		{"Should parse comma-separated versions", "v1.0.0, v2.0.0, v3.0.0", []string{"v1.0.0", "v2.0.0", "v3.0.0"}},
		{"Should trim whitespace around versions", "  v1.0.0  ,  v2.0.0  ", []string{"v1.0.0", "v2.0.0"}},
		{"Should handle commas without spaces", "v1.0.0,v2.0.0,v3.0.0", []string{"v1.0.0", "v2.0.0", "v3.0.0"}},
		{"Should parse newline-separated versions", "v1.0.0\nv2.0.0\nv3.0.0", []string{"v1.0.0", "v2.0.0", "v3.0.0"}},
		{"Should handle Windows-style line endings (CRLF)", "v1.0.0\r\nv2.0.0\r\nv3.0.0", []string{"v1.0.0", "v2.0.0", "v3.0.0"}},
		{"Should handle mixed newlines and commas", "v1.0.0,v2.0.0\nv3.0.0", []string{"v1.0.0", "v2.0.0", "v3.0.0"}},
		{"Should accept valid version patterns v1", "v1", []string{"v1"}},
		{"Should accept valid version patterns v1.0", "v1.0", []string{"v1.0"}},
		{"Should accept valid version patterns v1.0.0", "v1.0.0", []string{"v1.0.0"}},
		{"Should accept valid version patterns v1.*", "v1.*", []string{"v1.*"}},
		{"Should accept valid version patterns v1.0.*", "v1.0.*", []string{"v1.0.*"}},
		{"Should skip invalid version patterns", "invalid, v1.0.0, badpattern", []string{"v1.0.0"}},
		{"Should skip versions without 'v' prefix", "1.0.0, v2.0.0", []string{"v2.0.0"}},
		{"Should handle empty entries in lists", "v1.0.0, , , v2.0.0", []string{"v1.0.0", "v2.0.0"}},
		{"Should not parse JSON arrays", `["v1.0.0", "v2.0.0"]`, nil},
		{"Should handle consecutive delimiters", "v1.0.0,,,v2.0.0", []string{"v1.0.0", "v2.0.0"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseIgnoreVersions(c.in, nil)
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
	t.Run("Should warn about invalid patterns", func(t *testing.T) {
		var b bytes.Buffer
		ParseIgnoreVersions("badpattern", &output.Logger{Out: &b, Actions: true})
		if !strings.Contains(b.String(), "Pattern 'badpattern' does not match") {
			t.Fatal(b.String())
		}
	})
}

func valid() state.Config {
	return state.Config{CheckMinorVersion: "error", CheckReleases: "error", CheckImmutability: "error", CheckMarketplace: "none", FloatingVersionsUse: "tags"}
}

func TestValidate(t *testing.T) {
	t.Run("Should return no errors for valid configuration", func(t *testing.T) {
		if e := Validate(valid()); len(e) != 0 {
			t.Fatal(e)
		}
	})
	t.Run("Should accept all valid check-minor-version values", func(t *testing.T) {
		for _, v := range []string{"error", "warning", "none"} {
			c := valid()
			c.CheckMinorVersion = v
			if e := Validate(c); len(e) != 0 {
				t.Fatal(v, e)
			}
		}
	})
	t.Run("Should accept 'tags' and 'branches' for floating-versions-use", func(t *testing.T) {
		for _, v := range []string{"tags", "branches"} {
			c := valid()
			c.FloatingVersionsUse = v
			if e := Validate(c); len(e) != 0 {
				t.Fatal(v, e)
			}
		}
	})
	one := func(name string, mut func(*state.Config), want string) {
		t.Run(name, func(t *testing.T) {
			c := valid()
			mut(&c)
			e := Validate(c)
			if len(e) != 1 || !strings.Contains(e[0], want) {
				t.Fatalf("got %v", e)
			}
		})
	}
	one("Should return error for invalid check-minor-version value", func(c *state.Config) { c.CheckMinorVersion = "invalid" }, "check-minor-version")
	one("Should return error for invalid check-releases value", func(c *state.Config) { c.CheckReleases = "bad" }, "check-releases")
	one("Should return error for invalid check-release-immutability value", func(c *state.Config) { c.CheckImmutability = "invalid" }, "check-release-immutability")
	one("Should return error for invalid floating-versions-use value", func(c *state.Config) { c.FloatingVersionsUse = "invalid" }, "floating-versions-use")
	t.Run("Should return all errors when multiple inputs are invalid", func(t *testing.T) {
		c := state.Config{CheckMinorVersion: "bad1", CheckReleases: "bad2", CheckImmutability: "bad3", CheckMarketplace: "bad4", FloatingVersionsUse: "bad5"}
		if e := Validate(c); len(e) != 5 {
			t.Fatal(e)
		}
	})
}

func TestCheckAutoFixRequirement(t *testing.T) {
	log := func() (*output.Logger, *bytes.Buffer) {
		var b bytes.Buffer
		return &output.Logger{Out: &b, Actions: true}, &b
	}
	t.Run("Should return true when auto-fix is disabled", func(t *testing.T) {
		l, _ := log()
		if !CheckAutoFixRequirement("", false, l) {
			t.Fatal("expected true")
		}
	})
	t.Run("Should return true when auto-fix is disabled even with token", func(t *testing.T) {
		l, _ := log()
		if !CheckAutoFixRequirement("tok", false, l) {
			t.Fatal("expected true")
		}
	})
	t.Run("Should return true when auto-fix is enabled with token", func(t *testing.T) {
		l, _ := log()
		if !CheckAutoFixRequirement("tok", true, l) {
			t.Fatal("expected true")
		}
	})
	t.Run("Should return false when auto-fix is enabled without token", func(t *testing.T) {
		l, b := log()
		if CheckAutoFixRequirement("", true, l) || !strings.Contains(b.String(), "::error title=Auto-fix requires token::") {
			t.Fatal(b.String())
		}
	})
}

func TestWriteDebugInfo(t *testing.T) {
	t.Run("Should not throw when called with valid config and handle empty ignore-versions", func(t *testing.T) {
		WriteDebugInfo(valid(), output.Discard())
	})
	t.Run("Should not print the token and handle missing token gracefully", func(t *testing.T) {
		var b bytes.Buffer
		st := state.New()
		st.Owner, st.Repo = "o", "r"
		WriteRepositoryDebugInfo(st, false, &output.Logger{Out: &b, Actions: true})
		if !strings.Contains(b.String(), "Token available: No") || !strings.Contains(b.String(), "Repository: o/r") {
			t.Fatal(b.String())
		}
	})
}

func TestResolveMode(t *testing.T) {
	cases := []struct {
		name  string
		f     ModeFlags
		want  remediation.Mode
		usage bool
	}{
		{"interactive requires a TTY", ModeFlags{Interactive: true}, 0, true},
		{"interactive on a TTY", ModeFlags{Interactive: true, IsTTY: true}, remediation.Interactive, false},
		{"interactive and auto-fix are mutually exclusive", ModeFlags{Interactive: true, AutoFix: true, IsTTY: true}, 0, true},
		{"interactive and check are mutually exclusive", ModeFlags{Interactive: true, Check: true, IsTTY: true}, 0, true},
		{"auto-fix and check are mutually exclusive", ModeFlags{AutoFix: true, Check: true}, 0, true},
		{"yes and check are mutually exclusive", ModeFlags{Yes: true, Check: true}, 0, true},
		{"yes turns interactive into auto-fix even without a TTY", ModeFlags{Interactive: true, Yes: true}, remediation.AutoFix, false},
		{"yes alone means auto-fix", ModeFlags{Yes: true, IsTTY: true}, remediation.AutoFix, false},
		{"check is report only", ModeFlags{Check: true, IsTTY: true}, remediation.ReportOnly, false},
		{"auto-fix flag", ModeFlags{AutoFix: true}, remediation.AutoFix, false},
		{"default on a TTY is interactive", ModeFlags{IsTTY: true}, remediation.Interactive, false},
		{"default without TTY follows the auto-fix input", ModeFlags{DefaultAutoFix: true}, remediation.AutoFix, false},
		{"default without TTY and input is report only", ModeFlags{}, remediation.ReportOnly, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveMode(c.f)
			var ue *UsageError
			if c.usage {
				if !errors.As(err, &ue) || ue.ExitCode() != 2 {
					t.Fatalf("expected usage error, got %v", err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %v %v want %v", got, err, c.want)
			}
		})
	}
}
