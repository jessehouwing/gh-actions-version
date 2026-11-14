// Package options ports lib/InputValidation.ps1 (input parsing/validation producing state.Config) and
// validates the fix-mode flags of the CLI.
package options

import (
	"fmt"
	"strings"

	"github.com/jessehouwing/gh-actions-version/internal/output"
	"github.com/jessehouwing/gh-actions-version/internal/remediation"
	"github.com/jessehouwing/gh-actions-version/internal/state"
	"github.com/jessehouwing/gh-actions-version/internal/version"
)

// IsValidPattern validates one ignore-versions pattern (Test-ValidVersionPattern).
var IsValidPattern = version.IsValidPattern

// DefaultConfig returns the documented input defaults.
func DefaultConfig() state.Config {
	return state.Config{
		CheckMinorVersion:     "error",
		CheckReleases:         "error",
		CheckImmutability:     "error",
		CheckMarketplace:      "error",
		IgnorePreviewReleases: true,
		FloatingVersionsUse:   "tags",
	}
}

// ParseIgnoreVersions parses ignore-versions from a comma/newline separated string. Invalid patterns
// are skipped with a warning.
func ParseIgnoreVersions(raw string, log *output.Logger) []string {
	if log == nil {
		log = output.Discard()
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	items := strings.FieldsFunc(trimmed, func(r rune) bool { return r == ',' || r == '\r' || r == '\n' })
	var out []string
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		if IsValidPattern(it) {
			out = append(out, it)
		} else {
			log.Annotate("warning", "Invalid ignore-versions pattern", fmt.Sprintf("Pattern '%s' does not match expected format (vX, vX.Y, vX.Y.Z, or wildcard like v1.*). Skipping.", it))
		}
	}
	return out
}

// Validate returns one message per invalid value; empty when valid.
func Validate(cfg state.Config) []string {
	var errs []string
	check := func(name, v string) {
		switch v {
		case "error", "warning", "none":
		default:
			errs = append(errs, fmt.Sprintf("%s must be 'error', 'warning', 'none', 'true', or 'false', got '%s'", name, v))
		}
	}
	check("check-minor-version", cfg.CheckMinorVersion)
	check("check-releases", cfg.CheckReleases)
	check("check-release-immutability", cfg.CheckImmutability)
	check("check-marketplace", cfg.CheckMarketplace)
	if cfg.FloatingVersionsUse != "tags" && cfg.FloatingVersionsUse != "branches" {
		errs = append(errs, fmt.Sprintf("floating-versions-use must be either 'tags' or 'branches', got '%s'", cfg.FloatingVersionsUse))
	}
	return errs
}

// WriteDebugInfo logs the parsed input values (Write-InputDebugInfo).
func WriteDebugInfo(cfg state.Config, log *output.Logger) {
	log.Debug("=== Parsed Input Values ===")
	log.Debug(fmt.Sprintf("auto-fix: %t", cfg.AutoFix))
	log.Debug("check-minor-version: " + cfg.CheckMinorVersion)
	log.Debug("check-releases: " + cfg.CheckReleases)
	log.Debug("check-release-immutability: " + cfg.CheckImmutability)
	log.Debug("check-marketplace: " + cfg.CheckMarketplace)
	log.Debug(fmt.Sprintf("ignore-preview-releases: %t", cfg.IgnorePreviewReleases))
	log.Debug("floating-versions-use: " + cfg.FloatingVersionsUse)
	log.Debug("ignore-versions: " + strings.Join(cfg.IgnoreVersions, ", "))
}

// WriteRepositoryDebugInfo logs repository details without ever printing the token (Write-RepositoryDebugInfo).
func WriteRepositoryDebugInfo(st *state.State, hasToken bool, log *output.Logger) {
	log.Debug(fmt.Sprintf("Repository: %s/%s", st.Owner, st.Repo))
	log.Debug("Host: " + st.Host)
	yn := "No"
	if hasToken {
		yn = "Yes"
	}
	log.Debug("Token available: " + yn)
	log.Debug("Check releases: " + st.Config.CheckReleases)
	log.Debug("Check release immutability: " + st.Config.CheckImmutability)
	log.Debug("Floating versions use: " + st.Config.FloatingVersionsUse)
}

// CheckAutoFixRequirement reports whether auto-fix has the token it needs (Test-AutoFixRequirement).
func CheckAutoFixRequirement(token string, autoFix bool, log *output.Logger) bool {
	if !autoFix {
		return true
	}
	if token == "" {
		log.Annotate("error", "Auto-fix requires token", "Auto-fix mode is enabled but no GitHub token is available. Please provide a token via the 'token' input or ensure GITHUB_TOKEN is available.\n\nExample:\n  - uses: jessehouwing/actions-semver-checker@v2\n    with:\n      auto-fix: true\n      token: ${{ secrets.GITHUB_TOKEN }}")
		return false
	}
	log.Debug("Auto-fix mode enabled with token")
	return true
}

// UsageError is an invalid flag combination; the process should exit with code 2.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// ExitCode is the process exit code for usage errors.
func (e *UsageError) ExitCode() int { return 2 }

// ModeFlags are the fix-mode selectors and modifiers of the CLI.
type ModeFlags struct {
	Fix         bool
	Mode        string
	Interactive bool
	Auto        bool
	Manual      bool
	Yes         bool
	DryRun      bool
	IsTTY       bool
}

// ResolveMode validates the fix-mode selectors and returns the remediation mode.
func ResolveMode(f ModeFlags) (remediation.Mode, error) {
	usage := func(msg string) (remediation.Mode, error) { return remediation.ReportOnly, &UsageError{msg} }
	if !f.Fix {
		return remediation.ReportOnly, nil
	}

	selected := 0
	mode := f.Mode
	if mode != "" {
		selected++
	}
	if f.Interactive {
		selected++
		mode = "interactive"
	}
	if f.Auto {
		selected++
		mode = "auto"
	}
	if f.Manual {
		selected++
		mode = "manual"
	}
	if selected > 1 {
		return usage("fix mode options are mutually exclusive")
	}
	if mode != "" && mode != "interactive" && mode != "auto" && mode != "manual" {
		return usage("--mode must be interactive, auto or manual")
	}
	if f.DryRun && mode != "" && mode != "auto" {
		return usage("--dry-run can only be combined with auto mode")
	}
	if f.DryRun {
		return remediation.DryRun, nil
	}
	switch mode {
	case "manual":
		if f.Yes {
			return usage("--yes cannot be used with manual mode")
		}
		return remediation.Manual, nil
	case "auto":
		return remediation.AutoFix, nil
	case "interactive":
		if !f.IsTTY && !f.Yes {
			return usage("--interactive requires a terminal (TTY); use --auto or --yes in non-interactive environments")
		}
		if f.Yes {
			return remediation.AutoFix, nil
		}
		return remediation.Interactive, nil
	}
	if f.IsTTY && !f.Yes {
		return remediation.Interactive, nil
	}
	return remediation.AutoFix, nil
}
