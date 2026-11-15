package cmd

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/jessehouwing/gh-actions-version/internal/policy"
	"github.com/jessehouwing/gh-actions-version/internal/state"
	"github.com/jessehouwing/gh-actions-version/internal/version"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type setupOptions struct {
	yes, print, noIgnore, allowTagDeletion bool
	floating, preview                      string
	levels                                 []string
	ruleLevels                             []string
	findingLevels                          []string
	fixes                                  []string
	ignores                                []string
}

type setupSettings struct {
	Level    string                   `yaml:"level,omitempty"`
	Fix      string                   `yaml:"fix,omitempty"`
	Findings map[string]setupSettings `yaml:"findings,omitempty"`
}

type setupPolicy struct {
	Version    int                      `yaml:"version"`
	Floating   string                   `yaml:"floating-versions-use"`
	Preview    string                   `yaml:"preview"`
	DeleteTags string                   `yaml:"permanently-delete-immutable-tags"`
	Ignore     []state.IgnoreEntry      `yaml:"ignore,omitempty"`
	Concerns   map[string]setupSettings `yaml:"concerns"`
	Rules      map[string]setupSettings `yaml:"rules,omitempty"`
}

func newSetupCommand(global *flags, env Env) *cobra.Command {
	var opts setupOptions
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Create a version policy",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if global.noConfig || global.config != "" {
				return fmt.Errorf("--config and --no-config cannot be used with setup --print")
			}
			if !opts.print {
				return fmt.Errorf("setup currently requires --print")
			}
			data, err := buildSetupPolicy(opts)
			if err != nil {
				return err
			}
			_, err = env.Stdout.Write(data)
			return err
		},
	}
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Accept defaults for unanswered questions")
	cmd.Flags().BoolVar(&opts.print, "print", false, "Print the policy instead of writing it")
	cmd.Flags().StringVar(&opts.floating, "floating-versions-use", "", "Use tags or branches for floating versions")
	cmd.Flags().StringVar(&opts.preview, "preview-releases", "", "Include or exclude previews: included or excluded")
	cmd.Flags().BoolVar(&opts.allowTagDeletion, "dangerously-allow-destructive-tag-deletion", false, "Allow irreversible deletion of tags used by deleted immutable releases")
	cmd.Flags().StringArrayVar(&opts.levels, "level", nil, "Set a concern level (repeatable KEY=LEVEL)")
	cmd.Flags().StringArrayVar(&opts.ruleLevels, "rule-level", nil, "Set a rule level (repeatable KEY=LEVEL)")
	cmd.Flags().StringArrayVar(&opts.findingLevels, "finding-level", nil, "Set a finding level (repeatable KEY=LEVEL)")
	cmd.Flags().StringArrayVar(&opts.fixes, "fix-policy", nil, "Set a concern fix policy (repeatable CONCERN=auto|manual)")
	cmd.Flags().StringArrayVar(&opts.ignores, "ignore-version", nil, "Add an all-scoped ignore pattern (repeatable)")
	cmd.Flags().BoolVar(&opts.noIgnore, "no-ignore", false, "Choose no ignored versions")
	return cmd
}

func buildSetupPolicy(opts setupOptions) ([]byte, error) {
	overrides, err := parseLevelOverrides(opts.levels, opts.ruleLevels, opts.findingLevels)
	if err != nil {
		return nil, err
	}
	defaults := policy.Default()
	result := setupPolicy{
		Version: 1, Floating: defaults.FloatingVersionsUse, Preview: defaults.Preview,
		DeleteTags: defaults.PermanentlyDeleteImmutableTags, Concerns: map[string]setupSettings{},
		Rules: map[string]setupSettings{},
	}
	if opts.allowTagDeletion {
		result.DeleteTags = "dangerously-allow"
	}
	if opts.floating != "" {
		result.Floating = strings.ToLower(strings.TrimSpace(opts.floating))
		if result.Floating != "tags" && result.Floating != "branches" {
			return nil, fmt.Errorf("--floating-versions-use must be tags or branches")
		}
	}
	if opts.preview != "" {
		switch strings.ToLower(strings.TrimSpace(opts.preview)) {
		case "included":
			result.Preview = "include"
		case "excluded":
			result.Preview = "exclude"
		default:
			return nil, fmt.Errorf("--preview-releases must be included or excluded")
		}
	}
	fixes := map[string]string{}
	for _, value := range opts.fixes {
		concern, fix, ok := strings.Cut(value, "=")
		concern, fix = strings.TrimSpace(concern), strings.TrimSpace(fix)
		if !ok || (fix != "auto" && fix != "manual") {
			return nil, fmt.Errorf("--fix-policy must be CONCERN=auto|manual")
		}
		if _, ok := defaults.Concerns[concern]; !ok {
			return nil, fmt.Errorf("--fix-policy has unknown concern %q", concern)
		}
		fixes[concern] = fix
	}
	for _, concern := range policy.Concerns() {
		defaultSetting := defaults.Concerns[concern]
		s := setupSettings{Level: defaultSetting.Level, Fix: defaultSetting.Fix}
		if override := overrides.Concerns[concern]; override.Level != "" {
			s.Level = override.Level
		}
		if fix := fixes[concern]; fix != "" {
			s.Fix = fix
		}
		result.Concerns[concern] = s
	}
	for name, settings := range overrides.Rules {
		s := setupSettings{Level: settings.Level}
		if len(settings.Findings) != 0 {
			s.Findings = map[string]setupSettings{}
			for finding, settings := range settings.Findings {
				s.Findings[finding] = setupSettings{Level: settings.Level}
			}
		}
		result.Rules[name] = s
	}
	for _, pattern := range opts.ignores {
		if !version.IsValidPattern(pattern) {
			return nil, fmt.Errorf("--ignore-version: invalid version pattern %q", pattern)
		}
		result.Ignore = append(result.Ignore, state.IgnoreEntry{Versions: []string{pattern}, Scope: "all"})
	}
	if !opts.yes {
		var missing []string
		if opts.floating == "" {
			missing = append(missing, "--floating-versions-use")
		}
		for _, concern := range []string{"minor", "releases", "immutability", "marketplace"} {
			if overrides.Concerns[concern].Level == "" {
				missing = append(missing, "--level="+concern+"=LEVEL")
			}
		}
		if opts.preview == "" {
			missing = append(missing, "--preview-releases")
		}
		if len(opts.ignores) == 0 && !opts.noIgnore {
			missing = append(missing, "--ignore-version or --no-ignore")
		}
		for _, concern := range policy.Concerns() {
			if fixes[concern] == "" {
				missing = append(missing, "--fix-policy="+concern+"=auto|manual")
			}
		}
		if len(missing) != 0 {
			return nil, fmt.Errorf("unanswered setup questions: %s; use --yes to accept defaults", strings.Join(missing, ", "))
		}
	}
	var data bytes.Buffer
	encoder := yaml.NewEncoder(&data)
	encoder.SetIndent(2)
	if err := encoder.Encode(result); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	if _, err := policy.Parse(data.Bytes()); err != nil {
		return nil, fmt.Errorf("generated policy: %w", err)
	}
	return data.Bytes(), nil
}
