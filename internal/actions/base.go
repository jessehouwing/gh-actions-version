// Package actions holds the remediation actions (port of lib/actions/**).
// Constructors are the contract used by the rules; Execute/ManualCommands are implemented per action file.
package actions

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

// Base carries the shared RemediationAction fields.
type Base struct {
	Desc     string
	Ver      string
	Prio     int
	Language string // manual command fence language, default "bash"
}

func (b *Base) Description() string { return b.Desc }
func (b *Base) Version() string     { return b.Ver }
func (b *Base) Priority() int       { return b.Prio }
func (b *Base) ManualCommandsLanguage() string {
	if b.Language == "" {
		return "bash"
	}
	return b.Language
}

func newBase(desc, version string, prio int) Base { return Base{Desc: desc, Ver: version, Prio: prio} }

// issueFor finds the issue owned by action a so Execute can update its status/message.
func issueFor(st *state.State, a state.Action) *state.Issue {
	for _, i := range st.Issues {
		if i.Action == a {
			return i
		}
	}
	return nil
}

// Out receives the human readable progress output of the actions.
var Out io.Writer = os.Stdout

func outf(format string, args ...any) { fmt.Fprintf(Out, format+"\n", args...) }

// markManualFix flags the issue owned by a as needing manual intervention.
func markManualFix(st *state.State, a state.Action, msg string) {
	if i := issueFor(st, a); i != nil {
		i.Status = state.StatusManualFixRequired
		i.Message = msg
	}
}

// failRef reports a failed ref write and applies the manual-fix status when the 'workflows' permission is missing.
// verb is "create" or "update", noun is "tag" or "branch", doneMsg the past-tense instruction ("created", "updated").
func failRef(st *state.State, a state.Action, res state.RefResult, verb, noun, name, issueMsg string) {
	if res.RequiresManualFix {
		outf("✗ Manual fix required: Cannot %s %s %s - requires 'workflows' permission to modify workflow files", verb, noun, name)
		markManualFix(st, a, issueMsg)
		return
	}
	outf("✗ Failed: %s %s %s", strings.ToUpper(verb[:1])+verb[1:], noun, name)
}

func repoArg(st *state.State) string {
	if st.Owner != "" && st.Repo != "" {
		return " --repo " + st.Owner + "/" + st.Repo
	}
	return ""
}

func latestArg(makeLatest *bool) string {
	if makeLatest == nil {
		return ""
	}
	if *makeLatest {
		return " --latest"
	}
	return " --latest=false"
}

// ReleaseBase is the port of ReleaseRemediationAction.
type ReleaseBase struct {
	Base
	TagName string
}

func newReleaseBase(desc, tag string, prio int) ReleaseBase {
	return ReleaseBase{Base: newBase(desc, tag, prio), TagName: tag}
}

func (b *ReleaseBase) findIssue(st *state.State, issueType string) *state.Issue {
	for _, i := range st.Issues {
		if i.Version == b.TagName && i.Type == issueType {
			return i
		}
	}
	return nil
}

// suggestedIgnoreVersions is the configured ignore-versions list plus this tag, deduplicated.
func (b *ReleaseBase) suggestedIgnoreVersions(st *state.State) string {
	seen := map[string]bool{}
	var out []string
	for _, v := range append(append([]string{}, st.Config.IgnoreVersions...), b.TagName) {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return strings.Join(out, ",")
}

// ignoreVersionsYamlSnippet switches the manual command language to yaml.
func (b *ReleaseBase) ignoreVersionsYamlSnippet(st *state.State) []string {
	b.Language = "yaml"
	return []string{
		"# Update your workflow step to skip this locked version:",
		"with:",
		fmt.Sprintf("  ignore-versions: %q", b.suggestedIgnoreVersions(st)),
	}
}

func (b *ReleaseBase) markAsUnfixable(st *state.State, issueType, msg string) {
	outf("✗ Unfixable: %s", msg)
	if i := b.findIssue(st, issueType); i != nil {
		i.Status = state.StatusUnfixable
		i.Message = msg
	}
}

func (b *ReleaseBase) markAsManualFixRequired(st *state.State, issueType, msg string) {
	outf("⚠ Manual fix required: %s", msg)
	if i := b.findIssue(st, issueType); i != nil {
		i.Status = state.StatusManualFixRequired
		i.Message = msg
	}
}

func (b *ReleaseBase) isIssueUnfixable(st *state.State, issueType string) bool {
	i := b.findIssue(st, issueType)
	return i != nil && i.Status == state.StatusUnfixable
}

// releaseID returns id, or the ID of the release in state for the tag when id is 0.
func (b *ReleaseBase) releaseID(st *state.State, id int64) int64 {
	if id != 0 {
		return id
	}
	if r := st.FindRelease(b.TagName); r != nil {
		return r.ID
	}
	return 0
}
