package refresh

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/tree"
)

// The tree is a graph once every folder under tree/ has a page: refresh writes one page
// per category under categories/ and a root page, Tree.md, each linking downward. With
// the link pages under repos/ and materials/, Obsidian's graph view then shows the whole
// atlas: categories, projects, and the folders they share.

const (
	CategoriesDir  = "categories"
	TreePage       = "Tree.md"
	CategorySchema = "atlas.category.v1"
)

// category is one folder under tree/ with what sits directly inside it.
type category struct {
	path     string // "engineering/usc"; "" for the root
	children []string
	projects []*tree.Project
}

func categoryName(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// categoryLink is the wikilink for a category path; the root is Tree.md.
func categoryLink(path string) string {
	if path == "" {
		return "[[Tree]]"
	}
	return "[[" + CategoriesDir + "/" + path + "|" + categoryName(path) + "]]"
}

// categories arranges the projects by folder, creating every ancestor on the way.
func categories(projects []*tree.Project) map[string]*category {
	out := map[string]*category{"": {}}
	for _, p := range projects {
		cat := p.Category()
		segments := strings.Split(cat, "/")
		if cat == "" {
			segments = nil
		}
		path := ""
		for _, seg := range segments {
			parent := path
			if path == "" {
				path = seg
			} else {
				path += "/" + seg
			}
			if _, ok := out[path]; !ok {
				out[path] = &category{path: path}
				out[parent].children = append(out[parent].children, path)
			}
		}
		out[path].projects = append(out[path].projects, p)
	}
	for _, c := range out {
		sort.Strings(c.children)
		sort.Slice(c.projects, func(i, j int) bool { return c.projects[i].Name < c.projects[j].Name })
	}
	return out
}

func countProjects(cats map[string]*category, path string) int {
	c := cats[path]
	n := len(c.projects)
	for _, child := range c.children {
		n += countProjects(cats, child)
	}
	return n
}

func renderCategory(cats map[string]*category, c *category) string {
	var b strings.Builder
	if c.path == "" {
		b.WriteString("---\ntitle: Tree\n---\n\n")
		b.WriteString("> [!info] Generated page\n> `claude-atlas refresh` rewrites this page and every page under `categories/` from the folders under `tree/`. Together with the pages under `repos/` and `materials/` they make the atlas a graph: open the graph view to see categories, projects, and the folders they share.\n\n")
	} else {
		fmt.Fprintf(&b, "---\nschema: %s\nname: %s\npath: %s\n---\n\n", CategorySchema, categoryName(c.path), c.path)
		b.WriteString("> [!info] Generated page\n> `claude-atlas refresh` rewrites this page from the folder `tree/" + c.path + "/`. Move project pages between folders to change it.\n\n")
		parent := ""
		if i := strings.LastIndex(c.path, "/"); i >= 0 {
			parent = c.path[:i]
		}
		b.WriteString("Part of " + categoryLink(parent) + ".\n\n")
	}
	if len(c.children) > 0 {
		b.WriteString("## Categories\n\n")
		for _, child := range c.children {
			n := countProjects(cats, child)
			fmt.Fprintf(&b, "- %s · %d project%s\n", categoryLink(child), n, plural(n))
		}
		b.WriteString("\n")
	}
	if len(c.projects) > 0 {
		b.WriteString("## Projects\n\n")
		for _, p := range c.projects {
			line := "- " + p.Wikilink()
			if purpose := strings.TrimSpace(strings.SplitN(p.Purpose, "\n", 2)[0]); purpose != "" {
				line += " — " + purpose
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	if len(c.children)+len(c.projects) == 0 {
		b.WriteString("No projects yet; run `claude-atlas new-vault`.\n")
	}
	return b.String()
}

// WriteCategories rewrites categories/ and Tree.md from scratch.
func WriteCategories(cfg *home.Config, projects []*tree.Project) error {
	dir := filepath.Join(cfg.AtlasVault, CategoriesDir)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	cats := categories(projects)
	for path, c := range cats {
		file := TreePage
		if path != "" {
			file = filepath.Join(CategoriesDir, filepath.FromSlash(path)+".md")
		}
		full := filepath.Join(cfg.AtlasVault, file)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(renderCategory(cats, c)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// LinkRow is one link page with what refresh found in its folder and the projects that
// link it.
type LinkRow struct {
	Page     links.Page `json:"page"`
	Link     links.Link `json:"link"`
	Projects []string   `json:"projects"`
}

// LinksState is the derived view of every link page, written to state/links.json.
type LinksState struct {
	Schema      string    `json:"schema"`
	GeneratedAt string    `json:"generated_at"`
	Links       []LinkRow `json:"links"`
}

const LinksStateSchema = "atlas.links.v1"

// inspectPages derives the facts for every link page once and says who links each.
func inspectPages(pages []links.Page, projects []*tree.Project) []LinkRow {
	rows := make([]LinkRow, 0, len(pages))
	for _, page := range pages {
		link := links.Inspect(page.Kind, page.Path)
		link.Name = page.Name
		row := LinkRow{Page: page, Link: link, Projects: []string{}}
		for _, p := range projects {
			if p.LinkedTo(page.Path) {
				row.Projects = append(row.Projects, p.Rel)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func linksStatePath(stateDir string) string { return filepath.Join(stateDir, "links.json") }

// ReadLinksState loads what the last refresh found in the link pages.
func ReadLinksState(stateDir string) (*LinksState, error) {
	data, err := os.ReadFile(linksStatePath(stateDir))
	if err != nil {
		return nil, err
	}
	var state LinksState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
