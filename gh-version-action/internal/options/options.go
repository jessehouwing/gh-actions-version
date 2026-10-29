// Package options ports lib/InputValidation.ps1 (input parsing/validation producing state.Config) and
// validates the fix-mode flags of the CLI.
package options

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jessehouwing/gh-version-action/internal/output"
	"github.com/jessehouwing/gh-version-action/internal/remediation"
	"github.com/jessehouwing/gh-version-action/internal/state"
	"github.com/jessehouwing/gh-version-action/internal/version"
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

// ConvertToCheckLevel normalizes a check input: trimmed, lower-cased, "true" -> "error", "false" -> "none".
// Empty input stays empty (callers apply defaults).
func ConvertToCheckLevel(value string) string {
	n := strings.ToLower(strings.TrimSpace(value))
	switch n {
	case "true":
		return "error"
	case "false":
		return "none"
	}
	return n
}

// ParseIgnoreVersions parses ignore-versions from a comma/newline separated string, a JSON array string
// or an already parsed []string / []any. Invalid patterns are skipped with a warning.
func ParseIgnoreVersions(raw any, log *output.Logger) []string {
	if log == nil {
		log = output.Discard()
	}
	var items []string
	switch v := raw.(type) {
	case nil:
		return nil
	case []string:
		items = v
	case []any:
		for _, e := range v {
			items = append(items, fmt.Sprint(e))
		}
	case string:
		trimmed := strings.TrimSpace(v)
		parsed := false
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			var arr []any
			if err := json.Unmarshal([]byte(trimmed), &arr); err != nil {
				log.Annotate("warning", "Invalid JSON in ignore-versions", "Failed to parse JSON array. Treating as comma/newline-separated list.")
			} else {
				parsed = len(arr) > 0
				for _, e := range arr {
					items = append(items, fmt.Sprint(e))
				}
			}
		}
		if !parsed && trimmed != "" {
			items = strings.FieldsFunc(trimmed, func(r rune) bool { return r == ',' || r == '\r' || r == '\n' })
		}
	default:
		return nil
	}
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

// ActionInput is the parsed action input.
type ActionInput struct {
	Token  string
	Config state.Config
}

func asString(v any) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	}
	return fmt.Sprint(v), true
}

// ReadActionInput parses the action inputs JSON (the `inputs` environment variable). fallbackToken is
// used when the token input is empty; a token that differs from envToken is masked in the log.
func ReadActionInput(inputsJSON, fallbackToken, envToken string, log *output.Logger) (*ActionInput, error) {
	if log == nil {
		log = output.Discard()
	}
	if inputsJSON == "" {
		return nil, errors.New("inputs environment variable is not set")
	}
	var in map[string]any
	if err := json.Unmarshal([]byte(inputsJSON), &in); err != nil {
		return nil, errors.New("failed to parse inputs JSON")
	}
	get := func(key, def string) string {
		if s, ok := asString(in[key]); ok {
			return s
		}
		return def
	}

	res := &ActionInput{Token: fallbackToken}
	if tok, _ := asString(in["token"]); strings.TrimSpace(tok) != "" {
		res.Token = tok
		if tok != envToken {
			log.Mask(tok)
		}
	}
	cfg := DefaultConfig()
	cfg.CheckMinorVersion = ConvertToCheckLevel(get("check-minor-version", "error"))
	cfg.CheckReleases = ConvertToCheckLevel(get("check-releases", "error"))
	cfg.CheckImmutability = ConvertToCheckLevel(get("check-release-immutability", "error"))
	cfg.CheckMarketplace = ConvertToCheckLevel(get("check-marketplace", "error"))
	cfg.IgnorePreviewReleases = strings.EqualFold(strings.TrimSpace(get("ignore-preview-releases", "true")), "true")
	cfg.AutoFix = strings.EqualFold(strings.TrimSpace(get("auto-fix", "false")), "true")
	cfg.FloatingVersionsUse = strings.ToLower(strings.TrimSpace(get("floating-versions-use", "tags")))
	cfg.IgnoreVersions = ParseIgnoreVersions(in["ignore-versions"], log)
	if len(cfg.IgnoreVersions) > 0 {
		log.Debug("Ignoring versions: " + strings.Join(cfg.IgnoreVersions, ", "))
	}
	res.Config = cfg
	return res, nil
}

// Validate returns one message per invalid value (Test-ActionInput); empty when valid.
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

// ModeFlags are the fix-mode CLI flags.
type ModeFlags struct {
	AutoFix     bool // --auto-fix
	Check       bool // --check / --no-fix
	Interactive bool // -i / --interactive
	Yes         bool // --yes
	IsTTY       bool // stdin and stdout are terminals
	// DefaultAutoFix is the auto-fix default when no mode flag is given and not on a TTY (the auto-fix input).
	DefaultAutoFix bool
}

// ResolveMode validates the flag combination and returns the remediation mode:
//   - --interactive is mutually exclusive with --auto-fix and --check, and requires a TTY unless --yes is given
//   - --yes turns interactive into auto-fix and cannot be combined with --check
//   - --auto-fix and --check are mutually exclusive
//   - with no mode flag: interactive on a TTY, otherwise auto-fix when DefaultAutoFix else report-only
func ResolveMode(f ModeFlags) (remediation.Mode, error) {
	usage := func(msg string) (remediation.Mode, error) { return remediation.ReportOnly, &UsageError{msg} }
	switch {
	case f.Interactive && f.AutoFix:
		return usage("--interactive and --auto-fix are mutually exclusive")
	case f.Interactive && f.Check:
		return usage("--interactive and --check are mutually exclusive")
	case f.AutoFix && f.Check:
		return usage("--auto-fix and --check are mutually exclusive")
	case f.Yes && f.Check:
		return usage("--yes and --check are mutually exclusive")
	}
	switch {
	case f.Check:
		return remediation.ReportOnly, nil
	case f.AutoFix || f.Yes:
		return remediation.AutoFix, nil
	case f.Interactive:
		if !f.IsTTY {
			return usage("--interactive requires a terminal (TTY); use --auto-fix or --check in non-interactive environments")
		}
		return remediation.Interactive, nil
	case f.IsTTY:
		return remediation.Interactive, nil
	case f.DefaultAutoFix:
		return remediation.AutoFix, nil
	}
	return remediation.ReportOnly, nil
}
