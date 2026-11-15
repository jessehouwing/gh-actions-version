package progress

import (
	"strings"
	"testing"
	"time"
)

func TestPlainStagesHaveNoControlCharacters(t *testing.T) {
	var out strings.Builder
	r := New(Options{Writer: &out})
	r.Message("Rate limited, resuming in 2s")
	stage := r.Start("Loading releases")
	stage.Update("page 2, 140 releases")
	stage.Done("140 releases")

	got := out.String()
	for _, want := range []string{"Rate limited, resuming in 2s", "Loading releases...", "page 2, 140 releases", "Loading releases: 140 releases"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q does not contain %q", got, want)
		}
	}
	if strings.ContainsAny(got, "\r\x1b") {
		t.Errorf("plain output contains terminal control characters: %q", got)
	}
}

func TestActionsStagesAreGroupedAndSanitized(t *testing.T) {
	var out strings.Builder
	r := New(Options{Writer: &out, Actions: true})
	stage := r.Start("Loading releases")
	r.Message("Rate limited\n::error::injected")
	stage.Update("page 1\n::error::injected")
	stage.Done("complete")

	got := out.String()
	for _, want := range []string{"Rate limited%0A::error::injected", "::group::Loading releases", "page 1%0A::error::injected", "Loading releases: complete", "::endgroup::"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q does not contain %q", got, want)
		}
	}
}

func TestQuietStagesWriteNothing(t *testing.T) {
	var out strings.Builder
	r := New(Options{Writer: &out, Quiet: true})
	stage := r.Start("Loading releases")
	stage.Update("page 1")
	time.Sleep(time.Millisecond)
	stage.Done("complete")
	r.Message("rate limit")
	if out.Len() != 0 {
		t.Fatalf("quiet output = %q", out.String())
	}
	if got := r.Timings()["Loading releases"]; got <= 0 {
		t.Fatalf("quiet stage timing = %s, want a positive duration", got)
	}
}

func TestTerminalStageSpinsAndClearsLine(t *testing.T) {
	var out strings.Builder
	r := New(Options{Writer: &out, TTY: true})
	stage := r.Start("Loading releases")
	time.Sleep(150 * time.Millisecond)
	stage.Done("complete")
	if got := out.String(); !strings.Contains(got, "\r| Loading releases") ||
		!strings.Contains(got, "\r\x1b[KLoading releases: complete") {
		t.Fatalf("terminal output does not clear spinner line: %q", got)
	}
}
