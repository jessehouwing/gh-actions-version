package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jessehouwing/gh-actions-version/internal/policy"
	"github.com/jessehouwing/gh-actions-version/internal/policy/edit"
)

func writeSetupPolicy(env Env, opts setupOptions, generated []byte) error {
	dir := env.Dir
	if dir == "" {
		dir = "."
	}
	path := opts.path
	if path == "" {
		path = "action-version.yml"
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	info, err := os.Lstat(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if exists && !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	var before []byte
	if exists {
		before, err = os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		doc, err := edit.Parse(before)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		setup, err := policy.Parse(generated)
		if err != nil {
			return err
		}
		if err := doc.ApplySetup(setup, opts.noIgnore); err != nil {
			return fmt.Errorf("updating %s: %w", path, err)
		}
		generated, err = doc.Bytes()
		if err != nil {
			return fmt.Errorf("updating %s: %w", path, err)
		}
		if !opts.force {
			if !env.IsTTY {
				return fmt.Errorf("%s already exists; use --force or confirm on a terminal", path)
			}
			prompt := env.Prompter
			if prompt == nil {
				prompt = processPrompter(true)
			}
			confirmed, err := prompt.Confirm("Update existing policy "+path+"?", false)
			if err != nil {
				return fmt.Errorf("confirming policy update: %w", err)
			}
			if !confirmed {
				return fmt.Errorf("policy update declined")
			}
		}
		currentInfo, err := os.Lstat(path)
		if err != nil || !os.SameFile(info, currentInfo) {
			return fmt.Errorf("%s changed while setup was running", path)
		}
		current, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, current) {
			return fmt.Errorf("%s changed while setup was running", path)
		}
		if err := writePolicy(path, generated, info.Mode().Perm()); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("creating policy directory: %w", err)
		}
		if err := writeNewPolicy(path, generated); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	added, removed := lineChanges(before, generated)
	verb := "Created"
	if exists {
		verb = "Updated"
	}
	_, err = fmt.Fprintf(env.Stdout, "%s %s (+%d, -%d lines)\n", verb, path, added, removed)
	return err
}

func writeNewPolicy(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	info, statErr := file.Stat()
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	err = errors.Join(statErr, writeErr, closeErr)
	if err != nil && statErr == nil {
		current, statErr := os.Lstat(path)
		if statErr == nil && os.SameFile(info, current) {
			err = errors.Join(err, os.Remove(path))
		}
	}
	return err
}

func lineChanges(before, after []byte) (added, removed int) {
	lines := map[string]int{}
	for _, line := range strings.Split(strings.TrimSuffix(string(before), "\n"), "\n") {
		if len(before) != 0 {
			lines[line]++
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(after), "\n"), "\n") {
		if len(after) == 0 {
			break
		}
		if lines[line] > 0 {
			lines[line]--
		} else {
			added++
		}
	}
	for _, count := range lines {
		removed += count
	}
	return added, removed
}
