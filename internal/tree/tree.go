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
	"github.com/nathanaday/claude-atlas/internal/tasks"
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
	Related          []string `yaml:"related"`
}

// Linked is one entry of a project's repos or materials list, resolved against the link
// pages beside the tree. Name is empty for a plain path with no page yet; Path is empty
// for a wikilink that names no page.
type Linked struct {
	Kind  string `json:"kind"`
	Entry string `json:"entry"`
	Name  string `json:"name,omitempty"`
	Path  string `json:"path,omitempty"`
}

// Project is one markdown file under the tree root.
type Project struct {
	Path string // the markdown file
	Rel  string // path relative to the tree root without .md, forward slashes
	Body string // markdown after the frontmatter; not interpreted
	Frontmatter
	// Linked resolves Repos and Materials; RelatedTo resolves Related to project rels;
	// Warnings lists entries that name nothing. Walk fills all three, Load the first.
	Linked    []Linked
	RelatedTo []string
	Warnings  []string
}

// ID is the file name without extension.
func (p *Project) ID() string { return strings.TrimSuffix(filepath.Base(p.Path), ".md") }

// Wikilink is how another atlas page refers to this project.
func (p *Project) Wikilink() string { return "[[tree/" + p.Rel + "|" + p.Name + "]]" }

// Paths lists the linked folders of a kind that resolve to a path.
func (p *Project) Paths(kind string) []string {
	var out []string
	for _, l := range p.Linked {
		if l.Kind == kind && l.Path != "" {
			out = append(out, l.Path)
		}
	}
	return out
}

// LinkedTo reports whether a folder is linked to the project, whatever its kind.
func (p *Project) LinkedTo(path string) bool {
	target := filepath.Clean(home.Expand(path))
	for _, l := range p.Linked {
		if l.Path != "" && filepath.Clean(l.Path) == target {
			return true
		}
	}
	return false
}

// Atlas is the vault the tree sits in; link pages live beside the tree.
func Atlas(root string) string { return filepath.Dir(root) }

// ResolveLinks fills Linked from the repos and materials lists against the given pages.
func ResolveLinks(p *Project, pages []links.Page) {
	p.Linked = nil
	for _, kind := range []string{links.Repo, links.Materials} {
		entries := p.Repos
		if kind == links.Materials {
			entries = p.Materials
		}
		for _, entry := range entries {
			l := Linked{Kind: kind, Entry: entry}
			page, isLink := links.Resolve(pages, kind, entry)
			switch {
			case page != nil:
				l.Name, l.Path = page.Name, page.Path
			case isLink:
				target, _, _ := links.ParseWikilink(entry)
				l.Name = strings.TrimSuffix(filepath.Base(target), ".md")
				p.Warnings = append(p.Warnings, fmt.Sprintf("%s: %s names no page under %s/", kind, entry, links.Dir(kind)))
			default:
				abs, err := filepath.Abs(home.Expand(entry))
				if err != nil {
					abs = home.Expand(entry)
				}
				l.Path = abs
			}
			p.Linked = append(p.Linked, l)
		}
	}
}

// relatedTarget reads a related entry: "[[tree/a/b|B]]", "[[b]]", or a bare rel or id.
func relatedTarget(entry string) string {
	target, _, ok := links.ParseWikilink(entry)
	if !ok {
		target = strings.TrimSpace(entry)
	}
	target = strings.TrimSuffix(strings.Trim(target, "/"), ".md")
	return strings.TrimPrefix(target, "tree/")
}

// resolveRelated fills RelatedTo for every project once all of them are loaded.
func resolveRelated(projects []*Project) {
	byID := map[string][]*Project{}
	for _, p := range projects {
		byID[strings.ToLower(p.ID())] = append(byID[strings.ToLower(p.ID())], p)
	}
	for _, p := range projects {
		p.RelatedTo = nil
		for _, entry := range p.Related {
			target := relatedTarget(entry)
			if target == "" {
				continue
			}
			var found *Project
			if q := FindByRel(projects, target); q != nil && q.Rel == target {
				found = q
			} else if same := byID[strings.ToLower(target)]; len(same) == 1 {
				found = same[0]
			}
			switch {
			case found == nil:
				p.Warnings = append(p.Warnings, fmt.Sprintf("related: %s names no project", entry))
			case found == p:
				p.Warnings = append(p.Warnings, "related: names the project itself")
			case !contains(p.RelatedTo, found.Rel):
				p.RelatedTo = append(p.RelatedTo, found.Rel)
			}
		}
	}
}

// RelatedFrom lists the projects whose related list names p.
func RelatedFrom(projects []*Project, p *Project) []*Project {
	var out []*Project
	for _, q := range projects {
		if q != p && contains(q.RelatedTo, p.Rel) {
			out = append(out, q)
		}
	}
	return out
}

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

// CleanCategory trims a category's slashes and refuses one that climbs out of the tree.
func CleanCategory(category string) (string, error) {
	clean := strings.Trim(filepath.ToSlash(category), "/")
	if hasParentSegment(clean) {
		return "", fmt.Errorf("category %q must stay inside the tree", category)
	}
	return clean, nil
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
	pages, _, _ := links.Walk(Atlas(root))
	ResolveLinks(p, pages)
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
	resolveRelated(projects)
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
	category, err := CleanCategory(opts.Category)
	if err != nil {
		return "", err
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
		Related:   []string{},
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
	Stubs         *int `json:"stubs"`
	WantedPages   *int `json:"wanted_pages"`
	DeadLinks     *int `json:"dead_links"`
}

func (u Unfinished) counts() []struct {
	label string
	n     *int
} {
	return []struct {
		label string
		n     *int
	}{{"empty sections", u.EmptySections}, {"stubs", u.Stubs}, {"wanted pages", u.WantedPages}, {"dead links", u.DeadLinks}}
}

// Text lists the known counts with their labels, such as "1 empty sections · 2 stubs".
func (u Unfinished) Text() string {
	var bits []string
	for _, c := range u.counts() {
		if c.n != nil {
			bits = append(bits, fmt.Sprintf("%d %s", *c.n, c.label))
		}
	}
	return strings.Join(bits, " · ")
}

// Total adds the known counts; nil when none is known.
func (u Unfinished) Total() *int {
	var total *int
	for _, c := range u.counts() {
		if c.n != nil {
			if total == nil {
				total = new(int)
			}
			*total += *c.n
		}
	}
	return total
}

// TaskLine is one open task as the atlas shows it.
type TaskLine struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Due         string `json:"due,omitempty"`
	Workdir     string `json:"workdir,omitempty"`
	LastTouched string `json:"last_touched"`
	Path        string `json:"path"` // absolute path of the task page
	Stale       bool   `json:"stale,omitempty"`
}

// TaskSummary is what refresh read from a vault's task ledger.
type TaskSummary struct {
	Counts tasks.Counts `json:"counts"`
	Open   []TaskLine   `json:"open"`
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
	Tasks           *TaskSummary `json:"tasks,omitempty"`
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
