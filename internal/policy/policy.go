// Package policy reads action-version.yml: the versioning strategy (tags or branches, which versions
// are checked) and, per concern or rule, whether problems are errors, warnings or ignored and
// whether they may be fixed automatically. The built-in file holds the defaults; a repository file
// is layered over it and command line flags win over both.
package policy

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jessehouwing/gh-actions-version/internal/rules"
	"github.com/jessehouwing/gh-actions-version/internal/state"
	"github.com/jessehouwing/gh-actions-version/internal/version"
)

//go:embed action-version.yml
var defaultFile []byte

// FileNames are the names looked up, in this order, in each search directory.
var FileNames = []string{"action-version.yml", "action-version.yaml"}

// SearchDirs are the directories searched for FileNames, in order: next to action.yaml (the
// repository root), then .github.
var SearchDirs = []string{"", ".github"}

// Settings is the level and autofix choice of a concern or rule; unset fields are inherited.
type Settings struct {
	Level    string              `yaml:"level"`
	AutoFix  *bool               `yaml:"autofix"`
	Findings map[string]Settings `yaml:"findings"`
}

// File is the content of an action-version.yml.
type File struct {
	Version               int                 `yaml:"version"`
	FloatingVersionsUse   string              `yaml:"floating-versions-use"`
	IgnorePreviewReleases *bool               `yaml:"ignore-preview-releases"`
	IgnoreVersions        []string            `yaml:"ignore-versions"`
	Concerns              map[string]Settings `yaml:"concerns"`
	Rules                 map[string]Settings `yaml:"rules"`
}

// concernRules lists the rules of every concern.
var concernRules = map[string][]string{
	"major":  {"major_tag_missing", "major_branch_missing", "major_tag_tracks_highest_patch", "major_branch_tracks_highest_patch"},
	"minor":  {"minor_tag_missing", "minor_branch_missing", "minor_tag_tracks_highest_patch", "minor_branch_tracks_highest_patch"},
	"patch":  {"patch_tag_missing"},
	"latest": {"latest_tag_tracks_global_highest", "latest_branch_tracks_global_highest", "duplicate_latest_ref"},
	"refs":   {"branch_should_be_tag", "tag_should_be_branch", "duplicate_floating_version_ref", "duplicate_patch_version_ref"},
	"releases": {"patch_release_required", "floating_version_no_release", "duplicate_release",
		"highest_patch_release_should_be_latest"},
	"immutability": {"immutable_releases_enabled", "release_should_be_immutable", "release_should_be_published"},
	"marketplace":  {"action_metadata_required", "marketplace_publication_required"},
}

// Concerns returns the concern names, sorted.
func Concerns() []string {
	out := make([]string, 0, len(concernRules))
	for c := range concernRules {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// checkLevel returns the Config field controlled by the concern, or nil.
func checkLevel(cfg *state.Config, concern string) *string {
	switch concern {
	case "minor":
		return &cfg.CheckMinorVersion
	case "releases":
		return &cfg.CheckReleases
	case "immutability":
		return &cfg.CheckImmutability
	case "marketplace":
		return &cfg.CheckMarketplace
	}
	return nil
}

// Parse reads and validates an action-version.yml. Unknown keys, concerns and rules are errors so
// typos do not silently weaken the policy.
func Parse(data []byte) (*File, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil && err != io.EOF {
		return nil, err
	}
	if err := f.validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

func (f *File) validate() error {
	var errs []string
	if f.Version != 0 && f.Version != 1 {
		errs = append(errs, fmt.Sprintf("version: unsupported value %d (expected 1)", f.Version))
	}
	if f.FloatingVersionsUse != "" && f.FloatingVersionsUse != "tags" && f.FloatingVersionsUse != "branches" {
		errs = append(errs, fmt.Sprintf("floating-versions-use: %q is not tags or branches", f.FloatingVersionsUse))
	}
	for _, p := range f.IgnoreVersions {
		if !version.IsValidPattern(p) {
			errs = append(errs, fmt.Sprintf("ignore-versions: %q is not vX, vX.Y, vX.Y.Z or a wildcard like v1.*", p))
		}
	}
	check := func(kind, name string, s Settings) {
		if s.Level != "" && s.Level != "error" && s.Level != "warning" && s.Level != "none" {
			errs = append(errs, fmt.Sprintf("%s %s: level %q is not error, warning or none", kind, name, s.Level))
		}
	}
	for _, name := range sortedKeys(f.Concerns) {
		if _, ok := concernRules[name]; !ok {
			errs = append(errs, fmt.Sprintf("concerns: unknown concern %q (known: %s)", name, strings.Join(Concerns(), ", ")))
		}
		s := f.Concerns[name]
		check("concern", name, s)
		if len(s.Findings) > 0 {
			errs = append(errs, fmt.Sprintf("concern %q: findings can only be configured under a rule", name))
		}
	}
	known := map[string]*rules.Rule{}
	for _, r := range rules.All() {
		known[r.Name] = r
	}
	for _, name := range sortedKeys(f.Rules) {
		r, ok := known[name]
		if !ok {
			errs = append(errs, fmt.Sprintf("rules: unknown rule %q", name))
		}
		s := f.Rules[name]
		check("rule", name, s)
		knownFindings := map[string]bool{}
		if r != nil {
			for _, finding := range r.Findings {
				knownFindings[finding.ID] = true
			}
		}
		for _, finding := range sortedKeys(s.Findings) {
			if !knownFindings[finding] {
				errs = append(errs, fmt.Sprintf("rule %q: unknown finding %q", name, finding))
			}
			check("finding", finding, s.Findings[finding])
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid action-version.yml:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

func sortedKeys(m map[string]Settings) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Default returns the built-in policy.
func Default() *File {
	f, err := Parse(defaultFile)
	if err != nil {
		panic("built-in action-version.yml is invalid: " + err.Error())
	}
	return f
}

// Merge layers over on top of base; settings that over leaves unset are taken from base.
func Merge(base, over *File) *File {
	out := &File{
		Version:               base.Version,
		FloatingVersionsUse:   base.FloatingVersionsUse,
		IgnorePreviewReleases: base.IgnorePreviewReleases,
		IgnoreVersions:        base.IgnoreVersions,
		Concerns:              mergeSettings(base.Concerns, over.Concerns),
		Rules:                 mergeSettings(base.Rules, over.Rules),
	}
	if over.FloatingVersionsUse != "" {
		out.FloatingVersionsUse = over.FloatingVersionsUse
	}
	if over.IgnorePreviewReleases != nil {
		out.IgnorePreviewReleases = over.IgnorePreviewReleases
	}
	if over.IgnoreVersions != nil {
		out.IgnoreVersions = over.IgnoreVersions
	}
	return out
}

func mergeSettings(base, over map[string]Settings) map[string]Settings {
	out := map[string]Settings{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		cur := out[k]
		if v.Level != "" {
			cur.Level = v.Level
		}
		if v.AutoFix != nil {
			cur.AutoFix = v.AutoFix
		}
		if len(v.Findings) > 0 {
			cur.Findings = mergeSettings(cur.Findings, v.Findings)
		}
		out[k] = cur
	}
	return out
}

// Reader returns the content of a repository file; found is false when it does not exist.
type Reader func(ctx context.Context, path string) (content string, found bool, err error)

// Load returns the built-in policy with the repository's action-version.yml layered over it, and
// the path of that file ("" when the repository has none).
func Load(ctx context.Context, read Reader) (*File, string, error) {
	base := Default()
	for _, dir := range SearchDirs {
		for _, name := range FileNames {
			path := name
			if dir != "" {
				path = dir + "/" + name
			}
			content, found, err := read(ctx, path)
			if err != nil {
				return nil, "", fmt.Errorf("reading %s: %w", path, err)
			}
			if !found {
				continue
			}
			over, err := Parse([]byte(content))
			if err != nil {
				return nil, "", fmt.Errorf("%s: %w", path, err)
			}
			return Merge(base, over), path, nil
		}
	}
	return base, "", nil
}

// LoadFrom layers one explicitly named repository file over the built-in policy. Unlike Load, the
// requested file must exist and no search is performed.
func LoadFrom(ctx context.Context, read Reader, path string) (*File, string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, "", fmt.Errorf("config path must not be empty")
	}
	content, found, err := read(ctx, path)
	if err != nil {
		return nil, "", fmt.Errorf("reading %s: %w", path, err)
	}
	if !found {
		return nil, "", fmt.Errorf("config file %s not found", path)
	}
	over, err := Parse([]byte(content))
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	return Merge(Default(), over), path, nil
}

// Pinned names the settings already fixed by command line flags; the policy must not change them.
type Pinned struct {
	FloatingVersionsUse, IgnorePreviewReleases, IgnoreVersions bool
	// Levels holds concern levels explicitly set by the command line.
	Levels map[string]bool
}

// Apply writes the policy into cfg, leaving pinned settings untouched.
func (f *File) Apply(cfg *state.Config, pin Pinned) {
	if !pin.FloatingVersionsUse && f.FloatingVersionsUse != "" {
		cfg.FloatingVersionsUse = f.FloatingVersionsUse
	}
	if !pin.IgnorePreviewReleases && f.IgnorePreviewReleases != nil {
		cfg.IgnorePreviewReleases = *f.IgnorePreviewReleases
	}
	if !pin.IgnoreVersions && f.IgnoreVersions != nil {
		cfg.IgnoreVersions = append([]string(nil), f.IgnoreVersions...)
	}

	cfg.Rules = map[string]state.RulePolicy{}
	for concern, names := range concernRules {
		s := f.Concerns[concern]
		level := s.Level
		if field := checkLevel(cfg, concern); field != nil {
			// These concern levels also gate the rules, so they are set on the config; the issue
			// level is then taken from there and per-rule overrides apply below.
			if !pin.Levels[concern] && s.Level != "" {
				*field = s.Level
			}
			level = ""
		}
		for _, name := range names {
			if level != "" || s.AutoFix != nil {
				cfg.Rules[name] = state.RulePolicy{Level: level, AutoFix: s.AutoFix}
			}
		}
	}
	for name, s := range f.Rules {
		p := cfg.Rules[name]
		if s.Level != "" {
			p.Level = s.Level
		}
		if s.AutoFix != nil {
			p.AutoFix = s.AutoFix
		}
		if len(s.Findings) > 0 {
			p.Findings = map[string]state.FindingPolicy{}
			for finding, settings := range s.Findings {
				p.Findings[finding] = state.FindingPolicy{Level: settings.Level, AutoFix: settings.AutoFix}
			}
		}
		cfg.Rules[name] = p
	}
}
