package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/policy"
)

func TestSetupWritesAndUpdatesPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".github", "action-version.yml")
	var out, errOut bytes.Buffer
	env := Env{Dir: dir, Stdout: &out, Stderr: &errOut}
	run := func(args ...string) int {
		out.Reset()
		errOut.Reset()
		return Run(context.Background(), args, env)
	}
	if code := run("setup", "--yes", "--path", path, "--preview-releases=included"); code != ExitOK {
		t.Fatalf("create exit %d: %s", code, errOut.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := policy.Parse(data)
	if err != nil || p.Preview != "include" || p.PermanentlyDeleteImmutableTags != "deny" || !strings.Contains(out.String(), "Created") {
		t.Fatalf("created: %s, output %s, error %v", data, out.String(), err)
	}
	existing := "# keep this comment\npreview: exclude # keep preview\nignore:\n  - versions: [v1]\n    scope: selection\n    reason: existing\nrules:\n  major_tag_missing:\n    fix: manual # keep override\n"
	if err := os.WriteFile(path, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	if code := run("setup", "--yes", "--path", path); code != ExitUsage {
		t.Fatalf("existing without force: %d %s", code, errOut.String())
	}
	unchanged, _ := os.ReadFile(path)
	if string(unchanged) != existing {
		t.Fatal("unconfirmed file was changed")
	}
	if code := run("setup", "--yes", "--force", "--path", path, "--preview-releases=included", "--dangerously-allow-destructive-tag-deletion", "--rule-level=major_tag_missing=warning", "--finding-level=major_tag_missing=none", "--ignore-version=v2"); code != ExitOK {
		t.Fatalf("update exit %d: %s", code, errOut.String())
	}
	data, err = os.ReadFile(path)
	p, parseErr := policy.Parse(data)
	if err != nil || parseErr != nil || !strings.Contains(string(data), "# keep this comment") || !strings.Contains(string(data), "# keep preview") ||
		!strings.Contains(string(data), "# keep override") || p.Preview != "include" || p.PermanentlyDeleteImmutableTags != "dangerously-allow" ||
		len(p.Ignore) != 2 || p.Ignore[0].Reason != "existing" || p.Rules["major_tag_missing"].Fix != "manual" ||
		p.Rules["major_tag_missing"].Level != "warning" || p.Rules["major_tag_missing"].Findings["major_tag_missing"].Level != "none" || !strings.Contains(out.String(), "Updated") {
		t.Fatalf("updated: %s, output %s, errors %v %v", data, out.String(), err, parseErr)
	}
	if code := run("setup", "--yes", "--force", "--path", path, "--no-ignore"); code != ExitOK {
		t.Fatalf("clear ignores exit %d: %s", code, errOut.String())
	}
	data, _ = os.ReadFile(path)
	p, parseErr = policy.Parse(data)
	if parseErr != nil || len(p.Ignore) != 0 || len(p.IgnoreVersions) != 0 {
		t.Fatalf("ignores not cleared: %s %v", data, parseErr)
	}
}

type overwritePrompter struct {
	setupTestPrompter
	accept bool
}

func (p *overwritePrompter) Confirm(message string, def bool) (bool, error) {
	p.asked = append(p.asked, message)
	if strings.Contains(message, "Update existing") {
		return p.accept, nil
	}
	return def, nil
}

func TestSetupRequiresOverwriteConfirmation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "action-version.yml")
	original := "preview: include\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	prompt := &overwritePrompter{}
	env := Env{Dir: dir, Stdout: &out, Stderr: &errOut, IsTTY: true, Prompter: prompt}
	if code := Run(context.Background(), []string{"setup", "--yes"}, env); code != ExitUsage {
		t.Fatalf("declined exit %d: %s", code, errOut.String())
	}
	content, _ := os.ReadFile(path)
	if string(content) != original {
		t.Fatal("declined overwrite changed file")
	}
	prompt.accept = true
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"setup", "--yes"}, env); code != ExitOK {
		t.Fatalf("accepted exit %d: %s", code, errOut.String())
	}
}

func TestSetupRejectsInvalidExistingPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "action-version.yml")
	original := "ignore: [bad\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := Run(context.Background(), []string{"setup", "--yes", "--force"}, Env{Dir: dir, Stdout: &out, Stderr: &out}); code != ExitUsage {
		t.Fatalf("invalid policy exit %d: %s", code, out.String())
	}
	content, _ := os.ReadFile(path)
	if string(content) != original {
		t.Fatal("invalid file changed")
	}
}

type concurrentEditPrompter struct {
	setupTestPrompter
	path string
}

func (p *concurrentEditPrompter) Confirm(string, bool) (bool, error) {
	if err := os.WriteFile(p.path, []byte("preview: include # concurrent\n"), 0600); err != nil {
		return false, err
	}
	return true, nil
}

func TestSetupDoesNotOverwriteConcurrentEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "action-version.yml")
	if err := os.WriteFile(path, []byte("preview: exclude\n"), 0600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	env := Env{Dir: dir, Stdout: &out, Stderr: &out, IsTTY: true, Prompter: &concurrentEditPrompter{path: path}}
	if code := Run(context.Background(), []string{"setup", "--yes"}, env); code != ExitUsage || !strings.Contains(out.String(), "changed while setup was running") {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "preview: include # concurrent\n" {
		t.Fatalf("concurrent edit lost: %s %v", b, err)
	}
}

func TestSetupRejectsAnchorsAndAliasesInManagedFields(t *testing.T) {
	for _, tc := range []struct {
		name, content string
	}{
		{"scalar anchor", "preview: &preview include\n"},
		{"scalar alias", "rules:\n  major_tag_missing:\n    level: &level warning\n  minor_tag_missing:\n    level: *level\n"},
		{"nested anchor", "rules:\n  major_tag_missing:\n    level: &level warning\n"},
		{"nested alias", "concerns:\n  major:\n    level: &level warning\n  minor:\n    level: *level\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "action-version.yml")
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			code := Run(context.Background(), []string{"setup", "--yes", "--force"}, Env{Dir: dir, Stdout: &out, Stderr: &out})
			if code != ExitUsage || !strings.Contains(out.String(), "aliases or anchors") {
				t.Fatalf("exit %d: %s", code, out.String())
			}
			content, err := os.ReadFile(path)
			if err != nil || string(content) != tc.content {
				t.Fatalf("original file changed: %q, %v", content, err)
			}
		})
	}
}
