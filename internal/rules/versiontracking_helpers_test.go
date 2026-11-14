package rules

import (
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/actions"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

type vtRef struct {
	version, sha string
	branch       bool
	ignored      bool
}

func tg(v, sha string) vtRef { return vtRef{version: v, sha: sha} }
func br(v, sha string) vtRef { return vtRef{version: v, sha: sha, branch: true} }
func ign(r vtRef) vtRef      { r.ignored = true; return r }

// vtState builds a state; prerelease lists tags that have a prerelease GitHub release.
func vtState(prerelease []string, refs ...vtRef) *state.State {
	st := state.New()
	for _, r := range refs {
		var v *state.VersionRef
		if r.branch {
			v = state.NewVersionRef(r.version, "refs/heads/"+r.version, r.sha, state.TypeBranch)
			v.IsIgnored = r.ignored
			st.Branches = append(st.Branches, v)
		} else {
			v = state.NewVersionRef(r.version, "refs/tags/"+r.version, r.sha, state.TypeTag)
			v.IsIgnored = r.ignored
			st.Tags = append(st.Tags, v)
		}
	}
	for _, p := range prerelease {
		st.Releases = append(st.Releases, &state.ReleaseInfo{TagName: p, IsPrerelease: true})
	}
	return st
}

func vtRule(t *testing.T, name string) *Rule {
	t.Helper()
	for _, r := range All() {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("rule %s not registered", name)
	return nil
}

func vtFindRef(t *testing.T, st *state.State, version string) *state.VersionRef {
	t.Helper()
	v := st.FindVersion(version, "")
	if v == nil {
		t.Fatalf("ref %s not found", version)
	}
	return v
}

func vtMeta(t *testing.T, name string, priority int) {
	t.Helper()
	r := vtRule(t, name)
	if r.Priority != priority || r.Category != "version_tracking" {
		t.Fatalf("%s: priority/category = %d/%s", name, r.Priority, r.Category)
	}
}

type vtCond struct {
	name       string
	prerelease []string
	refs       []vtRef
	cfg        state.Config
	want       int
}

func vtRunCond(t *testing.T, rule string, cases []vtCond) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := vtState(c.prerelease, c.refs...)
			cfg := c.cfg
			if got := len(vtRule(t, rule).Condition(st, &cfg)); got != c.want {
				t.Fatalf("Condition count = %d, want %d", got, c.want)
			}
		})
	}
}

type vtCheck struct {
	name       string
	prerelease []string
	refs       []vtRef
	item       string // version of the ref under test
	ignorePre  bool
	want       bool
}

func vtRunCheck(t *testing.T, rule string, cases []vtCheck) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := vtState(c.prerelease, c.refs...)
			cfg := &state.Config{IgnorePreviewReleases: c.ignorePre}
			if got := vtRule(t, rule).Check(vtFindRef(t, st, c.item), st, cfg); got != c.want {
				t.Fatalf("Check = %v, want %v", got, c.want)
			}
		})
	}
}

type vtIssue struct {
	name        string
	prerelease  []string
	refs        []vtRef
	item        string // version of the ref under test (tracks rules)
	missing     any    // item for missing rules
	cfg         state.Config
	typ         string
	severity    string // default error
	version     string
	currentSha  string
	expectedSha string
	message     string
	checkAction func(t *testing.T, a state.Action)
}

func vtRunIssue(t *testing.T, rule string, cases []vtIssue) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := vtState(c.prerelease, c.refs...)
			cfg := c.cfg
			var item any = c.missing
			if c.item != "" {
				item = vtFindRef(t, st, c.item)
			}
			issue := vtRule(t, rule).CreateIssue(item, st, &cfg)
			if issue.Type != c.typ {
				t.Errorf("Type = %s, want %s", issue.Type, c.typ)
			}
			sev := c.severity
			if sev == "" {
				sev = state.SeverityError
			}
			if issue.Severity != sev {
				t.Errorf("Severity = %s, want %s", issue.Severity, sev)
			}
			if c.version != "" && issue.Version != c.version {
				t.Errorf("Version = %s, want %s", issue.Version, c.version)
			}
			if issue.CurrentSha != c.currentSha {
				t.Errorf("CurrentSha = %s, want %s", issue.CurrentSha, c.currentSha)
			}
			if c.expectedSha != "" && issue.ExpectedSha != c.expectedSha {
				t.Errorf("ExpectedSha = %s, want %s", issue.ExpectedSha, c.expectedSha)
			}
			if c.message != "" && issue.Message != c.message {
				t.Errorf("Message = %q, want %q", issue.Message, c.message)
			}
			if issue.Action == nil {
				t.Fatal("Action is nil")
			}
			if c.checkAction != nil {
				c.checkAction(t, issue.Action)
			}
		})
	}
}

func wantCreateTag(tag, sha string) func(*testing.T, state.Action) {
	return func(t *testing.T, a state.Action) {
		x, ok := a.(*actions.CreateTagAction)
		if !ok || x.TagName != tag || x.Sha != sha {
			t.Fatalf("action = %#v, want CreateTagAction %s %s", a, tag, sha)
		}
	}
}

func wantCreateBranch(name, sha string) func(*testing.T, state.Action) {
	return func(t *testing.T, a state.Action) {
		x, ok := a.(*actions.CreateBranchAction)
		if !ok || x.BranchName != name || x.Sha != sha {
			t.Fatalf("action = %#v, want CreateBranchAction %s %s", a, name, sha)
		}
	}
}

func wantUpdateTag(tag, sha string) func(*testing.T, state.Action) {
	return func(t *testing.T, a state.Action) {
		x, ok := a.(*actions.UpdateTagAction)
		if !ok || x.TagName != tag || x.Sha != sha || !x.Force {
			t.Fatalf("action = %#v, want forced UpdateTagAction %s %s", a, tag, sha)
		}
	}
}

func wantUpdateBranch(name, sha string) func(*testing.T, state.Action) {
	return func(t *testing.T, a state.Action) {
		x, ok := a.(*actions.UpdateBranchAction)
		if !ok || x.BranchName != name || x.Sha != sha || !x.Force {
			t.Fatalf("action = %#v, want forced UpdateBranchAction %s %s", a, name, sha)
		}
	}
}
