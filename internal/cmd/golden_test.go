package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/testutil"
)

var updateGoldens = flag.Bool("update", false, "update command output golden files")

func TestGolden(t *testing.T) {
	fixtures := []struct {
		name string
		new  func(testing.TB) *testutil.GitHub
	}{
		{"checkout", testutil.CheckoutFixture},
		{"clean", testutil.CleanFixture},
		{"floating-branches", testutil.FloatingOnBranchesFixture},
		{"duplicate-refs", testutil.DuplicateRefsFixture},
		{"drafts", testutil.DraftsFixture},
	}
	formats := []string{"console", "json", "actions"}
	for _, fixture := range fixtures {
		for _, format := range formats {
			t.Run(fixture.name+"/"+format, func(t *testing.T) {
				gh := fixture.new(t)
				e := newEnv(t, gh)
				args := []string{"verify", "-R", "o/r", "--level=marketplace=none", "--output", format}
				tempDir := t.TempDir()
				var summaryPath string
				if format == "actions" {
					summaryPath = filepath.Join(tempDir, "summary.md")
					e.vars["GITHUB_ACTIONS"] = "true"
					e.vars["GITHUB_STEP_SUMMARY"] = summaryPath
				}
				result := e.run(args...)
				output := fmt.Sprintf("exit code: %d\n--- stdout ---\n%s--- stderr ---\n%s", result.code, result.out, result.err)
				if summaryPath != "" {
					summary, err := os.ReadFile(summaryPath)
					if err != nil {
						t.Fatal(err)
					}
					output += "--- step summary ---\n" + string(summary)
				}
				output = normalizeGolden(output, gh.URL, tempDir)
				path := filepath.Join("testdata", fixture.name+"."+format+".golden")
				if *updateGoldens {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if normalizeGolden(string(want), gh.URL, tempDir) != output {
					t.Errorf("output differs from %s; run go test ./internal/cmd -run '^TestGolden$' -update", path)
				}
			})
		}
	}
}

var (
	goldenMarkerPattern   = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)
	goldenDurationPattern = regexp.MustCompile(`\b\d+(?:\.\d+)?(?:ns|µs|ms|s)\b`)
)

func normalizeGolden(value, host, tempDir string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, host, "<GITHUB_HOST>")
	value = strings.ReplaceAll(value, tempDir, "<TEMP_DIR>")
	value = strings.ReplaceAll(value, filepath.ToSlash(tempDir), "<TEMP_DIR>")
	value = goldenMarkerPattern.ReplaceAllString(value, "<MARKER>")
	value = goldenDurationPattern.ReplaceAllString(value, "<DURATION>")
	return value
}
