package cmd

import (
	"fmt"
	"strings"

	"github.com/jessehouwing/gh-actions-version/internal/policy"
	"github.com/jessehouwing/gh-actions-version/internal/policy/edit"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func promptSetup(opts *setupOptions, env Env) error {
	prompt := env.Prompter
	if prompt == nil {
		prompt = processPrompter(true)
	}
	selectOne := func(message string, options []string) (string, error) {
		index, err := prompt.Select(message, options)
		if err != nil {
			return "", fmt.Errorf("%s: %w", message, err)
		}
		if index < 0 || index >= len(options) {
			return "", fmt.Errorf("%s: invalid selection", message)
		}
		return options[index], nil
	}
	confirm := func(message string, def bool) (bool, error) {
		value, err := prompt.Confirm(message, def)
		if err != nil {
			return false, fmt.Errorf("%s: %w", message, err)
		}
		return value, nil
	}
	section := func(title, intro string) error {
		_, err := fmt.Fprintf(env.Stderr, "\n%s\n%s\n\n", title, intro)
		return err
	}
	if err := section("Version strategy",
		"Major (v1) and minor (v1.2) versions float to the latest matching patch (v1.2.3).\n"+
			"Patch versions stay fixed. Use tags for floating versions, or branches if your workflows require them.\n"+
			"Yes enables a check as an error; No skips it. Setup only writes a policy and does not change repository refs or releases."); err != nil {
		return err
	}
	var err error
	if opts.floating == "" {
		opts.floating, err = selectOne("Use tags or branches for floating versions?", []string{"tags", "branches"})
		if err != nil {
			return err
		}
	}
	overrides, err := parseLevelOverrides(opts.levels, opts.ruleLevels, opts.findingLevels)
	if err != nil {
		return err
	}
	if overrides.Concerns["minor"].Level == "" {
		yes, err := confirm("Maintain minor floating versions?", true)
		if err != nil {
			return err
		}
		level := "none"
		if yes {
			level = "error"
		}
		opts.levels = append(opts.levels, "minor="+level)
	}
	if opts.preview == "" {
		if err := section("Preview releases",
			"Excluded keeps GitHub Releases marked as prerelease from becoming floating-version targets.\n"+
				"Included allows those releases to be selected too. Prerelease status comes from GitHub, not tag suffixes."); err != nil {
			return err
		}
		opts.preview, err = selectOne("How should preview releases be handled?", []string{"excluded", "included"})
		if err != nil {
			return err
		}
	}
	if err := section("Releases and publication",
		"Patch versions can require GitHub Releases; immutable releases protect published tags and assets from changes.\n"+
			"Marketplace checks require action metadata and Marketplace publication.\n"+
			"Yes reports missing requirements as errors; No skips that group of checks."); err != nil {
		return err
	}
	for _, question := range []struct{ concern, message string }{
		{"releases", "Require releases for patch versions?"},
		{"immutability", "Require immutable releases?"},
		{"marketplace", "Check Marketplace publication?"},
	} {
		if overrides.Concerns[question.concern].Level != "" {
			continue
		}
		yes, err := confirm(question.message, true)
		if err != nil {
			return err
		}
		level := "none"
		if yes {
			level = "error"
		}
		opts.levels = append(opts.levels, question.concern+"="+level)
	}
	if !opts.deletionAnswered {
		if err := section("Irreversible tag deletion",
			"A tag used by a deleted immutable release cannot be republished. Permanently deleting it is irreversible.\n"+
				"No (the default) forbids this deletion; Yes allows it during fixes, subject to the fix policy."); err != nil {
			return err
		}
		opts.allowTagDeletion, err = confirm("Allow irreversible deletion of tags used by deleted immutable releases?", false)
		if err != nil {
			return err
		}
	}
	if len(opts.ignores) == 0 && !opts.noIgnore {
		if err := section("Ignored versions",
			"Ignore exact versions (v1.2.3) or wildcard patterns (v1.*, v1.2.*). Leave the pattern blank to finish.\n"+
				"Scopes: all skips everything; selection excludes floating-version targets;\n"+
				"validation skips checks; fix prevents changes. An optional reason documents why."); err != nil {
			return err
		}
		input, ok := prompt.(interface {
			Input(string, string) (string, error)
		})
		if !ok {
			return fmt.Errorf("terminal prompter does not support text input")
		}
		doc, err := edit.Parse(nil)
		if err != nil {
			return err
		}
		for {
			pattern, err := input.Input("Version pattern to ignore (blank to finish)", "")
			if err != nil {
				return fmt.Errorf("reading ignore pattern: %w", err)
			}
			pattern = strings.TrimSpace(pattern)
			if pattern == "" {
				break
			}
			scope, err := selectOne("Ignore scope?", []string{"all", "selection", "validation", "fix"})
			if err != nil {
				return err
			}
			reason, err := input.Input("Reason (optional)", "")
			if err != nil {
				return fmt.Errorf("reading ignore reason: %w", err)
			}
			if err := doc.Upsert(state.IgnoreEntry{Versions: []string{pattern}, Scope: scope, Reason: reason}); err != nil {
				return err
			}
		}
		opts.interactiveIgnores, err = doc.Entries()
		if err != nil {
			return err
		}
		opts.ignoresAnswered = true
	}
	fixes := map[string]bool{}
	for _, value := range opts.fixes {
		concern, _, _ := strings.Cut(value, "=")
		fixes[strings.TrimSpace(concern)] = true
	}
	if len(fixes) == len(policy.Concerns()) {
		return nil
	}
	if err := section("Fix policies",
		"Auto makes supported fixes eligible for `fix --mode auto` or `fix --mode interactive`.\n"+
			"Manual shows instructions instead and never changes the affected items automatically.\n"+
			"Choose one policy for all unanswered concerns, or fine-tune each group. Explicit --fix-policy answers are kept.\n"+
			"Auto does not run fixes now, enable skipped checks, or bypass the irreversible-deletion safeguard."); err != nil {
		return err
	}
	mode, err := selectOne("How should fix policies be configured?", []string{"fine-tune", "manual", "auto"})
	if err != nil {
		return err
	}
	defaults := policy.Default()
	for _, group := range []struct {
		title, intro string
		concerns     []string
	}{
		{"Version fixes", "Major and minor refs track their latest patch; patch tags stay fixed.\nLatest tracks the highest patch overall; refs enforces the chosen tag or branch type.",
			[]string{"major", "minor", "patch", "latest", "refs"}},
		{"Release and publication fixes", "Releases covers missing, duplicate and floating-version releases and the latest-release marker.\nImmutability covers repository settings and published, immutable releases; Marketplace covers metadata and publication.",
			[]string{"releases", "immutability", "marketplace"}},
		{"Policy maintenance fixes", "Config covers expired ignore entries and patterns that no longer match versions.",
			[]string{"config"}},
	} {
		shown := false
		for _, concern := range group.concerns {
			if fixes[concern] {
				continue
			}
			fix := mode
			if mode == "fine-tune" {
				if !shown {
					if err := section(group.title, group.intro); err != nil {
						return err
					}
					shown = true
				}
				choices := []string{"auto", "manual"}
				if defaults.Concerns[concern].Fix == "manual" {
					choices[0], choices[1] = choices[1], choices[0]
				}
				fix, err = selectOne(concern+" fix policy?", choices)
				if err != nil {
					return err
				}
			}
			opts.fixes = append(opts.fixes, concern+"="+fix)
		}
	}
	return nil
}
