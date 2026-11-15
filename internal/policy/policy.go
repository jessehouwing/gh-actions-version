// Package policy reads action-version.yml: the versioning strategy (tags or branches, which versions
// are checked) and, per concern or rule, whether problems are errors, warnings or ignored and
// the fix policy. The built-in file holds the defaults; a repository file
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
	"time"

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

// Settings is the level and fix policy of a concern or rule; unset fields are inherited.
type Settings struct {
	Level    string              `yaml:"level"`
	Fix      string              `yaml:"fix"`
	Findings map[string]Settings `yaml:"findings"`
}

// File is the content of an action-version.yml.
type File struct {
	Version                        int                 `yaml:"version"`
	FloatingVersionsUse            string              `yaml:"floating-versions-use"`
	Preview                        string              `yaml:"preview"`
	PermanentlyDeleteImmutableTags string              `yaml:"permanently-delete-immutable-tags"`
	IgnoreVersions                 []string            `yaml:"ignore-versions"`
	Ignore                         []state.IgnoreEntry `yaml:"ignore"`
	Concerns                       map[string]Settings `yaml:"concerns"`
	Rules                          map[string]Settings `yaml:"rules"`
	Sources                        map[string]string   `yaml:"-"`
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

// ConcernForRule returns the concern that owns a rule.
func ConcernForRule(name string) (string, bool) {
	for concern, names := range concernRules {
		for _, rule := range names {
			if rule == name {
				return concern, true
			}
		}
	}
	return "", false
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
	if f.Preview != "" && f.Preview != "include" && f.Preview != "exclude" {
		errs = append(errs, fmt.Sprintf("preview: %q is not include or exclude", f.Preview))
	}
	if f.PermanentlyDeleteImmutableTags != "" && f.PermanentlyDeleteImmutableTags != "deny" && f.PermanentlyDeleteImmutableTags != "dangerously-allow" {
		errs = append(errs, fmt.Sprintf("permanently-delete-immutable-tags: %q is not deny or dangerously-allow", f.PermanentlyDeleteImmutableTags))
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
		if s.Fix != "" && s.Fix != "auto" && s.Fix != "manual" {
			errs = append(errs, fmt.Sprintf("%s %s: fix %q is not auto or manual", kind, name, s.Fix))
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
	knownIDs := map[string]bool{}
	for _, r := range rules.All() {
		known[r.Name] = r
		knownIDs[r.Name] = true
		for _, finding := range r.Findings {
			knownIDs[finding.ID] = true
		}
	}
	for i := range f.Ignore {
		entry := &f.Ignore[i]
		if len(entry.Versions) == 0 {
			errs = append(errs, fmt.Sprintf("ignore[%d]: versions must not be empty", i))
		}
		for _, pattern := range entry.Versions {
			if !version.IsValidPattern(pattern) {
				errs = append(errs, fmt.Sprintf("ignore[%d]: invalid version pattern %q", i, pattern))
			}
		}
		if entry.Scope == "" {
			entry.Scope = "all"
		}
		switch entry.Scope {
		case "all", "selection", "validation", "fix":
		default:
			errs = append(errs, fmt.Sprintf("ignore[%d]: invalid scope %q", i, entry.Scope))
		}
		for _, id := range entry.Rules {
			if !knownIDs[id] {
				errs = append(errs, fmt.Sprintf("ignore[%d]: unknown rule or finding %q", i, id))
			}
		}
		if entry.Until != "" {
			if _, err := time.Parse("2006-01-02", entry.Until); err != nil {
				errs = append(errs, fmt.Sprintf("ignore[%d]: invalid until date %q", i, entry.Until))
			}
		}
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

// MarkSource records the origin of each setting explicitly present in f.
func (f *File) MarkSource(source string) {
	if f.Sources == nil {
		f.Sources = map[string]string{}
	}
	mark := func(path, value string) {
		if value != "" {
			f.Sources[path] = source
		}
	}
	mark("floating-versions-use", f.FloatingVersionsUse)
	mark("preview", f.Preview)
	mark("permanently-delete-immutable-tags", f.PermanentlyDeleteImmutableTags)
	if f.IgnoreVersions != nil {
		f.Sources["ignore-versions"] = source
	}
	if f.Ignore != nil {
		f.Sources["ignore"] = source
	}
	for concern, settings := range f.Concerns {
		markSettingsSources(f.Sources, "concerns."+concern, settings, source)
	}
	for name, settings := range f.Rules {
		markSettingsSources(f.Sources, "rules."+name, settings, source)
	}
}

func markSettingsSources(sources map[string]string, prefix string, settings Settings, source string) {
	if settings.Level != "" {
		sources[prefix+".level"] = source
	}
	if settings.Fix != "" {
		sources[prefix+".fix"] = source
	}
	for finding, settings := range settings.Findings {
		markSettingsSources(sources, prefix+".findings."+finding, settings, source)
	}
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
	f.MarkSource("default")
	return f
}

// Merge layers over on top of base; settings that over leaves unset are taken from base.
func Merge(base, over *File) *File {
	out := &File{
		Version:                        base.Version,
		FloatingVersionsUse:            base.FloatingVersionsUse,
		Preview:                        base.Preview,
		PermanentlyDeleteImmutableTags: base.PermanentlyDeleteImmutableTags,
		IgnoreVersions:                 base.IgnoreVersions,
		Ignore:                         base.Ignore,
		Concerns:                       mergeSettings(base.Concerns, over.Concerns),
		Rules:                          mergeSettings(base.Rules, over.Rules),
		Sources:                        mergeSources(base.Sources, over.Sources),
	}
	if over.FloatingVersionsUse != "" {
		out.FloatingVersionsUse = over.FloatingVersionsUse
	}
	if over.Preview != "" {
		out.Preview = over.Preview
	}
	if over.PermanentlyDeleteImmutableTags != "" {
		out.PermanentlyDeleteImmutableTags = over.PermanentlyDeleteImmutableTags
	}
	if over.IgnoreVersions != nil {
		out.IgnoreVersions = over.IgnoreVersions
	}
	if over.Ignore != nil {
		out.Ignore = over.Ignore
	}
	return out
}

func mergeSources(base, over map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(over))
	for key, source := range base {
		out[key] = source
	}
	for key, source := range over {
		out[key] = source
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
		if v.Fix != "" {
			cur.Fix = v.Fix
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
			over.MarkSource("file:" + path)
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
	over.MarkSource("file:" + path)
	return Merge(Default(), over), path, nil
}

// Pinned names the settings already fixed by command line flags; the policy must not change them.
type Pinned struct {
	FloatingVersionsUse, IgnorePreviewReleases, IgnoreVersions, AllowDestructiveTagDeletion bool
	// Levels holds concern levels explicitly set by the command line.
	Levels map[string]bool
}

// Apply writes the policy into cfg, leaving pinned settings untouched.
func (f *File) Apply(cfg *state.Config, pin Pinned) {
	cfg.Sources = mergeSources(f.Sources, nil)
	if cfg.Sources == nil {
		cfg.Sources = map[string]string{}
	}
	if !pin.FloatingVersionsUse && f.FloatingVersionsUse != "" {
		cfg.FloatingVersionsUse = f.FloatingVersionsUse
	}
	if pin.FloatingVersionsUse {
		cfg.Sources["floating-versions-use"] = "flag"
	}
	if !pin.IgnorePreviewReleases && f.Preview != "" {
		cfg.IgnorePreviewReleases = f.Preview == "exclude"
	}
	if pin.IgnorePreviewReleases {
		cfg.Sources["preview"] = "flag"
	}
	if !pin.AllowDestructiveTagDeletion && f.PermanentlyDeleteImmutableTags != "" {
		cfg.AllowDestructiveTagDeletion = f.PermanentlyDeleteImmutableTags == "dangerously-allow"
	}
	if pin.AllowDestructiveTagDeletion {
		cfg.Sources["permanently-delete-immutable-tags"] = "flag"
	}
	if !pin.IgnoreVersions && f.IgnoreVersions != nil {
		cfg.IgnoreVersions = append([]string(nil), f.IgnoreVersions...)
	}
	if pin.IgnoreVersions {
		cfg.Sources["ignore-versions"] = "flag"
	}
	cfg.IgnoreEntries = make([]state.IgnoreEntry, 0, len(cfg.IgnoreVersions)+len(f.Ignore))
	for _, pattern := range cfg.IgnoreVersions {
		cfg.IgnoreEntries = append(cfg.IgnoreEntries, state.IgnoreEntry{Versions: []string{pattern}, Scope: "all"})
	}
	for _, entry := range f.Ignore {
		entry.Versions = append([]string(nil), entry.Versions...)
		entry.Rules = append([]string(nil), entry.Rules...)
		cfg.IgnoreEntries = append(cfg.IgnoreEntries, entry)
	}

	cfg.Rules = map[string]state.RulePolicy{}
	rulesByName := make(map[string]*rules.Rule)
	for _, rule := range rules.All() {
		rulesByName[rule.Name] = rule
	}
	for concern, names := range concernRules {
		s := f.Concerns[concern]
		level := s.Level
		autoFix := fixPolicy(s.Fix)
		concernLevelSource := sourceFor(f.Sources, "concerns."+concern+".level", "default")
		concernFixSource := sourceFor(f.Sources, "concerns."+concern+".fix", "default")
		if field := checkLevel(cfg, concern); field != nil {
			// These concern levels also gate the rules, so they are set on the config; the issue
			// level is then taken from there and per-rule overrides apply below.
			if !pin.Levels[concern] && s.Level != "" {
				*field = s.Level
			}
			level = ""
		}
		for _, name := range names {
			rulePath := "rules." + name
			ruleLevelSource := sourceFor(f.Sources, rulePath+".level", concernLevelSource)
			ruleFixSource := sourceFor(f.Sources, rulePath+".fix", concernFixSource)
			cfg.Sources[rulePath+".level"] = ruleLevelSource
			cfg.Sources[rulePath+".fix"] = ruleFixSource
			if rule := rulesByName[name]; rule != nil {
				for _, finding := range rule.Findings {
					findingPath := rulePath + ".findings." + finding.ID
					cfg.Sources[findingPath+".level"] = sourceFor(f.Sources, findingPath+".level", ruleLevelSource)
					cfg.Sources[findingPath+".fix"] = sourceFor(f.Sources, findingPath+".fix", ruleFixSource)
				}
			}
			if level != "" || autoFix != nil {
				cfg.Rules[name] = state.RulePolicy{Level: level, AutoFix: autoFix}
			}
		}
	}
	for name, s := range f.Rules {
		p := cfg.Rules[name]
		autoFix := fixPolicy(s.Fix)
		if s.Level != "" {
			p.Level = s.Level
		}
		if autoFix != nil {
			p.AutoFix = autoFix
		}
		if len(s.Findings) > 0 {
			p.Findings = map[string]state.FindingPolicy{}
			for finding, settings := range s.Findings {
				p.Findings[finding] = state.FindingPolicy{Level: settings.Level, AutoFix: fixPolicy(settings.Fix)}
			}
		}
		cfg.Rules[name] = p
	}
}

func sourceFor(sources map[string]string, path, fallback string) string {
	if source := sources[path]; source != "" {
		return source
	}
	return fallback
}

func fixPolicy(value string) *bool {
	if value == "" {
		return nil
	}
	enabled := value == "auto"
	return &enabled
}
