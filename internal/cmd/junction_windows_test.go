package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalCommandsThroughJunction(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	repo := filepath.Join(real, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "fixture")
	git("tag", "v1.0.0")
	link := filepath.Join(dir, "junction")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, out)
	}
	linkedRepo := filepath.Join(link, "repo")
	e := newEnv(t, nil)
	e.dir = linkedRepo
	if r := e.runRaw("setup", "--yes"); r.code != ExitOK {
		t.Fatalf("setup: exit %d: %s", r.code, r.all())
	}
	e = newEnv(t, nil)
	e.dir = linkedRepo
	if r := e.runRaw("ignore", "v2", "--reason=junction"); r.code != ExitOK {
		t.Fatalf("ignore: exit %d: %s", r.code, r.all())
	}
	data, err := os.ReadFile(filepath.Join(repo, "action-version.yml"))
	if err != nil || !strings.Contains(string(data), "junction") {
		t.Fatalf("policy not updated: %s %v", data, err)
	}
	e = newEnv(t, nil)
	e.dir = linkedRepo
	r := e.runRaw("verify", "--local", "--rule=major_tag_missing")
	if r.code != ExitValidation || !strings.Contains(r.all(), "major_tag_missing") || e.tokenCalls != 0 {
		t.Fatalf("local verify: exit %d tokens=%d: %s", r.code, e.tokenCalls, r.all())
	}
}
