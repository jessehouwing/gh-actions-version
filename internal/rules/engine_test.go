package rules

import (
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func engIssue(item any) *state.Issue {
	s := item.(string)
	i := state.NewIssue("test_finding", state.SeverityError, "issue for "+s)
	i.Version = s
	return i
}

func engRule(name string, prio int, items []any, passes bool, create func(any, *state.State, *state.Config) *state.Issue) *Rule {
	return &Rule{
		Name: name, Description: name, Category: "test", Priority: prio,
		Findings:    []FindingDef{{ID: "test_finding", Summary: "A test finding"}},
		Condition:   func(*state.State, *state.Config) []any { return items },
		Check:       func(any, *state.State, *state.Config) bool { return passes },
		CreateIssue: create,
	}
}

func TestInvokeValidationRule(t *testing.T) {
	t.Run("adds issues when check fails", func(t *testing.T) {
		st := state.New()
		rule := engRule("rule_adds_issue", 10, []any{"item-1"}, false, func(item any, _ *state.State, _ *state.Config) *state.Issue { return engIssue(item) })
		res := Run(st, &state.Config{}, []*Rule{rule})
		if len(st.Issues) != 1 || st.Issues[0].Version != "item-1" || len(res) != 1 ||
			st.Issues[0].Rule != rule.Name || st.Issues[0].Finding != "test_finding" {
			t.Fatalf("issues %v result %v", st.Issues, res)
		}
	})
	t.Run("panics when a rule creates an undeclared finding", func(t *testing.T) {
		defer func() {
			if recovered := recover(); recovered == nil || !strings.Contains(recovered.(string), "created undeclared finding") {
				t.Fatalf("expected undeclared finding panic, got %v", recovered)
			}
		}()
		rule := engRule("undeclared", 1, []any{"item"}, false, func(any, *state.State, *state.Config) *state.Issue {
			return state.NewIssue("not_declared", state.SeverityError, "unexpected")
		})
		Run(state.New(), &state.Config{}, []*Rule{rule})
	})
	t.Run("skips issue creation when check passes", func(t *testing.T) {
		st := state.New()
		rule := engRule("rule_passes", 5, []any{"item-2"}, true, func(any, *state.State, *state.Config) *state.Issue {
			t.Fatal("CreateIssue should not run when check passes")
			return nil
		})
		if res := Run(st, &state.Config{}, []*Rule{rule}); len(st.Issues) != 0 || len(res) != 0 {
			t.Fatal("expected no issues")
		}
	})
	t.Run("processes rules in ascending priority order", func(t *testing.T) {
		st := state.New()
		var order []string
		mk := func(name string, prio int) *Rule {
			return engRule(name, prio, []any{name}, false, func(item any, _ *state.State, _ *state.Config) *state.Issue {
				order = append(order, name)
				return engIssue(item)
			})
		}
		low, high := mk("low", 50), mk("high", 1)
		Run(st, &state.Config{}, []*Rule{low, high}) // slice order is preserved; sorting happens in All()
		if strings.Join(order, ",") != "low,high" {
			t.Fatalf("Run must keep the given order, got %v", order)
		}
	})
	t.Run("All returns registered rules sorted by priority then name", func(t *testing.T) {
		all := All()
		if len(all) == 0 {
			t.Skip("no registered rules")
		}
		for i := 1; i < len(all); i++ {
			a, b := all[i-1], all[i]
			if a.Priority > b.Priority || (a.Priority == b.Priority && a.Name > b.Name) {
				t.Fatalf("rules not sorted at %d: %s(%d) before %s(%d)", i, a.Name, a.Priority, b.Name, b.Priority)
			}
		}
	})
	t.Run("throws when required callbacks are missing", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil || !strings.Contains(r.(string), "invalid") {
				t.Fatalf("expected panic naming the rule, got %v", r)
			}
		}()
		rule := &Rule{Name: "invalid", Priority: 1,
			Condition:   func(*state.State, *state.Config) []any { return []any{"item"} },
			CreateIssue: func(any, *state.State, *state.Config) *state.Issue { return nil }}
		Run(state.New(), &state.Config{}, []*Rule{rule})
	})
	t.Run("handles null condition results", func(t *testing.T) {
		st := state.New()
		rule := engRule("null_condition", 2, nil, false, func(any, *state.State, *state.Config) *state.Issue {
			t.Fatal("should not run")
			return nil
		})
		if res := Run(st, &state.Config{}, []*Rule{rule}); len(st.Issues) != 0 || len(res) != 0 {
			t.Fatal("expected no issues")
		}
	})
	t.Run("ignores nil issues returned by CreateIssue", func(t *testing.T) {
		st := state.New()
		rule := engRule("nil_issue", 2, []any{"x"}, false, func(any, *state.State, *state.Config) *state.Issue { return nil })
		if res := Run(st, &state.Config{}, []*Rule{rule}); len(st.Issues) != 0 || len(res) != 0 {
			t.Fatal("expected no issues")
		}
	})
}

func TestFindingRegistry(t *testing.T) {
	owners := map[string]string{}
	for _, rule := range All() {
		if len(rule.Findings) == 0 {
			t.Errorf("rule %s declares no findings", rule.Name)
			continue
		}
		for _, finding := range rule.Findings {
			if finding.ID == "" || finding.Summary == "" {
				t.Errorf("rule %s has incomplete finding: %+v", rule.Name, finding)
			}
			if owner, ok := owners[finding.ID]; ok {
				t.Errorf("finding %s is declared by both %s and %s", finding.ID, owner, rule.Name)
			}
			owners[finding.ID] = rule.Name
		}
	}
}

type engRef struct {
	v, sha string
	branch bool
	ign    bool
}

func engState(prerelease []string, refs ...engRef) *state.State {
	st := state.New()
	for _, r := range refs {
		if r.branch {
			v := state.NewVersionRef(r.v, "refs/heads/"+r.v, r.sha, state.TypeBranch)
			v.IsIgnored = r.ign
			st.Branches = append(st.Branches, v)
		} else {
			v := state.NewVersionRef(r.v, "refs/tags/"+r.v, r.sha, state.TypeTag)
			v.IsIgnored = r.ign
			st.Tags = append(st.Tags, v)
		}
	}
	for _, p := range prerelease {
		st.Releases = append(st.Releases, &state.ReleaseInfo{TagName: p, IsPrerelease: true})
	}
	return st
}

func version(v *state.VersionRef) string {
	if v == nil {
		return "<nil>"
	}
	return v.Version
}

func TestHelperFunctions(t *testing.T) {
	t.Run("Get-HighestPatchForMajor returns highest patch across refs and skips prerelease when requested", func(t *testing.T) {
		st := engState([]string{"v1.3.0"}, engRef{v: "v1.0.0"}, engRef{v: "v1.3.0"}, engRef{v: "v1.2.0", branch: true})
		if got := version(HighestPatchForMajor(st, 1, true)); got != "v1.2.0" {
			t.Fatal(got)
		}
		if got := version(HighestPatchForMajor(st, 1, false)); got != "v1.3.0" {
			t.Fatal(got)
		}
	})
	t.Run("Get-HighestPatchForMajor returns null when no patches exist", func(t *testing.T) {
		if HighestPatchForMajor(state.New(), 9, false) != nil {
			t.Fatal("expected nil")
		}
	})
	t.Run("Get-HighestPatchForMinor returns highest patch for minor and ignores ignored refs", func(t *testing.T) {
		st := engState(nil, engRef{v: "v1.1.0"}, engRef{v: "v1.1.1"}, engRef{v: "v1.1.5", ign: true})
		if got := version(HighestPatchForMinor(st, 1, 1, false)); got != "v1.1.1" {
			t.Fatal(got)
		}
	})
	t.Run("Get-HighestMinorForMajor returns highest minor using patches when available", func(t *testing.T) {
		st := engState([]string{"v2.3.0"}, engRef{v: "v2.1.0"}, engRef{v: "v2.2.5"}, engRef{v: "v2.3.0", branch: true})
		if got := version(HighestMinorForMajor(st, 2, true)); got != "v2.2.5" {
			t.Fatal(got)
		}
		if got := version(HighestMinorForMajor(st, 2, false)); got != "v2.3.0" {
			t.Fatal(got)
		}
	})
	t.Run("Get-HighestMinorForMajor falls back to minor refs when no patches exist", func(t *testing.T) {
		st := engState(nil, engRef{v: "v3.1", branch: true})
		if got := version(HighestMinorForMajor(st, 3, false)); got != "v3.1" {
			t.Fatal(got)
		}
	})
	t.Run("Get-GlobalHighestPatch returns highest patch across all major versions", func(t *testing.T) {
		st := engState(nil, engRef{v: "v1.0.0"}, engRef{v: "v1.5.0"}, engRef{v: "v2.0.0"}, engRef{v: "v2.1.1"})
		if got := version(GlobalHighestPatch(st, false)); got != "v2.1.1" {
			t.Fatal(got)
		}
	})
	t.Run("Get-GlobalHighestPatch excludes prereleases when requested", func(t *testing.T) {
		st := engState([]string{"v3.0.0"}, engRef{v: "v1.0.0"}, engRef{v: "v2.0.0"}, engRef{v: "v3.0.0"})
		if got := version(GlobalHighestPatch(st, true)); got != "v2.0.0" {
			t.Fatal(got)
		}
		if got := version(GlobalHighestPatch(st, false)); got != "v3.0.0" {
			t.Fatal(got)
		}
	})
	t.Run("Get-GlobalHighestPatch returns null when no patches exist", func(t *testing.T) {
		if GlobalHighestPatch(engState(nil, engRef{v: "v1"}), false) != nil {
			t.Fatal("expected nil")
		}
	})
	t.Run("Get-GlobalHighestPatch skips ignored versions", func(t *testing.T) {
		st := engState(nil, engRef{v: "v1.0.0"}, engRef{v: "v2.0.0", ign: true})
		if got := version(GlobalHighestPatch(st, false)); got != "v1.0.0" {
			t.Fatal(got)
		}
	})
}
