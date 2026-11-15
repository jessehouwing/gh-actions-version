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
		got.IgnorePreviewReleases != want.IgnorePreviewReleases || got.FloatingVersionsUse != want.FloatingVersionsUse ||
		got.AllowDestructiveTagDeletion != want.AllowDestructiveTagDeletion {
		t.Errorf("defaults differ: %+v vs %+v", got, want)
	}
}

func TestConfigurationSourcesFollowPrecedence(t *testing.T) {
	f, _, err := Load(context.Background(), func(_ context.Context, path string) (string, bool, error) {
		if path != "action-version.yml" {
			return "", false, nil
		}
		return `floating-versions-use: tags
preview: exclude
concerns:
  major:
    level: error
rules:
  major_tag_missing:
    findings:
      major_tag_missing:
        level: warning
`, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := options.DefaultConfig()
	f.Apply(&cfg, Pinned{})
	for path, want := range map[string]string{
		"floating-versions-use":         "file:action-version.yml",
		"preview":                       "file:action-version.yml",
		"concerns.major.level":          "file:action-version.yml",
		"rules.major_tag_missing.level": "file:action-version.yml",
		"rules.major_tag_missing.findings.major_tag_missing.level": "file:action-version.yml",
		"concerns.minor.level": "default",
	} {
		if got := cfg.Sources[path]; got != want {
			t.Errorf("source for %q = %q, want %q", path, got, want)
		}
	}

	over, err := Parse([]byte("concerns:\n  major:\n    level: warning\n"))
	if err != nil {
		t.Fatal(err)
	}
	over.MarkSource("flag")
	cfg = options.DefaultConfig()
	Merge(f, over).Apply(&cfg, Pinned{})
	if got := cfg.Sources["concerns.major.level"]; got != "flag" {
		t.Errorf("source for command-line concern override = %q, want flag", got)
	}
	if got := cfg.Sources["rules.major_tag_missing.level"]; got != "flag" {
		t.Errorf("source for inherited command-line rule level = %q, want flag", got)
	}

	cfg = options.DefaultConfig()
	f.Apply(&cfg, Pinned{FloatingVersionsUse: true, IgnorePreviewReleases: true})
	if got := cfg.Sources["floating-versions-use"]; got != "flag" {
		t.Errorf("source for floating-versions-use flag = %q, want flag", got)
	}
	if got := cfg.Sources["preview"]; got != "flag" {
		t.Errorf("source for preview flag = %q, want flag", got)
	}
}

func TestPreviewPolicy(t *testing.T) {
	for _, tc := range []struct {
		value         string
		ignorePreview bool
	}{
		{value: "include", ignorePreview: false},
		{value: "exclude", ignorePreview: true},
	} {
		over, err := Parse([]byte("preview: " + tc.value + "\n"))
		if err != nil {
			t.Fatalf("preview %q: %v", tc.value, err)
		}
		cfg := options.DefaultConfig()
		Merge(Default(), over).Apply(&cfg, Pinned{})
		if cfg.IgnorePreviewReleases != tc.ignorePreview {
			t.Errorf("preview %q set IgnorePreviewReleases=%t, want %t", tc.value, cfg.IgnorePreviewReleases, tc.ignorePreview)
		}
	}

	over, err := Parse([]byte("preview: exclude\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := options.DefaultConfig()
	cfg.IgnorePreviewReleases = false
	Merge(Default(), over).Apply(&cfg, Pinned{IgnorePreviewReleases: true})
	if cfg.IgnorePreviewReleases {
		t.Error("policy should not override the command-line preview choice")
	}
}

func TestImmutableTagDeletionPolicy(t *testing.T) {
	for _, tc := range []struct {
		value string
		allow bool
	}{{"deny", false}, {"dangerously-allow", true}} {
		t.Run(tc.value, func(t *testing.T) {
			over, err := Parse([]byte("permanently-delete-immutable-tags: " + tc.value + "\n"))
			if err != nil {
				t.Fatal(err)
			}
			over.MarkSource("file:action-version.yml")
			cfg := options.DefaultConfig()
			Merge(Default(), over).Apply(&cfg, Pinned{})
			if cfg.AllowDestructiveTagDeletion != tc.allow || cfg.Sources["permanently-delete-immutable-tags"] != "file:action-version.yml" {
				t.Fatalf("effective config = %+v", cfg)
			}
		})
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	for name, y := range map[string]string{
		"unknown key":        "bogus: 1",
		"unknown concern":    "concerns:\n  nope:\n    level: error",
		"unknown rule":       "rules:\n  nope:\n    level: error",
		"unknown finding":    "rules:\n  major_tag_missing:\n    findings:\n      not_a_finding:\n        level: warning",
		"bad level":          "concerns:\n  minor:\n    level: loud",
		"bad floating":       "floating-versions-use: both",
		"bad preview":        "preview: previews",
		"bad deletion":       "permanently-delete-immutable-tags: allow",
		"boolean deletion":   "permanently-delete-immutable-tags: true",
		"old preview key":    "ignore-preview-releases: false",
		"old autofix key":    "concerns:\n  minor:\n    autofix: false",
		"bad fix":            "concerns:\n  minor:\n    fix: never",
		"ignore unknown key": "ignore:\n  - versions: [v1]\n    nope: x",
		"ignore no versions": "ignore:\n  - reason: test",
		"ignore bad pattern": "ignore:\n  - versions: ['v1.2.3-beta']",
		"ignore bad scope":   "ignore:\n  - versions: [v1]\n    scope: forever",
		"ignore bad rule":    "ignore:\n  - versions: [v1]\n    rules: [does_not_exist]",
		"ignore bad date":    "ignore:\n  - versions: [v1]\n    until: 2027-02-29",
	} {
		if _, err := Parse([]byte(y)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestStructuredIgnorePolicy(t *testing.T) {
	p, err := Parse([]byte(`ignore-versions: [v0.*]
ignore:
  - versions: [v1.2.3, v1.3.*]
    scope: selection
    reason: yanked
    rules: [major_tag_missing, release_missing]
    until: 2028-02-29
  - versions: [v2]
`))
	if err != nil {
		t.Fatal(err)
	}
	p.MarkSource("file:action-version.yml")
	cfg := options.DefaultConfig()
	Merge(Default(), p).Apply(&cfg, Pinned{})
	if len(cfg.IgnoreEntries) != 3 || cfg.IgnoreEntries[0].Versions[0] != "v0.*" || cfg.IgnoreEntries[0].Scope != "all" {
		t.Fatalf("simple ignore = %+v", cfg.IgnoreEntries)
	}
	if e := cfg.IgnoreEntries[1]; e.Scope != "selection" || e.Reason != "yanked" || e.Until != "2028-02-29" || len(e.Rules) != 2 {
		t.Fatalf("structured ignore = %+v", e)
	}
	if cfg.IgnoreEntries[2].Scope != "all" || cfg.Sources["ignore"] != "file:action-version.yml" {
		t.Fatalf("default scope/source = %+v", cfg)
	}
}

func TestOverridesAndPrecedence(t *testing.T) {
	over, err := Parse([]byte("concerns:\n  releases:\n    level: warning\n  marketplace:\n    level: none\nrules:\n  patch_tag_missing:\n    fix: manual\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := options.DefaultConfig()
	Merge(Default(), over).Apply(&cfg, Pinned{})
	if cfg.CheckReleases != "warning" || cfg.CheckMarketplace != "none" {
		t.Errorf("file levels not applied: %+v", cfg)
	}
	if rp := cfg.Rules["patch_tag_missing"]; rp.AutoFix == nil || *rp.AutoFix {
		t.Errorf("manual fix override missing: %+v", cfg.Rules)
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
    fix: manual
rules:
  major_tag_missing:
    level: error
    fix: auto
    findings:
      major_tag_missing:
        level: warning
        fix: manual
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

func TestRuleCanOverrideManualConcernWithAutoFix(t *testing.T) {
	over, err := Parse([]byte(`concerns:
  major:
    fix: manual
rules:
  major_tag_missing:
    fix: auto
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
			if issue.ManualOnly {
				t.Fatalf("auto fix setting was not applied: %+v", issue)
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

func TestLoadFromUsesOnlyExplicitPath(t *testing.T) {
	var asked []string
	f, path, err := LoadFrom(context.Background(), func(_ context.Context, p string) (string, bool, error) {
		asked = append(asked, p)
		return "floating-versions-use: branches\n", p == ".github/custom.yml", nil
	}, ".github/custom.yml")
	if err != nil || path != ".github/custom.yml" || f.FloatingVersionsUse != "branches" {
		t.Fatalf("path=%q err=%v file=%+v", path, err, f)
	}
	if len(asked) != 1 || asked[0] != ".github/custom.yml" {
		t.Fatalf("read paths = %v; want only the explicit path", asked)
	}
	if _, _, err := LoadFrom(context.Background(), func(context.Context, string) (string, bool, error) {
		return "", false, nil
	}, "missing.yml"); err == nil {
		t.Fatal("expected a missing explicit file to fail")
	}
	if _, _, err := LoadFrom(context.Background(), nil, " "); err == nil {
		t.Fatal("expected an empty path to fail")
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
