package state

import (
	"time"

	"github.com/jessehouwing/gh-actions-version/internal/version"
)

// MatchesIgnore reports whether an active entry applies to this version, scope and rule.
func (s *State) MatchesIgnore(name, scope, rule string, findings ...string) bool {
	if name == "" {
		return false
	}
	now := s.CheckedAt
	if now.IsZero() {
		now = time.Now()
	}
	date := now.UTC().Format("2006-01-02")
	for _, entry := range s.Config.IgnoreEntries {
		if entry.Until != "" && date > entry.Until {
			continue
		}
		entryScope := entry.Scope
		if entryScope == "" {
			entryScope = "all"
		}
		if entryScope != scope || !version.ShouldBeIgnored(name, entry.Versions) {
			continue
		}
		if len(entry.Rules) == 0 {
			return true
		}
		for _, id := range entry.Rules {
			if id == rule {
				return true
			}
			for _, finding := range findings {
				if id == finding {
					return true
				}
			}
		}
	}
	return false
}

// SelectionIgnored checks selection-scoped ignores for the rule currently being evaluated.
func (s *State) SelectionIgnored(name string) bool {
	return s.MatchesIgnore(name, "selection", s.ActiveRule, s.ActiveFindings...)
}
