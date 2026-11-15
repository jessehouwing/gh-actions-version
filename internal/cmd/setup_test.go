package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/api"
	"github.com/jessehouwing/gh-actions-version/internal/policy"
)

func TestSetupPrintPolicy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		check func(*testing.T, *policy.File)
	}{
		{"defaults", []string{"setup", "--yes", "--print"}, func(t *testing.T, p *policy.File) {
			if p.FloatingVersionsUse != "tags" || p.Preview != "exclude" || p.PermanentlyDeleteImmutableTags != "deny" || p.Concerns["minor"].Level != "error" {
				t.Fatalf("unexpected defaults: %+v", p)
			}
		}},
		{"overrides", []string{"setup", "--yes", "--print", "--floating-versions-use=branches", "--preview-releases=included", "--dangerously-allow-destructive-tag-deletion", "--level=minor=none", "--level=releases=warning", "--fix-policy=releases=manual", "--rule-level=major_tag_missing=warning", "--finding-level=major_tag_missing=none", "--ignore-version=v2.*"}, func(t *testing.T, p *policy.File) {
			if p.FloatingVersionsUse != "branches" || p.Preview != "include" || p.PermanentlyDeleteImmutableTags != "dangerously-allow" || p.Concerns["minor"].Level != "none" || p.Concerns["releases"].Fix != "manual" ||
				p.Rules["major_tag_missing"].Findings["major_tag_missing"].Level != "none" || len(p.Ignore) != 1 || p.Ignore[0].Versions[0] != "v2.*" {
				t.Fatalf("unexpected policy: %+v", p)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			env := Env{Stdout: &out, Stderr: &errOut,
				ResolveRepo: func(string) (api.Repo, error) { t.Fatal("resolved repository"); return api.Repo{}, nil },
				Token:       func(string) (string, string) { t.Fatal("requested token"); return "", "" }}
			if code := Run(context.Background(), tc.args, env); code != ExitOK {
				t.Fatalf("exit %d: %s", code, errOut.String())
			}
			p, err := policy.Parse(out.Bytes())
			if err != nil {
				t.Fatalf("invalid policy: %s: %v", out.String(), err)
			}
			tc.check(t, p)
		})
	}
}

func TestSetupRejectsInvalidOrUnanswered(t *testing.T) {
	for _, args := range [][]string{
		{"setup", "--print"},
		{"setup", "--yes", "--print", "--floating-versions-use=both"},
		{"setup", "--yes", "--print", "--preview-releases=include"},
		{"setup", "--yes", "--print", "--ignore-version=bad"},
		{"setup", "--yes", "--print", "--fix-policy=unknown=manual"},
		{"setup", "--yes", "--print", "--fix-policy=minor=invalid"},
	} {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, Env{Stdout: &out, Stderr: &errOut}); code != ExitUsage || out.Len() != 0 || !strings.Contains(errOut.String(), "Error:") {
			t.Errorf("%v: exit %d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

func TestSetupAllAnswersWithoutYes(t *testing.T) {
	args := []string{"setup", "--print", "--floating-versions-use=tags", "--preview-releases=excluded", "--no-ignore"}
	for _, concern := range []string{"minor", "releases", "immutability", "marketplace"} {
		args = append(args, "--level="+concern+"=none")
	}
	for _, concern := range policy.Concerns() {
		args = append(args, "--fix-policy="+concern+"=manual")
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), args, Env{Stdout: &out, Stderr: &errOut}); code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	p, err := policy.Parse(out.Bytes())
	if err != nil || p.Concerns["releases"].Level != "none" || p.Concerns["major"].Fix != "manual" {
		t.Fatalf("policy %+v: %v", p, err)
	}
}
