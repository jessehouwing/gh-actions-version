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
	for _, question := range []struct{ concern, message string }{
		{"minor", "Maintain minor floating versions?"},
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
	if opts.preview == "" {
		opts.preview, err = selectOne("How should preview releases be handled?", []string{"excluded", "included"})
		if err != nil {
			return err
		}
	}
	if !opts.deletionAnswered {
		opts.allowTagDeletion, err = confirm("Allow irreversible deletion of tags used by deleted immutable releases?", false)
		if err != nil {
			return err
		}
	}
	if len(opts.ignores) == 0 && !opts.noIgnore {
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
	defaults := policy.Default()
	for _, concern := range policy.Concerns() {
		if fixes[concern] {
			continue
		}
		choices := []string{"auto", "manual"}
		if defaults.Concerns[concern].Fix == "manual" {
			choices[0], choices[1] = choices[1], choices[0]
		}
		fix, err := selectOne(concern+" fix policy?", choices)
		if err != nil {
			return err
		}
		opts.fixes = append(opts.fixes, concern+"="+fix)
	}
	return nil
}
