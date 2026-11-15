package state

import (
	"testing"
	"time"
)

func TestMatchesIgnoreScopesRestrictionsAndExpiry(t *testing.T) {
	st := New()
	st.CheckedAt = time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC)
	st.Config.IgnoreEntries = []IgnoreEntry{
		{Versions: []string{"v1.*"}, Scope: "selection", Rules: []string{"rule_a"}, Until: "2028-02-29"},
		{Versions: []string{"v1.2.3"}, Scope: "fix", Rules: []string{"finding_b"}},
		{Versions: []string{"v1.*"}, Scope: "all", Until: "2028-02-28"},
	}
	if !st.MatchesIgnore("v1.2.3", "selection", "rule_a", "finding_a") ||
		st.MatchesIgnore("v1.2.3", "selection", "rule_b", "finding_b") ||
		!st.MatchesIgnore("v1.2.3", "fix", "rule_b", "finding_b") ||
		st.MatchesIgnore("v1.2.3", "all", "rule_a", "finding_a") ||
		st.MatchesIgnore("", "fix", "rule_b", "finding_b") {
		t.Fatal("scope, rule/finding restriction, or inclusive expiry is incorrect")
	}
}
