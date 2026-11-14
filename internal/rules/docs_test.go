package rules

import (
	"os"
	"path/filepath"
	"regexp"
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
		configStart := strings.Index(doc, "## Configuration\n")
		if configStart < 0 {
			t.Errorf("rule %s page has no Configuration section", rule.Name)
			continue
		}
		config := doc[configStart:]
		yamlStart := strings.Index(config, "```yaml\n")
		if yamlStart < 0 {
			t.Errorf("rule %s configuration has no YAML example", rule.Name)
			continue
		}
		yamlBlock := config[yamlStart+len("```yaml\n"):]
		yamlEnd := strings.Index(yamlBlock, "\n```")
		if yamlEnd < 0 {
			t.Errorf("rule %s configuration has an unterminated YAML example", rule.Name)
			continue
		}
		yamlBlock = yamlBlock[:yamlEnd]
		if !strings.Contains(yamlBlock, "rules:\n  "+rule.Name+":") {
			t.Errorf("rule %s configuration YAML does not name the rule", rule.Name)
		}
		for _, finding := range rule.Findings {
			if !strings.Contains(yamlBlock, "      "+finding.ID+":") {
				t.Errorf("finding %s is missing from %s configuration YAML", finding.ID, rule.Name)
			}
		}
	}
}

func TestDocsDoNotMentionRemovedCLIFlags(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate rule package")
	}
	root := filepath.Join(filepath.Dir(source), "..", "..")
	paths := []string{filepath.Join(root, "README.md")}
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	removedFlag := regexp.MustCompile(`--check($|[^A-Za-z0-9_-])|--json($|[^A-Za-z0-9_-])`)
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		text := string(content)
		for _, flag := range []string{"--check-", "--auto-fix", "--no-fix"} {
			if strings.Contains(text, flag) {
				t.Errorf("%s documents removed flag %s", path, flag)
			}
		}
		if match := removedFlag.FindString(text); match != "" {
			t.Errorf("%s documents removed flag %s", path, match)
		}
	}
}
