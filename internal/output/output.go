// Package output ports lib/Logging.ps1 and module/CliLogging.ps1: workflow-command-safe output when
// running in GitHub Actions and plain, tty-aware text everywhere else.
package output

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cli/go-gh/v2/pkg/term"
)

// Logger writes messages either as GitHub Actions workflow commands or as plain text.
type Logger struct {
	Out io.Writer
	// DebugOut receives verbose diagnostics when set.
	DebugOut io.Writer
	// Actions emits workflow commands (::error::, ##[group], ...) instead of plain text.
	Actions bool
	// Verbose enables debug messages without relying on GH_DEBUG.
	Verbose bool
	// Quiet suppresses informational and progress messages.
	Quiet bool
	// Color enables ANSI colors in plain mode.
	Color bool
}

// New returns a Logger for out. Actions annotations are used only when GITHUB_ACTIONS=true;
// colors only when out is a terminal (or colors are forced) and NO_COLOR is unset.
func New(out io.Writer) *Logger {
	return &Logger{Out: out, Actions: IsActions(), Color: (IsTerminal(out) || term.IsColorForced()) && !term.IsColorDisabled()}
}

// IsActions reports whether the process runs in GitHub Actions.
func IsActions() bool { return os.Getenv("GITHUB_ACTIONS") == "true" }

// IsTerminal reports whether w is a character device (tty).
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(f)
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
func (l *Logger) Info(msg string) {
	if !l.Quiet {
		l.println(msg)
	}
}

// Infof writes a formatted plain line.
func (l *Logger) Infof(format string, args ...any) {
	if !l.Quiet {
		l.println(fmt.Sprintf(format, args...))
	}
}

// Debug writes ::debug:: in Actions; locally it shows "[DEBUG] ..." when verbose or GH_DEBUG is set.
func (l *Logger) Debug(msg string) {
	if l.Quiet {
		return
	}
	if l.Actions {
		out := l.Out
		if l.Verbose && l.DebugOut != nil {
			out = l.DebugOut
		}
		fmt.Fprintln(out, "::debug::"+escapeData(msg))
		return
	}
	verbose := l.Verbose
	if !verbose {
		v := os.Getenv("GH_DEBUG")
		verbose = v != "" && v != "0" && v != "false"
	}
	if !verbose {
		return
	}
	out := l.Out
	if l.Verbose && l.DebugOut != nil {
		out = l.DebugOut
	}
	fmt.Fprintln(out, l.colored("36", "[DEBUG] "+msg))
}

// Notice writes a notice annotation.
func (l *Logger) Notice(msg string) {
	if !l.Quiet {
		l.annotate("notice", "", msg, "NOTICE: ", "32")
	}
}

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
		if !l.Quiet {
			l.annotate("notice", title, msg, "NOTICE: ", "32")
		}
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

// Mask hides secret in the Actions log. It is a no-op outside Actions.
func (l *Logger) Mask(secret string) {
	if l.Actions && secret != "" {
		l.println("::add-mask::" + escapeData(secret))
	}
}

// Group starts a collapsible log section.
func (l *Logger) Group(title string) {
	if l.Quiet {
		return
	}
	if l.Actions {
		l.println("##[group]" + title)
		return
	}
	l.println(l.colored("1", "== "+title+" =="))
}

// EndGroup ends the section started by Group.
func (l *Logger) EndGroup() {
	if l.Quiet {
		return
	}
	if l.Actions {
		l.println("##[endgroup]")
	}
}

func newMarker() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
