// Package report builds the shared, renderer-independent output model.
package report

import (
	"fmt"
	"sort"
	"time"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/policy"
	"github.com/jessehouwing/gh-actions-version/internal/state"
	"github.com/jessehouwing/gh-actions-version/internal/version"
)

const docsURL = "https://github.com/jessehouwing/gh-actions-version/blob/"
const SchemaVersion = 1

type BuildOptions struct {
	Mode        string
	DocsVersion string
	Timings     map[string]time.Duration
}

type Report struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Repository    Repository          `json:"repository"`
	Mode          string              `json:"mode"`
	Local         bool                `json:"local"`
	Config        Config              `json:"config"`
	Filters       Filters             `json:"filters"`
	GeneratedAt   time.Time           `json:"generatedAt"`
	Refs          Refs                `json:"refs"`
	Findings      []Finding           `json:"findings"`
	Ignored       []IgnoredVersion    `json:"ignored"`
	Suppressed    []SuppressedFinding `json:"-"`
	Groups        []Group             `json:"groups"`
	Skipped       []SkippedCheck      `json:"skipped"`
	Fixes         []Fix               `json:"fixes"`
	Summary       Summary             `json:"summary"`
	Timings       map[string]string   `json:"timings"`
	ExitCode      int                 `json:"exitCode"`
}

type IgnoredVersion struct {
	Version      string   `json:"version"`
	Scope        string   `json:"scope"`
	Reason       string   `json:"reason,omitempty"`
	Until        string   `json:"until,omitempty"`
	MatchedRules []string `json:"matchedRules"`
}

type SuppressedFinding struct {
	Version string
	Rule    string
	Finding string
}

type Filters struct {
	Rules    []string `json:"rules"`
	SkipRule []string `json:"skipRules"`
	Findings []string `json:"findings"`
	Versions []string `json:"versions"`
	Ignored  []string `json:"ignored"`
}

type Refs struct {
	Tags     []Ref     `json:"tags"`
	Branches []Ref     `json:"branches"`
	Releases []Release `json:"releases"`
}

type Ref struct {
	Name    string `json:"name"`
	Ref     string `json:"ref"`
	SHA     string `json:"sha"`
	Ignored bool   `json:"ignored"`
}

type Release struct {
	TagName    string `json:"tagName"`
	SHA        string `json:"sha"`
	ID         int64  `json:"id"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Immutable  bool   `json:"immutable"`
	Ignored    bool   `json:"ignored"`
	Latest     bool   `json:"latest"`
	HTMLURL    string `json:"htmlUrl"`
}

type Repository struct {
	Host          string `json:"host"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	NameWithOwner string `json:"nameWithOwner"`
}

type Config struct {
	CheckMinorVersion           string                `json:"checkMinorVersion"`
	CheckReleases               string                `json:"checkReleases"`
	CheckReleaseImmutability    string                `json:"checkReleaseImmutability"`
	CheckMarketplace            string                `json:"checkMarketplace"`
	IgnorePreviewReleases       bool                  `json:"ignorePreviewReleases"`
	FloatingVersionsUse         string                `json:"floatingVersionsUse"`
	AutoFix                     bool                  `json:"autoFix"`
	AllowDestructiveTagDeletion bool                  `json:"allowDestructiveTagDeletion"`
	IgnoreVersions              []string              `json:"ignoreVersions"`
	IgnoreEntries               []state.IgnoreEntry   `json:"ignoreEntries,omitempty"`
	Rules                       map[string]RulePolicy `json:"rules"`
	Sources                     map[string]string     `json:"sources"`
}

type RulePolicy struct {
	Level    string                   `json:"level,omitempty"`
	Fix      string                   `json:"fix,omitempty"`
	Findings map[string]FindingPolicy `json:"findings,omitempty"`
}

type FindingPolicy struct {
	Level string `json:"level,omitempty"`
	Fix   string `json:"fix,omitempty"`
}

type Finding struct {
	Rule        string                    `json:"rule"`
	Finding     string                    `json:"finding"`
	Concern     string                    `json:"concern"`
	Severity    string                    `json:"severity"`
	Version     string                    `json:"version"`
	Message     string                    `json:"message"`
	Cause       string                    `json:"cause,omitempty"`
	Docs        string                    `json:"docs"`
	AutoFixable bool                      `json:"autoFixable"`
	Manual      []string                  `json:"manualCommands"`
	Status      string                    `json:"status"`
	CurrentSHA  string                    `json:"currentSha,omitempty"`
	ExpectedSHA string                    `json:"expectedSha,omitempty"`
	Action      *actions.SerializedAction `json:"action,omitempty"`
}

type Group struct {
	By       string    `json:"by"`
	Key      string    `json:"key"`
	Concern  string    `json:"concern,omitempty"`
	Rule     string    `json:"rule,omitempty"`
	Versions []string  `json:"versions"`
	Count    int       `json:"count"`
	Findings []Finding `json:"findings"`
}

type SkippedCheck struct {
	Check  string `json:"check"`
	Reason string `json:"reason"`
}

type Fix struct {
	Rule        string   `json:"rule"`
	Finding     string   `json:"finding"`
	Version     string   `json:"version"`
	Status      string   `json:"status"`
	Description string   `json:"description,omitempty"`
	Manual      []string `json:"manualCommands"`
}

type Summary struct {
	Total             int `json:"total"`
	Errors            int `json:"errors"`
	Warnings          int `json:"warnings"`
	Ignored           int `json:"ignored"`
	AutoFixable       int `json:"autoFixable"`
	Fixed             int `json:"fixed"`
	Failed            int `json:"failed"`
	ManualFixRequired int `json:"manualFixRequired"`
	Unfixable         int `json:"unfixable"`
	Pending           int `json:"pending"`
}

// Build creates a report without modifying the input state.
func Build(st *state.State, options BuildOptions) Report {
	version := options.DocsVersion
	if version == "" || version == "dev" {
		version = "main"
	}
	out := Report{
		SchemaVersion: SchemaVersion,
		Repository: Repository{
			Host: st.Host, Owner: st.Owner, Name: st.Repo, NameWithOwner: st.Owner + "/" + st.Repo,
		},
		Mode:     options.Mode,
		Config:   buildConfig(st.Config),
		Findings: make([]Finding, 0, len(st.Issues)),
		Ignored:  ignoredVersions(st),
		Groups:   []Group{},
		Skipped:  make([]SkippedCheck, 0, len(st.SkippedChecks)),
		Fixes:    []Fix{},
		Timings:  formatTimings(options.Timings),
	}
	for _, issue := range st.SuppressedIssues {
		out.Suppressed = append(out.Suppressed, SuppressedFinding{Version: issue.Version, Rule: issue.Rule, Finding: issue.Finding})
	}
	for _, skipped := range st.SkippedChecks {
		out.Skipped = append(out.Skipped, SkippedCheck{Check: skipped.Check, Reason: skipped.Reason})
	}

	versionGroups := map[string]*Group{}
	ruleGroups := map[string]*Group{}
	concernGroups := map[string]*Group{}
	for _, issue := range st.Issues {
		concern, ok := policy.ConcernForRule(issue.Rule)
		if !ok {
			concern = "unknown"
		}
		manual := []string{}
		if issue.Action != nil {
			if commands := issue.Action.ManualCommands(st); commands != nil {
				manual = append(manual, commands...)
			}
		} else if issue.ManualFixCommand != "" {
			manual = append(manual, issue.ManualFixCommand)
		}
		finding := Finding{
			Rule: issue.Rule, Finding: issue.Finding, Concern: concern, Severity: issue.Severity,
			Version: issue.Version, Message: issue.Message, Docs: fmt.Sprintf("%s%s/docs/rules/%s.md", docsURL, version, issue.Rule),
			Cause: causeFor(st, issue), AutoFixable: issue.IsAutoFixable(), Manual: manual, Status: issue.Status,
			CurrentSHA: issue.CurrentSha, ExpectedSHA: issue.ExpectedSha,
		}
		out.Findings = append(out.Findings, finding)
		addToGroup(versionGroups, "version", issue.Version, "", "", finding)
		addToGroup(ruleGroups, "rule", issue.Rule, concern, issue.Rule, finding)
		addToGroup(concernGroups, "concern", concern, concern, "", finding)
		out.Summary.Total++
		if issue.Severity == state.SeverityError {
			out.Summary.Errors++
		} else if issue.Severity == state.SeverityWarning {
			out.Summary.Warnings++
		}
		if issue.IsAutoFixable() {
			out.Summary.AutoFixable++
		}
		switch issue.Status {
		case state.StatusFixed:
			out.Summary.Fixed++
		case state.StatusFailed:
			out.Summary.Failed++
		case state.StatusManualFixRequired:
			out.Summary.ManualFixRequired++
		case state.StatusUnfixable:
			out.Summary.Unfixable++
		case state.StatusPending:
			out.Summary.Pending++
		}
		if options.Mode != "" && options.Mode != "report-only" {
			fix := Fix{
				Rule: issue.Rule, Finding: issue.Finding, Version: issue.Version, Status: issue.Status, Manual: manual,
			}
			if issue.Action != nil {
				fix.Description = issue.Action.Description()
			}
			out.Fixes = append(out.Fixes, fix)
		}
	}
	out.Summary.Ignored = ignoredVersionCount(st, out.Ignored)
	out.Groups = append(out.Groups, groupsFromMap(versionGroups)...)
	out.Groups = append(out.Groups, groupsFromMap(ruleGroups)...)
	out.Groups = append(out.Groups, groupsFromMap(concernGroups)...)
	return out
}

func ignoredVersions(st *state.State) []IgnoredVersion {
	names := map[string]bool{}
	for _, ref := range st.AllRefs() {
		names[ref.Version] = true
	}
	for _, release := range st.Releases {
		names[release.TagName] = true
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		if name != "" {
			sorted = append(sorted, name)
		}
	}
	sort.Strings(sorted)
	now := st.CheckedAt
	if now.IsZero() {
		now = time.Now()
	}
	date := now.UTC().Format("2006-01-02")
	out := []IgnoredVersion{}
	for _, entry := range st.Config.IgnoreEntries {
		if entry.Until != "" && date > entry.Until {
			continue
		}
		scope := entry.Scope
		if scope == "" {
			scope = "all"
		}
		for _, name := range sorted {
			if version.ShouldBeIgnored(name, entry.Versions) {
				out = append(out, IgnoredVersion{Version: name, Scope: scope, Reason: entry.Reason,
					Until: entry.Until, MatchedRules: append([]string{}, entry.Rules...)})
			}
		}
	}
	return out
}

func ignoredVersionCount(st *state.State, entries []IgnoredVersion) int {
	ignored := map[string]bool{}
	for _, entry := range entries {
		ignored[entry.Version] = true
	}
	for _, ref := range st.Tags {
		if ref.IsIgnored {
			ignored[ref.Version] = true
		}
	}
	for _, ref := range st.Branches {
		if ref.IsIgnored {
			ignored[ref.Version] = true
		}
	}
	for _, release := range st.Releases {
		if release.IsIgnored {
			ignored[release.TagName] = true
		}
	}
	return len(ignored)
}

func causeFor(st *state.State, issue *state.Issue) string {
	if issue.Finding != "release_not_immutable" {
		return ""
	}
	if st.ImmutableReleasesEnabled != nil {
		if !*st.ImmutableReleasesEnabled {
			return "The repository setting 'Release immutability' is disabled."
		}
		return ""
	}
	for _, skipped := range st.SkippedChecks {
		if skipped.Check == "immutable_releases_enabled" {
			return "The repository setting 'Release immutability' is unknown because it could not be read (" + skipped.Reason + ")."
		}
	}
	return "The repository setting 'Release immutability' is unknown."
}

func buildConfig(cfg state.Config) Config {
	out := Config{
		CheckMinorVersion: cfg.CheckMinorVersion, CheckReleases: cfg.CheckReleases,
		CheckReleaseImmutability: cfg.CheckImmutability, CheckMarketplace: cfg.CheckMarketplace,
		IgnorePreviewReleases: cfg.IgnorePreviewReleases, FloatingVersionsUse: cfg.FloatingVersionsUse,
		AutoFix: cfg.AutoFix, AllowDestructiveTagDeletion: cfg.AllowDestructiveTagDeletion,
		IgnoreVersions: append([]string{}, cfg.IgnoreVersions...),
		Rules:          map[string]RulePolicy{}, Sources: cloneSources(cfg.Sources),
	}
	for _, entry := range cfg.IgnoreEntries {
		entry.Versions = append([]string(nil), entry.Versions...)
		entry.Rules = append([]string(nil), entry.Rules...)
		out.IgnoreEntries = append(out.IgnoreEntries, entry)
	}
	for name, policy := range cfg.Rules {
		rule := RulePolicy{Level: policy.Level, Fix: fixValue(policy.AutoFix)}
		if len(policy.Findings) > 0 {
			rule.Findings = map[string]FindingPolicy{}
			for finding, settings := range policy.Findings {
				rule.Findings[finding] = FindingPolicy{Level: settings.Level, Fix: fixValue(settings.AutoFix)}
			}
		}
		out.Rules[name] = rule
	}
	return out
}

func fixValue(auto *bool) string {
	if auto == nil {
		return ""
	}
	if *auto {
		return "auto"
	}
	return "manual"
}

func cloneSources(sources map[string]string) map[string]string {
	out := make(map[string]string, len(sources))
	for key, source := range sources {
		out[key] = source
	}
	return out
}

func formatTimings(timings map[string]time.Duration) map[string]string {
	out := make(map[string]string, len(timings))
	for stage, elapsed := range timings {
		out[stage] = elapsed.String()
	}
	return out
}

func addToGroup(groups map[string]*Group, by, key, concern, rule string, finding Finding) {
	group := groups[key]
	if group == nil {
		group = &Group{By: by, Key: key, Concern: concern, Rule: rule, Versions: []string{}, Findings: []Finding{}}
		groups[key] = group
	}
	group.Findings = append(group.Findings, finding)
	group.Count++
	if finding.Version != "" && !contains(group.Versions, finding.Version) {
		group.Versions = append(group.Versions, finding.Version)
		sort.Strings(group.Versions)
	}
}

func groupsFromMap(groups map[string]*Group) []Group {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]Group, 0, len(keys))
	for _, key := range keys {
		out = append(out, *groups[key])
	}
	return out
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}
