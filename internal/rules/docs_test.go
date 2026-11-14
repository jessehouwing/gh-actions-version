package rules

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFindingDocsInSync(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate rule package")
	}
	docs := filepath.Join(filepath.Dir(source), "..", "..", "docs", "rules")
	index, err := os.ReadFile(filepath.Join(docs, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range All() {
		page, err := os.ReadFile(filepath.Join(docs, rule.Name+".md"))
		if err != nil {
			t.Errorf("rule %s has no documentation page: %v", rule.Name, err)
			continue
		}
		doc := strings.ReplaceAll(string(page), "\r\n", "\n")
		if !strings.Contains(doc, "## Findings\n") {
			t.Errorf("rule %s page has no Findings section", rule.Name)
		}
		for _, finding := range rule.Findings {
			row := "| `" + finding.ID + "` |"
			if !strings.Contains(doc, row) {
				t.Errorf("finding %s is missing from %s.md", finding.ID, rule.Name)
			}
			if !strings.Contains(string(index), row) {
				t.Errorf("finding %s is missing from the rule index", finding.ID)
			}
		}
	}
}
