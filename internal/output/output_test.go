package output

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func actionsLogger() (*Logger, *bytes.Buffer) {
	var b bytes.Buffer
	return &Logger{Out: &b, Actions: true}, &b
}

func plainLogger() (*Logger, *bytes.Buffer) {
	var b bytes.Buffer
	return &Logger{Out: &b}, &b
}

func TestWriteSafeOutput(t *testing.T) {
	t.Run("Should write message with stop-commands wrapper", func(t *testing.T) {
		l, b := actionsLogger()
		l.Safe("Test message", "")
		out := b.String()
		if !strings.Contains(out, "::stop-commands::") || !strings.Contains(out, "Test message") {
			t.Fatalf("unexpected output %q", out)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		marker := strings.TrimPrefix(lines[0], "::stop-commands::")
		if lines[2] != "::"+marker+"::" {
			t.Fatalf("stop marker not closed: %q", out)
		}
	})
	t.Run("Should include prefix before stop-commands", func(t *testing.T) {
		l, b := actionsLogger()
		l.Safe("Details here", "::error title=Test::")
		out := b.String()
		if !strings.HasPrefix(out, "::error title=Test::::stop-commands::") || !strings.Contains(out, "Details here") {
			t.Fatalf("unexpected output %q", out)
		}
	})
	t.Run("Should neutralize workflow commands in message", func(t *testing.T) {
		l, b := actionsLogger()
		l.Safe("::set-env name=MALICIOUS::value", "")
		lines := strings.Split(b.String(), "\n")
		if !strings.HasPrefix(lines[0], "::stop-commands::") || lines[1] != "::set-env name=MALICIOUS::value" {
			t.Fatalf("message not wrapped: %q", b.String())
		}
	})
	t.Run("Should use a unique marker per call", func(t *testing.T) {
		if newMarker() == newMarker() {
			t.Fatal("markers must differ")
		}
	})
	t.Run("Should not emit stop-commands outside GitHub Actions", func(t *testing.T) {
		l, b := plainLogger()
		l.Safe("hello", "p: ")
		if b.String() != "p: hello\n" {
			t.Fatalf("got %q", b.String())
		}
	})
}

func TestWriteActionsError(t *testing.T) {
	t.Run("Should output the error message", func(t *testing.T) {
		l, b := actionsLogger()
		l.Error("Test error")
		if b.String() != "::error::Test error\n" {
			t.Fatalf("got %q", b.String())
		}
	})
	t.Run("Should track issue in State when provided", func(t *testing.T) {
		l, _ := actionsLogger()
		st := state.New()
		l.IssueError("Test error", st)
		if len(st.Issues) != 1 || st.Issues[0].Severity != "error" {
			t.Fatalf("issues %+v", st.Issues)
		}
	})
	t.Run("Should count the error when State not provided", func(t *testing.T) {
		l, _ := actionsLogger()
		l.IssueError("Test error", nil)
		if l.ErrorCount != 1 {
			t.Fatalf("ErrorCount %d", l.ErrorCount)
		}
	})
	t.Run("Should escape newlines so a message cannot start a new command", func(t *testing.T) {
		l, b := actionsLogger()
		l.Error("a\n::set-env name=X::y")
		if strings.Count(b.String(), "\n") != 1 {
			t.Fatalf("got %q", b.String())
		}
	})
}

func TestWriteActionsWarning(t *testing.T) {
	t.Run("Should output the warning message", func(t *testing.T) {
		l, b := actionsLogger()
		l.Warning("Test warning")
		if b.String() != "::warning::Test warning\n" {
			t.Fatalf("got %q", b.String())
		}
	})
	t.Run("Should format warning as plain text outside Actions", func(t *testing.T) {
		l, b := plainLogger()
		l.Warning("Test warning")
		if b.String() != "WARNING: Test warning\n" {
			t.Fatalf("got %q", b.String())
		}
	})
}

func TestWriteActionsMessage(t *testing.T) {
	t.Run("Should call Write-ActionsError for severity 'error'", func(t *testing.T) {
		l, _ := plainLogger()
		l.Message("Test", "error", nil)
		if l.ErrorCount != 1 {
			t.Fatal("expected error counted")
		}
	})
	t.Run("Should call Write-ActionsWarning for severity 'warning'", func(t *testing.T) {
		l, b := actionsLogger()
		l.Message("Test", "warning", nil)
		if b.String() != "::warning::Test\n" {
			t.Fatalf("got %q", b.String())
		}
	})
	t.Run("Should not output anything for severity 'none'", func(t *testing.T) {
		l, b := actionsLogger()
		l.Message("Should not appear", "none", nil)
		if b.Len() != 0 {
			t.Fatalf("got %q", b.String())
		}
	})
	t.Run("Should accept State parameter and track issues", func(t *testing.T) {
		l, _ := plainLogger()
		st := state.New()
		l.Message("Test", "error", st)
		if len(st.Issues) != 1 {
			t.Fatal("expected one issue")
		}
	})
}

func TestAnnotationsAndGroups(t *testing.T) {
	t.Run("Should write debug, notice, titled annotations and mask in Actions", func(t *testing.T) {
		l, b := actionsLogger()
		l.Debug("d")
		l.Notice("n")
		l.Annotate("error", "Unfixable issue", "m")
		l.Mask("s3cret")
		l.Group("g")
		l.EndGroup()
		want := "::debug::d\n::notice::n\n::error title=Unfixable issue::m\n::add-mask::s3cret\n##[group]g\n##[endgroup]\n"
		if b.String() != want {
			t.Fatalf("got %q want %q", b.String(), want)
		}
	})
	t.Run("Should write plain tty-aware text outside Actions", func(t *testing.T) {
		t.Setenv("GH_DEBUG", "1")
		l, b := plainLogger()
		l.Debug("d")
		l.Notice("n")
		l.Annotate("warning", "T", "m")
		l.Mask("s3cret")
		l.Group("g")
		l.EndGroup()
		want := "[DEBUG] d\nNOTICE: n\nWARNING: T: m\n== g ==\n"
		if b.String() != want {
			t.Fatalf("got %q want %q", b.String(), want)
		}
	})
	t.Run("Should color only when enabled", func(t *testing.T) {
		l, b := plainLogger()
		l.Color = true
		l.Error("x")
		if !strings.Contains(b.String(), "\x1b[31m") {
			t.Fatalf("got %q", b.String())
		}
	})
	t.Run("Should enable Actions only when GITHUB_ACTIONS is true", func(t *testing.T) {
		t.Setenv("GITHUB_ACTIONS", "true")
		if !New(&bytes.Buffer{}).Actions {
			t.Fatal("expected actions")
		}
		t.Setenv("GITHUB_ACTIONS", "")
		if New(&bytes.Buffer{}).Actions {
			t.Fatal("expected plain")
		}
	})
	t.Run("Should honour NO_COLOR and CLICOLOR_FORCE", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		t.Setenv("CLICOLOR_FORCE", "")
		if New(&bytes.Buffer{}).Color {
			t.Fatal("non-terminal output must not be colored")
		}
		t.Setenv("CLICOLOR_FORCE", "1")
		if !New(&bytes.Buffer{}).Color {
			t.Fatal("CLICOLOR_FORCE should force color")
		}
		t.Setenv("NO_COLOR", "1")
		if New(&bytes.Buffer{}).Color {
			t.Fatal("NO_COLOR should win")
		}
	})
}

func TestAppendStepSummary(t *testing.T) {
	t.Run("Should do nothing without GITHUB_STEP_SUMMARY", func(t *testing.T) {
		t.Setenv("GITHUB_STEP_SUMMARY", "")
		if AppendStepSummary("x") {
			t.Fatal("expected false")
		}
	})
	t.Run("Should append to the summary file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "summary.md")
		t.Setenv("GITHUB_STEP_SUMMARY", p)
		AppendStepSummary("one")
		AppendStepSummary("two\n")
		got, _ := os.ReadFile(p)
		if string(got) != "one\ntwo\n" {
			t.Fatalf("got %q", got)
		}
	})
}
