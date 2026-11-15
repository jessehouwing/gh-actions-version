package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/api"
	"github.com/jessehouwing/gh-actions-version/internal/policy"
)

func TestIgnoreCommandOffline(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	env := Env{Dir: dir, Stdout: &out, Stderr: &errOut,
		ResolveRepo: func(string) (api.Repo, error) { t.Fatal("resolved repository"); return api.Repo{}, nil },
		Token:       func(string) (string, string) { t.Fatal("requested token"); return "", "" }}
	run := func(args ...string) int {
		out.Reset()
		errOut.Reset()
		return Run(context.Background(), args, env)
	}
	if code := run("ignore", "v1.2.3", "--scope=selection", "--reason=broken", "--until=2027-01-01", "--rule=major_tag_missing"); code != 0 {
		t.Fatalf("add exit %d: %s", code, errOut.String())
	}
	path := filepath.Join(dir, "action-version.yml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := policy.Parse(content)
	if err != nil || len(cfg.Ignore) != 1 || cfg.Ignore[0].Scope != "selection" || cfg.Ignore[0].Reason != "broken" {
		t.Fatalf("policy: %+v %v", cfg, err)
	}
	if code := run("ignore", "--list"); code != 0 || !strings.Contains(out.String(), "v1.2.3") || !strings.Contains(out.String(), "broken") {
		t.Fatalf("list exit %d: %s %s", code, out.String(), errOut.String())
	}
	if code := run("ignore", "v1.2.3", "--scope=fix"); code != 0 {
		t.Fatalf("update exit %d: %s", code, errOut.String())
	}
	content, _ = os.ReadFile(path)
	cfg, err = policy.Parse(content)
	if err != nil || len(cfg.Ignore) != 1 || cfg.Ignore[0].Scope != "fix" {
		t.Fatalf("update policy: %+v %v", cfg, err)
	}
	if code := run("ignore", "--remove", "v1.2.3"); code != 0 {
		t.Fatalf("remove exit %d: %s", code, errOut.String())
	}
	content, _ = os.ReadFile(path)
	cfg, err = policy.Parse(content)
	if err != nil || len(cfg.Ignore) != 0 {
		t.Fatalf("remove policy: %+v %v", cfg, err)
	}
}

func TestIgnoreCommandErrors(t *testing.T) {
	dir := t.TempDir()
	var output bytes.Buffer
	env := Env{Dir: dir, Stdout: &output, Stderr: &output}
	for _, args := range [][]string{
		{"ignore", "invalid"}, {"ignore", "v1", "--scope=unknown"}, {"ignore", "--list", "v1"},
		{"ignore", "v1", "--until=invalid"}, {"ignore", "--remove", "v1", "--reason=x"},
	} {
		output.Reset()
		if code := Run(context.Background(), args, env); code != ExitUsage {
			t.Errorf("%v: exit %d: %s", args, code, output.String())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "action-version.yml")); !os.IsNotExist(err) {
		t.Fatal("invalid command created a policy")
	}
}

type reasonPrompter struct{ called bool }

func (p *reasonPrompter) Select(string, []string) (int, error) { return 0, nil }
func (p *reasonPrompter) Confirm(string, bool) (bool, error)   { return true, nil }
func (p *reasonPrompter) Input(string, string) (string, error) {
	p.called = true
	return "prompted reason", nil
}

func TestIgnoreCommandExistingFileAndPrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".github"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".github", "action-version.yml")
	if err := os.WriteFile(path, []byte("# keep\npreview: exclude\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	prompt := &reasonPrompter{}
	env := Env{Dir: dir, Stdout: &output, Stderr: &output, IsTTY: true, Prompter: prompt}
	if code := Run(context.Background(), []string{"ignore", "v2.*"}, env); code != 0 || !prompt.called {
		t.Fatalf("exit %d: %s", code, output.String())
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "# keep") || !strings.Contains(string(b), "prompted reason") {
		t.Fatalf("file %s: %v", b, err)
	}
	before := string(b)
	if code := Run(context.Background(), []string{"ignore", "v3", "--until=bad", "--reason=x"}, env); code != ExitUsage {
		t.Fatalf("invalid until exit %d", code)
	}
	b, err = os.ReadFile(path)
	if err != nil || string(b) != before {
		t.Fatal("invalid command changed the policy")
	}
}
