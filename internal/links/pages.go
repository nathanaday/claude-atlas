package links

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nathanaday/claude-atlas/internal/home"
)

// A linked folder is a page in the atlas vault, repos/<name>.md or materials/<name>.md,
// whose frontmatter holds the folder's path. A project page names it with a wikilink, so
// Obsidian draws the edge and a folder shared by two projects is one node between them.

const PageSchema = "atlas.link.v1"

// Dir is the atlas directory that holds a kind's pages.
func Dir(kind string) string {
	if kind == Repo {
		return "repos"
	}
	return "materials"
}

// Page is one linked folder's page.
type Page struct {
	Kind string `json:"kind"`
	Name string `json:"name"` // file name without .md; what links call it
	Path string `json:"path"` // the folder, absolute
	File string `json:"-"`    // the page on disk
}

// Rel is the page's vault-relative path without .md, as a wikilink target.
func (p Page) Rel() string { return Dir(p.Kind) + "/" + p.Name }

// Wikilink is how a project page refers to this page.
func (p Page) Wikilink() string { return "[[" + p.Rel() + "|" + p.Name + "]]" }

// Problem is a file under repos/ or materials/ that is not a link page.
type Problem struct {
	File   string
	Reason string
}

type pageFront struct {
	Schema string `yaml:"schema"`
	Path   string `yaml:"path"`
}

// splitFront separates a leading YAML block from the body.
func splitFront(text string) (front, body string, ok bool) {
	if !strings.HasPrefix(text, "---\n") {
		return "", text, false
	}
	rest := text[4:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", text, false
	}
	return rest[:idx], strings.TrimLeft(rest[idx+4:], "\n"), true
}

// LoadPage reads one link page of a kind.
func LoadPage(kind, file string) (Page, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Page{}, err
	}
	front, _, ok := splitFront(string(data))
	if !ok {
		return Page{}, fmt.Errorf("missing frontmatter")
	}
	var f pageFront
	if err := yaml.Unmarshal([]byte(front), &f); err != nil {
		return Page{}, err
	}
	if f.Schema != PageSchema {
		return Page{}, fmt.Errorf("frontmatter needs `schema: %s`", PageSchema)
	}
	if f.Path == "" {
		return Page{}, fmt.Errorf("frontmatter needs `path: <folder>`")
	}
	abs, err := filepath.Abs(home.Expand(f.Path))
	if err != nil {
		return Page{}, err
	}
	return Page{Kind: kind, Name: strings.TrimSuffix(filepath.Base(file), ".md"), Path: abs, File: file}, nil
}

// Walk reads every link page in the atlas vault, repos first, each kind sorted by name.
func Walk(atlas string) ([]Page, []Problem, error) {
	var pages []Page
	var problems []Problem
	for _, kind := range []string{Repo, Materials} {
		dir := filepath.Join(atlas, Dir(kind))
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
				continue
			}
			file := filepath.Join(dir, name)
			page, err := LoadPage(kind, file)
			if err != nil {
				problems = append(problems, Problem{File: Dir(kind) + "/" + name, Reason: err.Error()})
				continue
			}
			pages = append(pages, page)
		}
	}
	sort.SliceStable(pages, func(i, j int) bool {
		if pages[i].Kind != pages[j].Kind {
			return pages[i].Kind == Repo
		}
		return strings.ToLower(pages[i].Name) < strings.ToLower(pages[j].Name)
	})
	return pages, problems, nil
}

// FindByPath returns the page for a folder, whatever its kind.
func FindByPath(pages []Page, path string) *Page {
	target := filepath.Clean(home.Expand(path))
	for i := range pages {
		if filepath.Clean(pages[i].Path) == target {
			return &pages[i]
		}
	}
	return nil
}

// FindByName returns the page of a kind by its name, ignoring case.
func FindByName(pages []Page, kind, name string) *Page {
	for i := range pages {
		if pages[i].Kind == kind && strings.EqualFold(pages[i].Name, name) {
			return &pages[i]
		}
	}
	return nil
}

// FindPage returns the page a user names: "repos/code", "materials/Slides", or a bare
// name when only one kind has it.
func FindPage(pages []Page, target string) (*Page, error) {
	target = strings.TrimSuffix(strings.TrimSpace(target), ".md")
	if dir, name, ok := strings.Cut(target, "/"); ok {
		for _, kind := range []string{Repo, Materials} {
			if dir == Dir(kind) {
				if page := FindByName(pages, kind, name); page != nil {
					return page, nil
				}
				return nil, fmt.Errorf("no page %s/%s.md", dir, name)
			}
		}
		return nil, fmt.Errorf("%s is not a page under repos/ or materials/", target)
	}
	repo, material := FindByName(pages, Repo, target), FindByName(pages, Materials, target)
	switch {
	case repo != nil && material != nil:
		return nil, fmt.Errorf("both repos/%s.md and materials/%s.md exist; name one with its folder", target, target)
	case repo != nil:
		return repo, nil
	case material != nil:
		return material, nil
	}
	return nil, fmt.Errorf("no page named %q under repos/ or materials/", target)
}

var wikilinkPattern = regexp.MustCompile(`^\[\[([^\]|#]+)(?:#[^\]|]*)?(?:\|([^\]]*))?\]\]$`)

// ParseWikilink splits "[[target|alias]]"; ok is false when s is not a wikilink.
func ParseWikilink(s string) (target, alias string, ok bool) {
	m := wikilinkPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", "", false
	}
	return strings.TrimSpace(m[1]), strings.TrimSpace(m[2]), true
}

// Resolve maps one entry of a project's repos or materials list to a page: a wikilink to
// a page, or a plain folder path that a page already holds. The second result says
// whether the entry was a wikilink.
func Resolve(pages []Page, kind, entry string) (*Page, bool) {
	target, _, isLink := ParseWikilink(entry)
	if !isLink {
		return FindByPath(pages, entry), false
	}
	target = strings.TrimSuffix(target, ".md")
	if dir, name, ok := strings.Cut(target, "/"); ok {
		for _, k := range []string{Repo, Materials} {
			if dir == Dir(k) {
				return FindByName(pages, k, name), true
			}
		}
		return nil, true
	}
	return FindByName(pages, kind, target), true
}

var badNameChars = regexp.MustCompile(`[\\/:*?"<>|#^\[\]]+`)

// CleanName makes a page name safe for Obsidian and for wikilinks; empty when nothing
// usable is left.
func CleanName(name string) string {
	return strings.Trim(badNameChars.ReplaceAllString(name, "-"), " .-")
}

// PageName picks a file name for a folder: its base name, made safe for Obsidian, and
// suffixed with the parent's name when another page of the kind already uses it.
func PageName(path string, taken func(string) bool) string {
	base := CleanName(filepath.Base(path))
	if base == "" {
		base = "folder"
	}
	if !taken(base) {
		return base
	}
	parent := CleanName(filepath.Base(filepath.Dir(path)))
	if parent == "" {
		parent = "2"
	}
	candidate := fmt.Sprintf("%s (%s)", base, parent)
	for n := 2; taken(candidate); n++ {
		candidate = fmt.Sprintf("%s (%s %d)", base, parent, n)
	}
	return candidate
}

// Create writes the page for a folder under the kind's directory and returns it. A page
// that already holds the folder is returned as is.
func Create(atlas, kind, path string, existing []Page) (Page, error) {
	abs, err := filepath.Abs(home.Expand(path))
	if err != nil {
		return Page{}, err
	}
	if page := FindByPath(existing, abs); page != nil {
		return *page, nil
	}
	dir := filepath.Join(atlas, Dir(kind))
	taken := func(name string) bool {
		if FindByName(existing, kind, name) != nil {
			return true
		}
		_, err := os.Stat(filepath.Join(dir, name+".md"))
		return err == nil
	}
	page := Page{Kind: kind, Name: PageName(abs, taken), Path: abs}
	page.File = filepath.Join(dir, page.Name+".md")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Page{}, err
	}
	what := "folder"
	if kind == Repo {
		what = "repo"
	}
	text := fmt.Sprintf("---\nschema: %s\npath: %s\n---\n\n# %s\n\nNotes about this %s that belong to the atlas.\n",
		PageSchema, quoteYAML(abs), page.Name, what)
	if err := os.WriteFile(page.File, []byte(text), 0o644); err != nil {
		return Page{}, err
	}
	return page, nil
}

func quoteYAML(s string) string {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	if err := enc.Encode(s); err != nil {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	enc.Close()
	return strings.TrimSpace(b.String())
}
