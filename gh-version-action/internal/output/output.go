// Package output ports lib/Logging.ps1 and module/CliLogging.ps1: workflow-command-safe output when
// running in GitHub Actions and plain, tty-aware text everywhere else.
package output

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

// Logger writes messages either as GitHub Actions workflow commands or as plain text.
type Logger struct {
	Out io.Writer
	// Actions emits workflow commands (::error::, ##[group], ...) instead of plain text.
	Actions bool
	// Color enables ANSI colors in plain mode.
	Color bool
	// ErrorCount counts errors written without a State (port of $global:returnCode = 1).
	ErrorCount int
}

// New returns a Logger for out. Actions annotations are used only when GITHUB_ACTIONS=true;
// colors only when out is a terminal and NO_COLOR is unset.
func New(out io.Writer) *Logger {
	return &Logger{Out: out, Actions: IsActions(), Color: IsTerminal(out) && os.Getenv("NO_COLOR") == ""}
}

// IsActions reports whether the process runs in GitHub Actions.
func IsActions() bool { return os.Getenv("GITHUB_ACTIONS") == "true" }

// IsTerminal reports whether w is a character device (tty).
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Discard returns a Logger that drops all output.
func Discard() *Logger { return &Logger{Out: io.Discard} }

func (l *Logger) println(s string) { fmt.Fprintln(l.Out, s) }

func (l *Logger) colored(code, s string) string {
	if !l.Color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// escapeData escapes a workflow command message so it cannot span lines or start new commands.
func escapeData(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	return strings.ReplaceAll(s, "\n", "%0A")
}

func escapeProperty(s string) string {
	s = escapeData(s)
	s = strings.ReplaceAll(s, ":", "%3A")
	return strings.ReplaceAll(s, ",", "%2C")
}

// Info writes a plain line.
func (l *Logger) Info(msg string) { l.println(msg) }

// Infof writes a formatted plain line.
func (l *Logger) Infof(format string, args ...any) { l.println(fmt.Sprintf(format, args...)) }

// Debug writes ::debug:: in Actions; locally it always shows "[DEBUG] ...".
func (l *Logger) Debug(msg string) {
	if l.Actions {
		l.println("::debug::" + escapeData(msg))
		return
	}
	l.println(l.colored("36", "[DEBUG] "+msg))
}

// Notice writes a notice annotation.
func (l *Logger) Notice(msg string) { l.annotate("notice", "", msg, "NOTICE: ", "32") }

// Warning writes a warning annotation.
func (l *Logger) Warning(msg string) { l.annotate("warning", "", msg, "WARNING: ", "33") }

// Error writes an error annotation.
func (l *Logger) Error(msg string) { l.annotate("error", "", msg, "ERROR: ", "31") }

// Annotate writes an annotation with a title; level is error, warning or notice.
func (l *Logger) Annotate(level, title, msg string) {
	switch level {
	case "error":
		l.annotate(level, title, msg, "ERROR: ", "31")
	case "warning":
		l.annotate(level, title, msg, "WARNING: ", "33")
	default:
		l.annotate("notice", title, msg, "NOTICE: ", "32")
	}
}

func (l *Logger) annotate(level, title, msg, plainPrefix, color string) {
	if l.Actions {
		head := "::" + level
		if title != "" {
			head += " title=" + escapeProperty(title)
		}
		l.println(head + "::" + escapeData(msg))
		return
	}
	if title != "" {
		msg = title + ": " + msg
	}
	l.println(l.colored(color, plainPrefix+msg))
}

// Safe writes msg wrapped in ::stop-commands:: so untrusted content cannot inject workflow commands.
// The prefix (trusted, may be a workflow command) is written before the stop marker (port of Write-SafeOutput).
func (l *Logger) Safe(msg, prefix string) {
	if !l.Actions {
		l.println(prefix + msg)
		return
	}
	marker := newMarker()
	if prefix != "" {
		fmt.Fprint(l.Out, prefix)
	}
	l.println("::stop-commands::" + marker)
	l.println(msg)
	l.println("::" + marker + "::")
}

// SafeLines writes lines of untrusted text inside a single ::stop-commands:: block.
func (l *Logger) SafeLines(lines []string) {
	if l.Actions {
		marker := newMarker()
		l.println("::stop-commands::" + marker)
		defer l.println("::" + marker + "::")
	}
	for _, s := range lines {
		l.println(s)
	}
}

// SafeInfo writes untrusted text without a prefix.
func (l *Logger) SafeInfo(msg string) { l.Safe(msg, "") }

// Mask hides secret in the Actions log. It is a no-op outside Actions.
func (l *Logger) Mask(secret string) {
	if l.Actions && secret != "" {
		l.println("::add-mask::" + escapeData(secret))
	}
}

// Group starts a collapsible log section.
func (l *Logger) Group(title string) {
	if l.Actions {
		l.println("##[group]" + title)
		return
	}
	l.println(l.colored("1", "== "+title+" =="))
}

// EndGroup ends the section started by Group.
func (l *Logger) EndGroup() {
	if l.Actions {
		l.println("##[endgroup]")
	}
}

// IssueError writes an error and tracks it in st as an error issue; without st it increments ErrorCount
// (port of Write-ActionsError).
func (l *Logger) IssueError(msg string, st *state.State) {
	l.Error(msg)
	if st != nil {
		st.AddIssue(state.NewIssue("error", state.SeverityError, msg))
		return
	}
	l.ErrorCount++
}

// Message writes msg according to severity: "error" (tracked), "warning", or nothing for "none"
// (port of Write-ActionsMessage).
func (l *Logger) Message(msg, severity string, st *state.State) {
	switch severity {
	case state.SeverityError:
		l.IssueError(msg, st)
	case state.SeverityWarning:
		l.Warning(msg)
	}
}

func newMarker() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// AppendStepSummary appends text to the file named by GITHUB_STEP_SUMMARY. It returns false when the
// variable is unset or the file cannot be written.
func AppendStepSummary(text string) bool {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return false
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	defer f.Close()
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	_, err = f.WriteString(text)
	return err == nil
}
