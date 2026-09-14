package tree

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// UpdateFrontmatter sets the given keys in a project page and leaves everything
// else as written: other keys, their order, comments, and the body. Values are
// strings or string lists.
func UpdateFrontmatter(path string, fields map[string]any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	front, body, ok := SplitFrontmatter(string(data))
	if !ok {
		return fmt.Errorf("%s: missing frontmatter", path)
	}
	var doc yaml.Node
	if strings.TrimSpace(front) != "" {
		if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: frontmatter is not a mapping", path)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := scalar(fields[key])
		found := false
		for i := 0; i+1 < len(mapping.Content); i += 2 {
			if mapping.Content[i].Value == key {
				mapping.Content[i+1] = value
				found = true
				break
			}
		}
		if !found {
			mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
		}
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	buf.WriteString("---\n")
	if body != "" {
		buf.WriteString("\n" + body)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func scalar(value any) *yaml.Node {
	switch v := value.(type) {
	case []string:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		if len(v) == 0 {
			seq.Style = yaml.FlowStyle
		}
		for _, item := range v {
			seq.Content = append(seq.Content, scalar(item))
		}
		return seq
	case string:
		node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
		// Obsidian writes wikilinks in properties with double quotes; match it.
		if v == "" || strings.HasPrefix(v, "[[") {
			node.Style = yaml.DoubleQuotedStyle
		}
		return node
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: fmt.Sprint(v)}
	}
}

// Move files a project page under another category and returns the new path.
func Move(root string, p *Project, category string) (string, error) {
	category = strings.Trim(filepath.ToSlash(category), "/")
	if hasParentSegment(category) {
		return "", fmt.Errorf("category %q must stay inside the tree", category)
	}
	dir := filepath.Join(root, filepath.FromSlash(category))
	target := filepath.Join(dir, p.ID()+".md")
	if target == p.Path {
		return target, nil
	}
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("a project named %s already exists in %s", p.ID(), categoryOr(category))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(p.Path, target); err != nil {
		return "", err
	}
	return target, nil
}

func categoryOr(category string) string {
	if category == "" {
		return "the top level"
	}
	return category
}

// Unlink deletes a project page. The vault it pointed at is untouched.
func Unlink(p *Project) error {
	return os.Remove(p.Path)
}
