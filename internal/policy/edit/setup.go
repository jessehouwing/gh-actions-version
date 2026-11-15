package edit

import (
	"fmt"
	"sort"

	"github.com/jessehouwing/gh-actions-version/internal/policy"
	"gopkg.in/yaml.v3"
)

// ApplySetup updates setup-managed fields while retaining other policy and comments.
func (d *Document) ApplySetup(p *policy.File, clearIgnores bool) error {
	root := d.root.Content[0]
	for _, key := range []string{"version", "floating-versions-use", "preview", "permanently-delete-immutable-tags", "concerns", "rules"} {
		if node := field(root, key); node != nil && containsAliasOrAnchor(node) {
			return fmt.Errorf("%s uses YAML aliases or anchors and cannot be edited safely", key)
		}
	}
	for _, item := range []struct {
		name  string
		value any
	}{
		{"version", p.Version}, {"floating-versions-use", p.FloatingVersionsUse},
		{"preview", p.Preview}, {"permanently-delete-immutable-tags", p.PermanentlyDeleteImmutableTags},
	} {
		if err := set(root, item.name, item.value); err != nil {
			return err
		}
	}
	concerns, err := mapping(root, "concerns")
	if err != nil {
		return err
	}
	for _, name := range sortedSettings(p.Concerns) {
		node, err := mapping(concerns, name)
		if err != nil {
			return err
		}
		if err := applySettings(node, p.Concerns[name]); err != nil {
			return err
		}
	}
	if len(p.Rules) != 0 {
		rules, err := mapping(root, "rules")
		if err != nil {
			return err
		}
		for _, name := range sortedSettings(p.Rules) {
			node, err := mapping(rules, name)
			if err != nil {
				return err
			}
			if err := applySettings(node, p.Rules[name]); err != nil {
				return err
			}
		}
	}
	if clearIgnores {
		for _, name := range []string{"ignore", "ignore-versions"} {
			if field(root, name) != nil {
				sequence(root, name).Content = nil
			}
		}
	}
	for _, entry := range p.Ignore {
		if err := d.Upsert(entry); err != nil {
			return err
		}
	}
	return nil
}

func sortedSettings(settings map[string]policy.Settings) []string {
	names := make([]string, 0, len(settings))
	for name := range settings {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func applySettings(node *yaml.Node, settings policy.Settings) error {
	if settings.Level != "" {
		if err := set(node, "level", settings.Level); err != nil {
			return err
		}
	}
	if settings.Fix != "" {
		if err := set(node, "fix", settings.Fix); err != nil {
			return err
		}
	}
	if len(settings.Findings) != 0 {
		findings, err := mapping(node, "findings")
		if err != nil {
			return err
		}
		for _, name := range sortedSettings(settings.Findings) {
			finding, err := mapping(findings, name)
			if err != nil {
				return err
			}
			if err := applySettings(finding, settings.Findings[name]); err != nil {
				return err
			}
		}
	}
	return nil
}

func mapping(root *yaml.Node, name string) (*yaml.Node, error) {
	node := field(root, name)
	if node == nil {
		node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, node)
	}
	if node.Tag == "!!null" {
		node.Kind, node.Tag, node.Value = yaml.MappingNode, "!!map", ""
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s must be a mapping", name)
	}
	return node, nil
}
