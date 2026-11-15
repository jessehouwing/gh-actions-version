package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jessehouwing/gh-actions-version/internal/state"
)

func TestEditRoundTrip(t *testing.T) {
	input := `# policy comment
preview: exclude # keep preview
ignore-versions: [v0.*, v2]
ignore:
  # explanation
  - versions: [v1, v1.2]
    scope: selection # keep scope
    reason: old
rules:
  major_tag_missing:
    level: warning
`
	doc, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Upsert(state.IgnoreEntry{Versions: []string{"v1"}, Scope: "fix", Reason: "new"}); err != nil {
		t.Fatal(err)
	}
	removed, err := doc.Remove("v0.*")
	if err != nil || !removed {
		t.Fatalf("remove: %v, %v", removed, err)
	}
	result, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"# policy comment", "# keep preview", "# explanation", "# keep scope", "major_tag_missing:", "v1.2", "v2", "reason: new"} {
		if !strings.Contains(string(result), s) {
			t.Errorf("missing %q:\n%s", s, result)
		}
	}
	check, err := Parse(result)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := check.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Versions[0] != "v2" || entries[1].Versions[0] != "v1.2" || entries[2].Scope != "fix" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestUpsertExistingAndInvalid(t *testing.T) {
	doc, err := Parse([]byte("ignore:\n  - versions: [v1] # version comment\n    reason: old # reason comment\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Upsert(state.IgnoreEntry{Versions: []string{"v1"}, Scope: "selection", Reason: "new"}); err != nil {
		t.Fatal(err)
	}
	out, err := doc.Bytes()
	if err != nil || !strings.Contains(string(out), "# version comment") || !strings.Contains(string(out), "# reason comment") {
		t.Fatalf("comments lost: %s (%v)", out, err)
	}
	if err := doc.Upsert(state.IgnoreEntry{Versions: []string{"bad"}}); err == nil {
		t.Fatal("invalid entry accepted")
	}
	for _, input := range []string{"ignore: [bad", "unknown: true", "preview: include\n---\npreview: exclude\n"} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("invalid document accepted: %q", input)
		}
	}
}

func TestFindPath(t *testing.T) {
	dir := t.TempDir()
	path, err := FindPath(dir, "")
	if err != nil || path != filepath.Join(dir, "action-version.yml") {
		t.Fatalf("new path: %q %v", path, err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".github"), 0755); err != nil {
		t.Fatal(err)
	}
	github := filepath.Join(dir, ".github", "action-version.yaml")
	if err := os.WriteFile(github, []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path, err = FindPath(dir, "")
	if err != nil || path != github {
		t.Fatalf("github path: %q %v", path, err)
	}
	root := filepath.Join(dir, "action-version.yml")
	if err := os.WriteFile(root, []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if path, err = FindPath(dir, ""); err != nil || path != root {
		t.Fatalf("root precedence: %q %v", path, err)
	}
	path, err = FindPath(dir, "custom.yml")
	if err != nil || path != filepath.Join(dir, "custom.yml") {
		t.Fatalf("custom path: %q %v", path, err)
	}
}

func TestRemoveAndAliases(t *testing.T) {
	doc, err := Parse([]byte("ignore-versions: [v1, v2]\nignore:\n  - versions: [v1, v3]\n    scope: selection\n"))
	if err != nil {
		t.Fatal(err)
	}
	if removed, err := doc.Remove("v1"); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	entries, err := doc.Entries()
	if err != nil || len(entries) != 2 || entries[0].Versions[0] != "v2" || entries[1].Versions[0] != "v3" {
		t.Fatalf("remaining entries: %+v %v", entries, err)
	}
	if removed, err := doc.Remove("v4"); err != nil || removed {
		t.Fatalf("missing remove: %v %v", removed, err)
	}
	if _, err := Parse([]byte("ignore:\n  - versions: &versions [v1]\n  - versions: *versions\n")); err == nil {
		t.Fatal("aliases accepted")
	}
}
