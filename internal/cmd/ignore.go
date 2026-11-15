package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jessehouwing/gh-actions-version/internal/policy/edit"
	"github.com/jessehouwing/gh-actions-version/internal/state"
	"github.com/spf13/cobra"
)

func newIgnoreCommand(global *flags, env Env, code *int) *cobra.Command {
	var list, remove bool
	var reason, scope, until string
	var rules []string
	command := &cobra.Command{
		Use:   "ignore [pattern]",
		Short: "Manage ignored versions in the local policy",
		RunE: func(c *cobra.Command, args []string) error {
			if global.noConfig {
				return fmt.Errorf("--no-config cannot be used with ignore")
			}
			if list && remove || list && len(args) != 0 || !list && len(args) != 1 {
				return fmt.Errorf("use ignore PATTERN, ignore --list, or ignore --remove PATTERN")
			}
			if (list || remove) && (c.Flags().Changed("reason") || c.Flags().Changed("scope") || c.Flags().Changed("until") || c.Flags().Changed("rule")) {
				return fmt.Errorf("--reason, --scope, --until and --rule apply only when adding an ignore")
			}
			if err := manageIgnore(env, global.config, args, list, remove, state.IgnoreEntry{
				Reason: reason, Scope: scope, Until: until, Rules: rules,
			}, c.Flags().Changed("reason")); err != nil {
				*code = ExitUsage
				return err
			}
			return nil
		},
	}
	command.Flags().BoolVar(&list, "list", false, "List configured ignores")
	command.Flags().BoolVar(&remove, "remove", false, "Remove an ignore pattern")
	command.Flags().StringVar(&reason, "reason", "", "Reason for the ignore")
	command.Flags().StringVar(&scope, "scope", "all", "Ignore scope: all, selection, validation or fix")
	command.Flags().StringVar(&until, "until", "", "Ignore through this UTC date (YYYY-MM-DD)")
	command.Flags().StringArrayVar(&rules, "rule", nil, "Restrict to a rule or finding ID (repeatable)")
	return command
}

func manageIgnore(env Env, override string, args []string, list, remove bool, entry state.IgnoreEntry, reasonProvided bool) error {
	dir := env.Dir
	if dir == "" {
		dir = "."
	}
	path, err := edit.FindPath(dir, override)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if exists && !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	var data []byte
	if exists {
		data, err = os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
	}
	doc, err := edit.Parse(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if list {
		entries, err := doc.Entries()
		if err != nil {
			return err
		}
		for _, e := range entries {
			fmt.Fprintf(env.Stdout, "%s  scope=%s", strings.Join(e.Versions, ", "), e.Scope)
			if e.Reason != "" {
				fmt.Fprintf(env.Stdout, "  reason=%s", e.Reason)
			}
			if e.Until != "" {
				fmt.Fprintf(env.Stdout, "  until=%s", e.Until)
			}
			if len(e.Rules) != 0 {
				fmt.Fprintf(env.Stdout, "  rules=%s", strings.Join(e.Rules, ","))
			}
			fmt.Fprintln(env.Stdout)
		}
		return nil
	}
	if remove {
		removed, err := doc.Remove(args[0])
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("ignore pattern %q not found", args[0])
		}
	} else {
		entry.Versions = []string{args[0]}
		if err := doc.Upsert(entry); err != nil {
			return err
		}
		if !reasonProvided && env.IsTTY {
			prompt := env.Prompter
			if prompt == nil {
				prompt = processPrompter(false)
			}
			input, ok := prompt.(interface {
				Input(string, string) (string, error)
			})
			if !ok {
				return fmt.Errorf("terminal prompter does not support text input")
			}
			entry.Reason, err = input.Input("Reason (optional)", "")
			if err != nil {
				return fmt.Errorf("reading reason: %w", err)
			}
			if err := doc.Upsert(entry); err != nil {
				return err
			}
		}
	}
	data, err = doc.Bytes()
	if err != nil {
		return err
	}
	mode := os.FileMode(0644)
	if exists {
		mode = info.Mode().Perm()
	}
	if err := writePolicy(path, data, mode); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	fmt.Fprintf(env.Stdout, "Updated %s\n", path)
	return nil
}

func writePolicy(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".action-version-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), path)
}
