// Package report builds the shared, renderer-independent output model.
package report

import (
	"fmt"
	"sort"
	"time"

	"github.com/jessehouwing/gh-actions-version/internal/policy"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

const docsURL = "https://github.com/jessehouwing/gh-actions-version/blob/"

type BuildOptions struct {
	Mode        string
	DocsVersion string
	Timings     map[string]time.Duration
}

type Report struct {
	Repository Repository               `json:"repository"`
	Mode       string                   `json:"mode"`
	Config     Config                   `json:"config"`
	Findings   []Finding                `json:"findings"`
	Groups     []Group                  `json:"groups"`
	Skipped    []SkippedCheck           `json:"skipped"`
	Fixes      []Fix                    `json:"fixes"`
	Summary    Summary                  `json:"summary"`
	Timings    map[string]time.Duration `json:"timings"`
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
	Rule        string   `json:"rule"`
	Finding     string   `json:"finding"`
	Concern     string   `json:"concern"`
	Severity    string   `json:"severity"`
	Version     string   `json:"version"`
	Message     string   `json:"message"`
	Cause       string   `json:"cause,omitempty"`
	Docs        string   `json:"docs"`
	AutoFixable bool     `json:"autoFixable"`
	Manual      []string `json:"manual"`
	Status      string   `json:"status"`
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
	Manual      []string `json:"manual"`
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
		Repository: Repository{
			Host: st.Host, Owner: st.Owner, Name: st.Repo, NameWithOwner: st.Owner + "/" + st.Repo,
		},
		Mode:     options.Mode,
		Config:   buildConfig(st.Config),
		Findings: make([]Finding, 0, len(st.Issues)),
		Skipped:  make([]SkippedCheck, 0, len(st.SkippedChecks)),
		Fixes:    []Fix{},
		Timings:  cloneTimings(options.Timings),
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
		}
		finding := Finding{
			Rule: issue.Rule, Finding: issue.Finding, Concern: concern, Severity: issue.Severity,
			Version: issue.Version, Message: issue.Message, Docs: fmt.Sprintf("%s%s/docs/rules/%s.md", docsURL, version, issue.Rule),
			Cause: causeFor(st, issue), AutoFixable: issue.IsAutoFixable(), Manual: manual, Status: issue.Status,
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
	out.Groups = append(out.Groups, groupsFromMap(versionGroups)...)
	out.Groups = append(out.Groups, groupsFromMap(ruleGroups)...)
	out.Groups = append(out.Groups, groupsFromMap(concernGroups)...)
	return out
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

func cloneTimings(timings map[string]time.Duration) map[string]time.Duration {
	out := make(map[string]time.Duration, len(timings))
	for stage, elapsed := range timings {
		out[stage] = elapsed
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
