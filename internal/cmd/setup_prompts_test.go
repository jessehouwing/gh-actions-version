package cmd

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/policy"
)

type setupTestPrompter struct {
	asked  []string
	inputs []string
	fail   bool
}

func (p *setupTestPrompter) Select(message string, options []string) (int, error) {
	p.asked = append(p.asked, message)
	if p.fail {
		return 0, fmt.Errorf("prompt interrupted")
	}
	switch {
	case strings.Contains(message, "floating"):
		return 1, nil
	case strings.Contains(message, "preview"):
		return 1, nil
	case strings.Contains(message, "scope"):
		return 1, nil
	default:
		return 0, nil
	}
}

func (p *setupTestPrompter) Confirm(message string, def bool) (bool, error) {
	p.asked = append(p.asked, message)
	if p.fail {
		return false, fmt.Errorf("prompt interrupted")
	}
	if strings.Contains(message, "irreversible") {
		if def {
			return false, fmt.Errorf("unsafe default")
		}
		return true, nil
	}
	return def, nil
}

func (p *setupTestPrompter) Input(message, def string) (string, error) {
	p.asked = append(p.asked, message)
	if p.fail {
		return "", fmt.Errorf("prompt interrupted")
	}
	if len(p.inputs) == 0 {
		return "", nil
	}
	value := p.inputs[0]
	p.inputs = p.inputs[1:]
	return value, nil
}

func TestSetupInteractive(t *testing.T) {
	prompt := &setupTestPrompter{inputs: []string{"v1.2.3", "broken", ""}}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"setup", "--print"}, Env{Stdout: &out, Stderr: &errOut, IsTTY: true, Prompter: prompt}); code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	p, err := policy.Parse(out.Bytes())
	if err != nil || p.FloatingVersionsUse != "branches" || p.Preview != "include" ||
		p.PermanentlyDeleteImmutableTags != "dangerously-allow" || len(p.Ignore) != 1 ||
		p.Ignore[0].Scope != "selection" || p.Ignore[0].Reason != "broken" {
		t.Fatalf("policy %+v, error %v", p, err)
	}
}

func TestSetupSkipsPreansweredPrompts(t *testing.T) {
	prompt := &setupTestPrompter{}
	var out, errOut bytes.Buffer
	args := []string{"setup", "--print", "--floating-versions-use=tags", "--preview-releases=excluded", "--dangerously-allow-destructive-tag-deletion", "--no-ignore", "--level=minor=warning", "--fix-policy=major=manual"}
	if code := Run(context.Background(), args, Env{Stdout: &out, Stderr: &errOut, IsTTY: true, Prompter: prompt}); code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, question := range prompt.asked {
		for _, preanswered := range []string{"floating", "preview", "irreversible", "Maintain minor", "Version pattern", "major fix"} {
			if strings.Contains(question, preanswered) {
				t.Errorf("asked preanswered question %q", question)
			}
		}
	}
	p, err := policy.Parse(out.Bytes())
	if err != nil || p.Concerns["minor"].Level != "warning" || p.Concerns["major"].Fix != "manual" {
		t.Fatalf("policy %+v: %v", p, err)
	}
}

func TestSetupPromptFailureDoesNotPrintPolicy(t *testing.T) {
	var out, errOut bytes.Buffer
	prompt := &setupTestPrompter{fail: true}
	if code := Run(context.Background(), []string{"setup", "--print"}, Env{Stdout: &out, Stderr: &errOut, IsTTY: true, Prompter: prompt}); code != ExitUsage || out.Len() != 0 || !strings.Contains(errOut.String(), "prompt interrupted") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}

func TestSetupYesDoesNotPromptOrAllowDeletion(t *testing.T) {
	var out, errOut bytes.Buffer
	prompt := &setupTestPrompter{fail: true}
	if code := Run(context.Background(), []string{"setup", "--yes", "--print"}, Env{Stdout: &out, Stderr: &errOut, IsTTY: true, Prompter: prompt}); code != ExitOK || len(prompt.asked) != 0 {
		t.Fatalf("exit %d: %s, prompts %v", code, errOut.String(), prompt.asked)
	}
	p, err := policy.Parse(out.Bytes())
	if err != nil || p.PermanentlyDeleteImmutableTags != "deny" {
		t.Fatalf("unsafe default policy: %+v %v", p, err)
	}
}
