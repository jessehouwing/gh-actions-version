// Package edit modifies local policy YAML while retaining comments and ordering.
package edit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jessehouwing/gh-actions-version/internal/policy"
	"github.com/jessehouwing/gh-actions-version/internal/state"
	"github.com/jessehouwing/gh-actions-version/internal/version"
	"gopkg.in/yaml.v3"
)

type Document struct {
	root *yaml.Node
}

// FindPath follows the same search order as policy.Load. An explicit path overrides the search.
func FindPath(dir, override string) (string, error) {
	if override != "" {
		if filepath.IsAbs(override) {
			return override, nil
		}
		return filepath.Join(dir, override), nil
	}
	for _, subdir := range policy.SearchDirs {
		for _, name := range policy.FileNames {
			path := filepath.Join(dir, subdir, name)
			info, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return "", err
			}
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("%s is not a regular file", path)
			}
			return path, nil
		}
	}
	return filepath.Join(dir, policy.FileNames[0]), nil
}

// Parse rejects invalid policy before editing it.
func Parse(data []byte) (*Document, error) {
	if _, err := policy.Parse(data); err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil && err != io.EOF {
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("multiple YAML documents are not supported")
	}
	if len(doc.Content) == 0 {
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	root := doc.Content[0]
	if root.Tag == "!!null" {
		root.Kind, root.Tag, root.Value = yaml.MappingNode, "!!map", ""
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("policy must be a YAML mapping")
	}
	for _, key := range []string{"ignore", "ignore-versions"} {
		if node := field(root, key); node != nil && containsAliasOrAnchor(node) {
			return nil, fmt.Errorf("%s uses YAML aliases or anchors and cannot be edited safely", key)
		}
	}
	return &Document{root: &doc}, nil
}

func containsAliasOrAnchor(n *yaml.Node) bool {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return true
	}
	for _, child := range n.Content {
		if containsAliasOrAnchor(child) {
			return true
		}
	}
	return false
}

// Entries returns both simple and structured ignores in policy order.
func (d *Document) Entries() ([]state.IgnoreEntry, error) {
	b, err := d.Bytes()
	if err != nil {
		return nil, err
	}
	p, err := policy.Parse(b)
	if err != nil {
		return nil, err
	}
	entries := make([]state.IgnoreEntry, 0, len(p.IgnoreVersions)+len(p.Ignore))
	for _, pattern := range p.IgnoreVersions {
		entries = append(entries, state.IgnoreEntry{Versions: []string{pattern}, Scope: "all"})
	}
	return append(entries, p.Ignore...), nil
}

// Upsert adds or replaces the policy for one pattern.
func (d *Document) Upsert(entry state.IgnoreEntry) error {
	if len(entry.Versions) != 1 {
		return fmt.Errorf("an edit must specify exactly one version pattern")
	}
	var check yaml.Node
	if err := check.Encode(policy.File{Ignore: []state.IgnoreEntry{entry}}); err != nil {
		return err
	}
	raw, err := yaml.Marshal(&check)
	if err != nil {
		return err
	}
	if _, err := policy.Parse(raw); err != nil {
		return err
	}
	pattern := entry.Versions[0]
	root := d.root.Content[0]
	var existing *yaml.Node
	if list := field(root, "ignore"); list != nil && list.Kind == yaml.SequenceNode {
		for _, item := range list.Content {
			versions := field(item, "versions")
			if versions != nil && len(versions.Content) == 1 && versions.Content[0].Value == pattern {
				existing = item
				break
			}
		}
	}
	if _, err := d.remove(pattern, existing); err != nil {
		return err
	}
	if existing == nil {
		existing = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		list := sequence(root, "ignore")
		list.Content = append(list.Content, existing)
	}
	for _, field := range []struct {
		key   string
		value any
	}{
		{"versions", entry.Versions}, {"scope", entry.Scope}, {"reason", entry.Reason},
		{"rules", entry.Rules}, {"until", entry.Until},
	} {
		if err := set(existing, field.key, field.value); err != nil {
			return err
		}
	}
	return nil
}

// Remove removes a pattern from both simple and structured entries.
func (d *Document) Remove(pattern string) (bool, error) {
	if !version.IsValidPattern(pattern) {
		return false, fmt.Errorf("invalid version pattern %q", pattern)
	}
	return d.remove(pattern, nil)
}

func (d *Document) remove(pattern string, keep *yaml.Node) (bool, error) {
	root := d.root.Content[0]
	removed := false
	if list := field(root, "ignore-versions"); list != nil && list.Kind == yaml.SequenceNode {
		removed = removeScalar(list, pattern) || removed
	}
	if list := field(root, "ignore"); list != nil && list.Kind == yaml.SequenceNode {
		items := list.Content[:0]
		for _, item := range list.Content {
			if item == keep {
				items = append(items, item)
				continue
			}
			versions := field(item, "versions")
			if versions != nil && removeScalar(versions, pattern) {
				removed = true
				if len(versions.Content) == 0 {
					continue
				}
			}
			items = append(items, item)
		}
		list.Content = items
	}
	return removed, nil
}

func removeScalar(list *yaml.Node, value string) bool {
	var removed bool
	items := list.Content[:0]
	for _, item := range list.Content {
		if item.Value == value {
			removed = true
		} else {
			items = append(items, item)
		}
	}
	list.Content = items
	return removed
}

func field(mapping *yaml.Node, name string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == name {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func sequence(root *yaml.Node, name string) *yaml.Node {
	if n := field(root, name); n != nil {
		if n.Tag == "!!null" {
			n.Kind, n.Tag, n.Value = yaml.SequenceNode, "!!seq", ""
		}
		return n
	}
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, n)
	return n
}

func set(mapping *yaml.Node, key string, value any) error {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return err
	}
	empty := node.Tag == "!!null" || node.Kind == yaml.ScalarNode && node.Value == "" || node.Kind == yaml.SequenceNode && len(node.Content) == 0
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != key {
			continue
		}
		old := mapping.Content[i+1]
		if empty {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
		} else {
			node.HeadComment, node.LineComment, node.FootComment = old.HeadComment, old.LineComment, old.FootComment
			if old.Kind == node.Kind {
				node.Style = old.Style
			}
			mapping.Content[i+1] = &node
		}
		return nil
	}
	if !empty {
		mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &node)
	}
	return nil
}

// Bytes encodes the edited document and verifies the resulting policy.
func (d *Document) Bytes() ([]byte, error) {
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(d.root); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	if _, err := policy.Parse(out.Bytes()); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
