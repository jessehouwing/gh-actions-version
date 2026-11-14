// Package rules is the validation rule engine (port of lib/ValidationRules.ps1).
package rules

import (
	"sort"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

// Rule validates items of the repository state. Condition returns the items to validate,
// Check reports whether an item is already valid and CreateIssue builds the issue for an invalid one.
type Rule struct {
	Name        string
	Description string
	Priority    int
	Category    string
	Condition   func(st *state.State, cfg *state.Config) []any
	Check       func(item any, st *state.State, cfg *state.Config) bool
	CreateIssue func(item any, st *state.State, cfg *state.Config) *state.Issue
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
	for _, r := range rs {
		r.validate()
		for _, item := range r.Condition(st, cfg) {
			if r.Check(item, st, cfg) {
				continue
			}
			issue := r.CreateIssue(item, st, cfg)
			if issue == nil {
				continue
			}
			if p, ok := cfg.Rules[r.Name]; ok {
				if p.Level == "none" {
					continue
				}
				if p.Level != "" {
					issue.Severity = p.Level
				}
				if p.AutoFix != nil && !*p.AutoFix {
					issue.ManualOnly = true
				}
			}
			st.AddIssue(issue)
			added = append(added, issue)
		}
	}
	return added
}

// IsPrerelease reports whether the release for ref is a prerelease (Release API only).
func IsPrerelease(st *state.State, v *state.VersionRef) bool {
	r := st.FindRelease(v.Version)
	return r != nil && r.IsPrerelease
}

func highest(st *state.State, pred func(*state.VersionRef) bool, excludePre bool) *state.VersionRef {
	var best *state.VersionRef
	for _, v := range st.AllRefs() {
		if !v.IsPatch || v.IsIgnored || !pred(v) || (excludePre && IsPrerelease(st, v)) {
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
		if v.IsIgnored || v.Major != major || !v.IsMinor || (excludePre && IsPrerelease(st, v)) {
			continue
		}
		if best == nil || v.Minor > best.Minor {
			best = v
		}
	}
	return best
}
