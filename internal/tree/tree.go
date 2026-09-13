// Package tree reads the project tree: directories are categories, markdown files are projects.
package tree

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
)

const (
	ProjectSchema = "atlas.project.v1"
	StateSchema   = "atlas.state.v1"
)

var (
	Priorities = []string{"high", "normal", "low", "someday"}
	States     = []string{"active", "paused", "blocked", "archived"}
)

// Frontmatter is the authored half of a project, edited by hand or in Obsidian.
type Frontmatter struct {
	Schema           string   `yaml:"schema"`
	Name             string   `yaml:"name"`
	Vault            string   `yaml:"vault"`
	Purpose          string   `yaml:"purpose"`
	DefinitionOfDone string   `yaml:"definition_of_done"`
	Priority         string   `yaml:"priority"`
	State            string   `yaml:"state"`
	BlockedOn        string   `yaml:"blocked_on"`
	ReviewAfter      string   `yaml:"review_after"`
	Repos            []string `yaml:"repos"`
	Materials        []string `yaml:"materials"`
}

// Project is one markdown file under the tree root.
type Project struct {
	Path string // the markdown file
	Rel  string // path relative to the tree root without .md, forward slashes
	Body string // markdown after the frontmatter; not interpreted
	Frontmatter
}

// ID is the file name without extension.
func (p *Project) ID() string { return strings.TrimSuffix(filepath.Base(p.Path), ".md") }

// Category is the directory part of Rel, or "" at the top level.
func (p *Project) Category() string {
	if i := strings.LastIndex(p.Rel, "/"); i >= 0 {
		return p.Rel[:i]
	}
	return ""
}

func (p *Project) VaultPath() string { return home.Expand(p.Vault) }

// Problem is a markdown file under the tree that is not a valid project.
type Problem struct {
	Rel    string
	Reason string
}

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify derives a file name from a display name.
func Slugify(name string) (string, error) {
	slug := strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		return "", fmt.Errorf("cannot derive a project id from %q", name)
	}
	return slug, nil
}

// hasParentSegment reports whether a slash path climbs with a ".." segment. A name that
// only contains dots, like "v1..v2", is allowed.
func hasParentSegment(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// SplitFrontmatter separates a leading YAML block from the body.
func SplitFrontmatter(text string) (front, body string, ok bool) {
	if !strings.HasPrefix(text, "---\n") {
		return "", text, false
	}
	rest := text[4:]
	if strings.HasPrefix(rest, "---\n") {
		return "", strings.TrimLeft(rest[4:], "\n"), true
	}
	if rest == "---" {
		return "", "", true
	}
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", text, false
	}
	return rest[:idx], strings.TrimLeft(rest[idx+4:], "\n"), true
}

func relOf(root, path string) string {
	rel, _ := filepath.Rel(root, path)
	return strings.TrimSuffix(filepath.ToSlash(rel), ".md")
}

// Load reads and validates one project file.
func Load(path, root string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	front, body, ok := SplitFrontmatter(string(data))
	if !ok {
		return nil, fmt.Errorf("missing frontmatter")
	}
	p := &Project{Path: path, Rel: relOf(root, path), Body: body}
	if err := yaml.Unmarshal([]byte(front), &p.Frontmatter); err != nil {
		return nil, err
	}
	if p.Schema != ProjectSchema {
		return nil, fmt.Errorf("frontmatter needs `schema: %s`", ProjectSchema)
	}
	if p.Vault == "" {
		return nil, fmt.Errorf("frontmatter needs `vault: <path>`")
	}
	if p.Name == "" {
		p.Name = p.ID()
	}
	if p.Priority == "" {
		p.Priority = "normal"
	}
	if p.State == "" {
		p.State = "active"
	}
	if !contains(Priorities, p.Priority) {
		return nil, fmt.Errorf("priority must be one of %s", strings.Join(Priorities, ", "))
	}
	if !contains(States, p.State) {
		return nil, fmt.Errorf("state must be one of %s", strings.Join(States, ", "))
	}
	return p, nil
}

// Walk lists every project under root, sorted by path. Invalid files are reported, not fatal.
func Walk(root string) ([]*Project, []Problem, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return fs.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	var projects []*Project
	var problems []Problem
	byVault := map[string]*Project{}
	for _, file := range files {
		p, err := Load(file, root)
		if err != nil {
			problems = append(problems, Problem{Rel: relOf(root, file), Reason: err.Error()})
			continue
		}
		key := filepath.Clean(p.VaultPath())
		if other, dup := byVault[key]; dup {
			problems = append(problems, Problem{Rel: p.Rel, Reason: fmt.Sprintf("names the same vault as %s", other.Rel)})
			continue
		}
		byVault[key] = p
		projects = append(projects, p)
	}
	return projects, problems, nil
}

func FindByVault(projects []*Project, vault string) *Project {
	target := filepath.Clean(vault)
	for _, p := range projects {
		if filepath.Clean(p.VaultPath()) == target {
			return p
		}
	}
	return nil
}

func FindByRel(projects []*Project, rel string) *Project {
	rel = strings.Trim(strings.TrimSuffix(rel, ".md"), "/")
	for _, p := range projects {
		if p.Rel == rel || p.ID() == rel {
			return p
		}
	}
	return nil
}

// Render produces the project file text for a frontmatter and body.
func Render(front Frontmatter, body string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(front); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	buf.WriteString("---\n")
	if body != "" {
		buf.WriteString("\n" + strings.TrimRight(body, "\n") + "\n")
	}
	return buf.Bytes(), nil
}

// ProjectOptions are the authored fields set when a project is created.
type ProjectOptions struct {
	ID       string
	Name     string
	Vault    string
	Category string
	Purpose  string
	Priority string
}

// Create writes a new project file and returns its path. Missing category directories are created.
func Create(root string, opts ProjectOptions) (string, error) {
	if opts.Priority == "" {
		opts.Priority = "normal"
	}
	if !contains(Priorities, opts.Priority) {
		return "", fmt.Errorf("priority must be one of %s", strings.Join(Priorities, ", "))
	}
	category := strings.Trim(filepath.ToSlash(opts.Category), "/")
	if hasParentSegment(category) {
		return "", fmt.Errorf("category %q must stay inside the tree", opts.Category)
	}
	dir := filepath.Join(root, filepath.FromSlash(category))
	path := filepath.Join(dir, opts.ID+".md")
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("project %s already exists", relOf(root, path))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	front := Frontmatter{
		Schema:    ProjectSchema,
		Name:      opts.Name,
		Vault:     opts.Vault,
		Purpose:   opts.Purpose,
		Priority:  opts.Priority,
		State:     "active",
		Repos:     []string{},
		Materials: []string{},
	}
	body := "# " + opts.Name + "\n\nNotes that belong to the atlas rather than the vault.\n"
	data, err := Render(front, body)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Unfinished counts work the vault still owes. nil means unknown.
type Unfinished struct {
	EmptySections *int `json:"empty_sections"`
	SeedPages     *int `json:"seed_pages"`
	DeadLinks     *int `json:"dead_links"`
}

// State is the derived half of a project, regenerated in full by refresh.
type State struct {
	Schema      string `json:"schema"`
	GeneratedAt string `json:"generated_at"`
	Project     string `json:"project"`
	Vault       string `json:"vault"`
	VaultOK     bool   `json:"vault_ok"`
	VaultError  string `json:"vault_error"`
	// Legacy marks a claude-obsidian vault that has not been adopted.
	Legacy bool `json:"legacy,omitempty"`
	// PendingRecovery marks a vault with an interrupted operation.
	PendingRecovery bool         `json:"pending_recovery,omitempty"`
	Created         string       `json:"created"`
	LastOperation   string       `json:"last_operation"`
	LastTouched     string       `json:"last_touched"`
	DaysIdle        *int         `json:"days_idle"`
	Heat            string       `json:"heat"`
	Pages           *int         `json:"pages"`
	OpenThreads     []string     `json:"open_threads"`
	Unfinished      Unfinished   `json:"unfinished"`
	Links           []links.Link `json:"links"`
}

// StatePath is where a project's derived state lives: the state dir mirrors the tree.
func StatePath(stateDir, rel string) string {
	return filepath.Join(stateDir, filepath.FromSlash(rel)+".json")
}

func ReadState(stateDir, rel string) (*State, error) {
	data, err := os.ReadFile(StatePath(stateDir, rel))
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func WriteState(stateDir, rel string, state *State) error {
	if state.OpenThreads == nil {
		state.OpenThreads = []string{}
	}
	if state.Links == nil {
		state.Links = []links.Link{}
	}
	path := StatePath(stateDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
