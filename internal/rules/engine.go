// Package rules is the validation rule engine (port of lib/ValidationRules.ps1).
package rules

import (
	"sort"
	"time"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// Rule validates items of the repository state. Condition returns the items to validate,
// Check reports whether an item is already valid and CreateIssue builds the issue for an invalid one.
type Rule struct {
	Name        string
	Description string
	Findings    []FindingDef
	Priority    int
	Category    string
	Condition   func(st *state.State, cfg *state.Config) []any
	Check       func(item any, st *state.State, cfg *state.Config) bool
	CreateIssue func(item any, st *state.State, cfg *state.Config) *state.Issue
}

// FindingDef declares a finding emitted by a rule.
type FindingDef struct {
	ID      string
	Summary string
}

var registry []*Rule

// Register adds a rule; each rule file calls it from init().
func Register(r *Rule) { registry = append(registry, r) }

// validate panics when a required callback is missing (Invoke-ValidationRule throws).
func (r *Rule) validate() {
	if r.Condition == nil || r.Check == nil || r.CreateIssue == nil {
		panic("rule " + r.Name + ": Condition, Check and CreateIssue are required")
	}
}

// All returns the registered rules sorted by priority then name.
func All() []*Rule {
	out := append([]*Rule(nil), registry...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Run executes the rules (all registered rules when rs is nil) and appends the issues to st.
func Run(st *state.State, cfg *state.Config, rs []*Rule) []*state.Issue {
	if rs == nil {
		rs = All()
	}
	var added []*state.Issue
	checkedAt := st.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = time.Now().UTC()
		st.CheckedAt = checkedAt
	}
	for _, r := range rs {
		r.validate()
		view := ruleView(st, cfg, r, checkedAt)
		for _, item := range r.Condition(view, cfg) {
			if r.Check(item, view, cfg) {
				continue
			}
			issue := r.CreateIssue(item, view, cfg)
			if issue == nil {
				continue
			}
			var declared bool
			for i := range r.Findings {
				if r.Findings[i].ID == issue.Finding {
					declared = true
					break
				}
			}
			if !declared {
				panic("rule " + r.Name + " created undeclared finding " + issue.Finding)
			}
			issue.Rule = r.Name
			if view.MatchesIgnore(issue.Version, "all", r.Name, issue.Finding) ||
				view.MatchesIgnore(issue.Version, "validation", r.Name, issue.Finding) {
				st.SuppressedIssues = append(st.SuppressedIssues, issue)
				continue
			}
			if view.MatchesIgnore(issue.Version, "fix", r.Name, issue.Finding) {
				issue.ManualOnly = true
			}
			if p, ok := cfg.Rules[r.Name]; ok {
				level, autoFix := p.Level, p.AutoFix
				if findingPolicy, ok := p.Findings[issue.Finding]; ok {
					if findingPolicy.Level != "" {
						level = findingPolicy.Level
					}
					if findingPolicy.AutoFix != nil {
						autoFix = findingPolicy.AutoFix
					}
				}
				if level == "none" {
					continue
				}
				if level != "" {
					issue.Severity = level
				}
				if autoFix != nil && !*autoFix {
					issue.ManualOnly = true
				}
			}
			st.AddIssue(issue)
			added = append(added, issue)
		}
	}
	return added
}

func ruleView(st *state.State, cfg *state.Config, rule *Rule, checkedAt time.Time) *state.State {
	view := *st
	view.CheckedAt = checkedAt
	view.Config = *cfg
	view.ActiveRule = rule.Name
	if len(rule.Findings) == 1 {
		view.ActiveFindings = []string{rule.Findings[0].ID}
	} else if rule.Name == "patch_release_required" {
		view.ActiveFindings = []string{"release_missing"}
	} else {
		view.ActiveFindings = nil
	}
	refs := func(in []*state.VersionRef) []*state.VersionRef {
		out := make([]*state.VersionRef, 0, len(in))
		for _, ref := range in {
			copy := *ref
			copy.IsIgnored = copy.IsIgnored || view.MatchesIgnore(copy.Version, "all", rule.Name, view.ActiveFindings...)
			out = append(out, &copy)
		}
		return out
	}
	view.Tags, view.Branches = refs(st.Tags), refs(st.Branches)
	view.Releases = make([]*state.ReleaseInfo, 0, len(st.Releases))
	for _, release := range st.Releases {
		copy := *release
		copy.IsIgnored = copy.IsIgnored || view.MatchesIgnore(copy.TagName, "all", rule.Name, view.ActiveFindings...)
		view.Releases = append(view.Releases, &copy)
	}
	return &view
}

// IsPrerelease reports whether the release for ref is a prerelease (Release API only).
func IsPrerelease(st *state.State, v *state.VersionRef) bool {
	r := st.FindRelease(v.Version)
	return r != nil && r.IsPrerelease
}

func highest(st *state.State, pred func(*state.VersionRef) bool, excludePre bool) *state.VersionRef {
	var best *state.VersionRef
	for _, v := range st.AllRefs() {
		if !v.IsPatch || v.IsIgnored || st.SelectionIgnored(v.Version) || !pred(v) || (excludePre && IsPrerelease(st, v)) {
			continue
		}
		if best == nil || less(best, v) {
			best = v
		}
	}
	return best
}

func less(a, b *state.VersionRef) bool {
	if a.Major != b.Major {
		return a.Major < b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor < b.Minor
	}
	return a.Patch < b.Patch
}

func HighestPatchForMajor(st *state.State, major int, excludePre bool) *state.VersionRef {
	return highest(st, func(v *state.VersionRef) bool { return v.Major == major }, excludePre)
}

func HighestPatchForMinor(st *state.State, major, minor int, excludePre bool) *state.VersionRef {
	return highest(st, func(v *state.VersionRef) bool { return v.Major == major && v.Minor == minor }, excludePre)
}

func GlobalHighestPatch(st *state.State, excludePre bool) *state.VersionRef {
	return highest(st, func(*state.VersionRef) bool { return true }, excludePre)
}

// HighestMinorForMajor returns the highest patch for the major, else the highest minor ref (port of Get-HighestMinorForMajor).
func HighestMinorForMajor(st *state.State, major int, excludePre bool) *state.VersionRef {
	if p := HighestPatchForMajor(st, major, excludePre); p != nil {
		return p
	}
	var best *state.VersionRef
	for _, v := range st.AllRefs() {
		if v.IsIgnored || st.SelectionIgnored(v.Version) || v.Major != major || !v.IsMinor || (excludePre && IsPrerelease(st, v)) {
			continue
		}
		if best == nil || v.Minor > best.Minor {
			best = v
		}
	}
	return best
}
