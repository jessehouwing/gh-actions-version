// Package remediation ports lib/Remediation.ps1: running fix actions (auto or interactive), manual
// instructions, unresolved-issue reporting and the final summary/exit code from main.ps1.
package remediation

import (
	"context"
	"fmt"
	"sort"

	"github.com/jessehouwing/gh-actions-version/internal/output"
	"github.com/jessehouwing/gh-actions-version/internal/progress"
	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// Mode selects how fixable issues are handled.
type Mode int

const (
	// ReportOnly makes no writes; pending issues become manual_fix_required.
	ReportOnly Mode = iota
	// AutoFix applies every fixable remediation in priority order without prompting.
	AutoFix
	// Interactive prompts for every fixable issue.
	Interactive
	// Manual prints remediation instructions without applying fixes.
	Manual
	// DryRun reports the actions auto mode would apply without executing them.
	DryRun
)

func (m Mode) String() string {
	switch m {
	case AutoFix:
		return "auto-fix"
	case Interactive:
		return "interactive"
	case Manual:
		return "manual"
	case DryRun:
		return "dry-run"
	}
	return "report-only"
}

// Prompter asks the user to pick one of the options and returns its index.
type Prompter interface {
	Select(message string, options []string) (int, error)
}

// Interactive prompt options, in the order they are offered.
const (
	OptApply    = "Apply"
	OptSkip     = "Skip"
	OptApplyAll = "Apply all remaining"
	OptSkipAll  = "Skip all remaining"
	OptQuit     = "Quit"
)

var promptOptions = []string{OptApply, OptSkip, OptApplyAll, OptSkipAll, OptQuit}

// Options configures Fix.
type Options struct {
	Mode     Mode
	Client   state.Client
	Log      *output.Logger
	Prompter Prompter // required for Interactive
	Progress progress.Stage
}

func (o Options) log() *output.Logger {
	if o.Log == nil {
		return output.Discard()
	}
	return o.Log
}

// Fix handles the pending issues according to opt.Mode (port of Invoke-AutoFix plus the interactive runner).
// In Interactive mode skipped issues stay pending.
func Fix(ctx context.Context, st *state.State, opt Options) error {
	for _, i := range st.Issues {
		if i.Status == state.StatusPending && i.ManualOnly {
			i.Status = state.StatusManualFixRequired
		}
	}
	switch opt.Mode {
	case AutoFix:
		autoFix(ctx, st, opt)
		return nil
	case Interactive:
		return interactive(ctx, st, opt)
	case DryRun:
		issues := pendingFixable(st)
		for n, i := range issues {
			if opt.Progress != nil {
				opt.Progress.Update(fmt.Sprintf("[%d/%d] %s %s", n+1, len(issues), i.Finding, i.Version))
			}
			opt.log().Infof("Would apply: %s (%s)", i.Action.Description(), i.Action.Version())
		}
	}
	for _, i := range st.Issues {
		if i.Status == state.StatusPending {
			i.Status = state.StatusManualFixRequired
		}
	}
	return nil
}

func pendingFixable(st *state.State) []*state.Issue {
	var out []*state.Issue
	for _, i := range st.Issues {
		if i.Status == state.StatusPending && i.Action != nil {
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Action.Priority() < out[b].Action.Priority() })
	return out
}

func markUnfixable(st *state.State, onlyWithoutAction bool) {
	for _, i := range st.Issues {
		if i.Status == state.StatusPending && (!onlyWithoutAction || i.Action == nil) {
			i.Status = state.StatusUnfixable
		}
	}
}

func autoFix(ctx context.Context, st *state.State, opt Options) {
	log := opt.log()
	if len(st.Issues) > 0 {
		log.Group("Verifying potential solutions")
		defer log.EndGroup()
	}
	issues := pendingFixable(st)
	for n, i := range issues {
		if opt.Progress != nil {
			opt.Progress.Update(fmt.Sprintf("[%d/%d] %s %s", n+1, len(issues), i.Finding, i.Version))
		}
		execute(ctx, st, i, opt)
	}
	markUnfixable(st, false)
}

// execute runs the issue's action and updates its status. An action that already set another status keeps it.
func execute(ctx context.Context, st *state.State, i *state.Issue, opt Options) {
	log := opt.log()
	ok := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Infof("✗ Failed: %s", i.Action.Description())
				log.Safe(fmt.Sprint(r), "::debug::Exception during auto-fix: ")
			}
		}()
		ok = i.Action.Execute(ctx, st, opt.Client)
	}()
	switch {
	case ok:
		i.Status = state.StatusFixed
	case i.Status == state.StatusPending:
		i.Status = state.StatusFailed
	}
}

func interactive(ctx context.Context, st *state.State, opt Options) error {
	if opt.Prompter == nil {
		return fmt.Errorf("interactive mode requires a prompter")
	}
	log := opt.log()
	defer markUnfixable(st, true)
	issues := pendingFixable(st)
	applyAll := false
	for n, i := range issues {
		if opt.Progress != nil {
			opt.Progress.Update(fmt.Sprintf("[%d/%d] %s %s", n+1, len(issues), i.Finding, i.Version))
		}
		if !applyAll {
			log.Info("")
			log.Infof("[%d/%d] %s", n+1, len(issues), i.String())
			log.Infof("  Action: %s (%s)", i.Action.Description(), i.Action.Version())
			for _, c := range i.Action.ManualCommands(st) {
				log.Infof("    %s", c)
			}
			idx, err := opt.Prompter.Select("Apply this fix?", promptOptions)
			if err != nil {
				return err
			}
			if idx < 0 || idx >= len(promptOptions) {
				return fmt.Errorf("invalid prompt selection %d", idx)
			}
			switch promptOptions[idx] {
			case OptSkip:
				continue
			case OptSkipAll, OptQuit:
				return nil
			case OptApplyAll:
				applyAll = true
			}
		}
		execute(ctx, st, i, opt)
	}
	return nil
}
