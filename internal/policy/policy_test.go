package policy

import (
	"context"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/options"
	"github.com/jessehouwing/gh-actions-version/internal/rules"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func TestEveryRuleBelongsToOneConcern(t *testing.T) {
	seen := map[string]string{}
	for concern, names := range concernRules {
		for _, n := range names {
			if prev, dup := seen[n]; dup {
				t.Errorf("rule %s is in both %s and %s", n, prev, concern)
			}
			seen[n] = concern
		}
	}
	for _, r := range rules.All() {
		if seen[r.Name] == "" {
			t.Errorf("rule %s is not in any concern", r.Name)
		}
		delete(seen, r.Name)
	}
	for n := range seen {
		t.Errorf("concern lists unknown rule %s", n)
	}
}

func TestDefaultsMatchBuiltInConfig(t *testing.T) {
	want := options.DefaultConfig()
	got := options.DefaultConfig()
	Default().Apply(&got, Pinned{})
	if got.CheckMinorVersion != want.CheckMinorVersion || got.CheckReleases != want.CheckReleases ||
		got.CheckImmutability != want.CheckImmutability || got.CheckMarketplace != want.CheckMarketplace ||
		got.IgnorePreviewReleases != want.IgnorePreviewReleases || got.FloatingVersionsUse != want.FloatingVersionsUse {
		t.Errorf("defaults differ: %+v vs %+v", got, want)
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	for name, y := range map[string]string{
		"unknown key":     "bogus: 1",
		"unknown concern": "concerns:\n  nope:\n    level: error",
		"unknown rule":    "rules:\n  nope:\n    level: error",
		"unknown finding": "rules:\n  major_tag_missing:\n    findings:\n      not_a_finding:\n        level: warning",
		"bad level":       "concerns:\n  minor:\n    level: loud",
		"bad floating":    "floating-versions-use: both",
	} {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestOverridesAndPrecedence(t *testing.T) {
	over, err := Parse([]byte("concerns:\n  releases:\n    level: warning\n  marketplace:\n    level: none\nrules:\n  patch_tag_missing:\n    autofix: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := options.DefaultConfig()
	Merge(Default(), over).Apply(&cfg, Pinned{})
	if cfg.CheckReleases != "warning" || cfg.CheckMarketplace != "none" {
		t.Errorf("file levels not applied: %+v", cfg)
	}
	if rp := cfg.Rules["patch_tag_missing"]; rp.AutoFix == nil || *rp.AutoFix {
		t.Errorf("autofix override missing: %+v", cfg.Rules)
	}

	cfg = options.DefaultConfig()
	cfg.CheckReleases = "error"
	Merge(Default(), over).Apply(&cfg, Pinned{Levels: map[string]bool{"releases": true}})
	if cfg.CheckReleases != "error" {
		t.Errorf("flag should beat the file, got %s", cfg.CheckReleases)
	}
}

func TestFindingPolicyOverridesRuleAndConcern(t *testing.T) {
	over, err := Parse([]byte(`concerns:
  major:
    level: warning
    autofix: false
rules:
  major_tag_missing:
    level: error
    autofix: true
    findings:
      major_tag_missing:
        level: warning
        autofix: false
`))
	if err != nil {
		t.Fatal(err)
	}

	cfg := options.DefaultConfig()
	Merge(Default(), over).Apply(&cfg, Pinned{})
	st := state.New()
	st.Tags = []*state.VersionRef{state.NewVersionRef("v1.0.0", "refs/tags/v1.0.0", "abc", state.TypeTag)}
	rules.Run(st, &cfg, rules.All())
	for _, issue := range st.Issues {
		if issue.Finding == "major_tag_missing" {
			if issue.Severity != state.SeverityWarning || !issue.ManualOnly {
				t.Fatalf("finding policy was not applied: %+v", issue)
			}
			return
		}
	}
	t.Fatal("expected major_tag_missing finding")
}

func TestLoadLookupOrder(t *testing.T) {
	files := map[string]string{
		".github/action-version.yml": "floating-versions-use: branches\n",
		"action-version.yml":         "floating-versions-use: tags\nconcerns:\n  minor:\n    level: none\n",
	}
	var asked []string
	f, path, err := Load(context.Background(), func(_ context.Context, p string) (string, bool, error) {
		asked = append(asked, p)
		c, ok := files[p]
		return c, ok, nil
	})
	if err != nil || path != "action-version.yml" || f.FloatingVersionsUse != "tags" {
		t.Fatalf("path=%q err=%v file=%+v asked=%v", path, err, f, asked)
	}
	if !strings.Contains(strings.Join(asked, ","), "action-version.yml") {
		t.Errorf("asked %v", asked)
	}
}

func TestRulePolicyAppliedByEngine(t *testing.T) {
	st := state.New()
	st.Owner, st.Repo = "o", "r"
	enabled := false
	st.ImmutableReleasesEnabled = &enabled
	cfg := options.DefaultConfig()
	st.Config = cfg
	rules.Run(st, &cfg, nil)
	if len(st.Issues) == 0 {
		t.Fatal("expected the immutability setting issue")
	}
	st = state.New()
	st.ImmutableReleasesEnabled = &enabled
	cfg.Rules = map[string]state.RulePolicy{"immutable_releases_enabled": {Level: "none"}}
	st.Config = cfg
	rules.Run(st, &cfg, nil)
	for _, i := range st.Issues {
		if i.Finding == "immutable_releases_disabled" {
			t.Error("level none should drop the issue")
		}
	}
}
