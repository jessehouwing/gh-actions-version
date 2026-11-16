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
	asked           []string
	inputs          []string
	selections      map[string]string
	beforeAsk       func(string)
	fail            bool
	failAt          string
	denyTagDeletion bool
}

func (p *setupTestPrompter) record(message string) {
	p.asked = append(p.asked, message)
	if p.beforeAsk != nil {
		p.beforeAsk(message)
	}
}

func (p *setupTestPrompter) Select(message string, options []string) (int, error) {
	p.record(message)
	if p.fail || message == p.failAt {
		return 0, fmt.Errorf("prompt interrupted")
	}
	if selection, ok := p.selections[message]; ok {
		for i, option := range options {
			if option == selection {
				return i, nil
			}
		}
		return 0, fmt.Errorf("option %q not offered for %q", selection, message)
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
	p.record(message)
	if p.fail {
		return false, fmt.Errorf("prompt interrupted")
	}
	if strings.Contains(message, "irreversible") {
		if def {
			return false, fmt.Errorf("unsafe default")
		}
		return !p.denyTagDeletion, nil
	}
	return def, nil
}

func (p *setupTestPrompter) Input(message, def string) (string, error) {
	p.record(message)
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

func TestSetupExplainsSectionsBeforeQuestions(t *testing.T) {
	var out, errOut bytes.Buffer
	explanations := map[string][]string{
		"Use tags or branches for floating versions?":                             {"Version strategy", "v1.2.3", "Use tags", "Yes enables a check", "does not change repository"},
		"How should preview releases be handled?":                                 {"Preview releases", "Excluded", "Included", "not tag suffixes"},
		"Require releases for patch versions?":                                    {"Releases and publication", "GitHub Releases", "Yes reports", "No skips"},
		"Allow irreversible deletion of tags used by deleted immutable releases?": {"Irreversible tag deletion", "No (the default)", "irreversible"},
		"Version pattern to ignore (blank to finish)":                             {"Ignored versions", "all skips", "selection excludes", "validation skips", "fix prevents"},
		"How should fix policies be configured?":                                  {"Fix policies", "Auto", "Manual", "fine-tune", "does not run fixes now"},
		"major fix policy?":                                                       {"Version fixes", "Major and minor", "patch tags"},
		"releases fix policy?":                                                    {"Release and publication fixes", "Immutability", "Marketplace"},
		"config fix policy?":                                                      {"Policy maintenance fixes", "expired ignore"},
	}
	prompt := &setupTestPrompter{beforeAsk: func(message string) {
		for _, explanation := range explanations[message] {
			if !strings.Contains(errOut.String(), explanation) {
				t.Errorf("missing explanation %q before %q", explanation, message)
			}
		}
	}}
	if code := Run(context.Background(), []string{"setup", "--print"}, Env{Stdout: &out, Stderr: &errOut, IsTTY: true, Prompter: prompt}); code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if _, err := policy.Parse(out.Bytes()); err != nil {
		t.Fatalf("stdout must contain only policy YAML: %v", err)
	}
}

func TestSetupFineTuneFixOrderAndDefaults(t *testing.T) {
	var out, errOut bytes.Buffer
	prompt := &setupTestPrompter{}
	if code := Run(context.Background(), []string{"setup", "--print", "--no-ignore"}, Env{Stdout: &out, Stderr: &errOut, IsTTY: true, Prompter: prompt}); code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var concerns []string
	for _, question := range prompt.asked {
		if concern, ok := strings.CutSuffix(question, " fix policy?"); ok {
			concerns = append(concerns, concern)
		}
	}
	if got, want := strings.Join(concerns, ","), "major,minor,patch,latest,refs,releases,immutability,marketplace,config"; got != want {
		t.Fatalf("fix question order %q, want %q", got, want)
	}
	p, err := policy.Parse(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for concern, settings := range policy.Default().Concerns {
		if p.Concerns[concern].Fix != settings.Fix {
			t.Errorf("%s fix = %q, want default %q", concern, p.Concerns[concern].Fix, settings.Fix)
		}
	}
}

func TestSetupBulkFixPolicies(t *testing.T) {
	for _, mode := range []string{"manual", "auto"} {
		t.Run(mode, func(t *testing.T) {
			explicit := "manual"
			if mode == "manual" {
				explicit = "auto"
			}
			var out, errOut bytes.Buffer
			prompt := &setupTestPrompter{
				selections:      map[string]string{"How should fix policies be configured?": mode},
				denyTagDeletion: true,
			}
			args := []string{"setup", "--print", "--no-ignore", "--level=minor=none", "--fix-policy=major=" + explicit}
			if code := Run(context.Background(), args, Env{Stdout: &out, Stderr: &errOut, IsTTY: true, Prompter: prompt}); code != ExitOK {
				t.Fatalf("exit %d: %s", code, errOut.String())
			}
			p, err := policy.Parse(out.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if p.PermanentlyDeleteImmutableTags != "deny" || p.Concerns["minor"].Level != "none" {
				t.Fatalf("bulk choice changed deletion safeguard or enabled skipped checks: %+v", p)
			}
			for _, concern := range policy.Concerns() {
				want := mode
				if concern == "major" {
					want = explicit
				}
				if got := p.Concerns[concern].Fix; got != want {
					t.Errorf("%s fix = %q, want %q", concern, got, want)
				}
			}
			for _, question := range prompt.asked {
				if strings.HasSuffix(question, " fix policy?") {
					t.Errorf("bulk mode asked individual question %q", question)
				}
			}
		})
	}
}

func TestSetupSkipsFullyAnsweredFixPolicies(t *testing.T) {
	var out, errOut bytes.Buffer
	prompt := &setupTestPrompter{}
	args := []string{"setup", "--print", "--no-ignore"}
	for _, concern := range policy.Concerns() {
		args = append(args, "--fix-policy="+concern+"=manual")
	}
	if code := Run(context.Background(), args, Env{Stdout: &out, Stderr: &errOut, IsTTY: true, Prompter: prompt}); code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, question := range prompt.asked {
		if strings.Contains(question, "fix polic") {
			t.Errorf("asked preanswered question %q", question)
		}
	}
}

func TestSetupFixPolicyPromptFailureDoesNotPrintPolicy(t *testing.T) {
	for _, question := range []string{"How should fix policies be configured?", "minor fix policy?"} {
		t.Run(question, func(t *testing.T) {
			var out, errOut bytes.Buffer
			prompt := &setupTestPrompter{failAt: question}
			if code := Run(context.Background(), []string{"setup", "--print", "--no-ignore"}, Env{Stdout: &out, Stderr: &errOut, IsTTY: true, Prompter: prompt}); code != ExitUsage || out.Len() != 0 || !strings.Contains(errOut.String(), "prompt interrupted") {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
			}
		})
	}
}
