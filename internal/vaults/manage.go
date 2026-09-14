package vaults

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/tree"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// Edit is a set of changes to one project. Empty strings mean "unchanged" except
// Purpose, which may be cleared by setting ClearPurpose. Pointer fields are unchanged
// when nil and cleared when they point at "".
type Edit struct {
	Name             string
	Purpose          string
	ClearPurpose     bool
	Priority         string
	State            string
	BlockedOn        *string
	ReviewAfter      *string // YYYY-MM-DD or ""
	DefinitionOfDone *string
	Category         *string // nil: unchanged; "" : top level
	Vault            string  // new vault path; "" unchanged
	MoveVault        bool    // move the directory on disk to Vault
	// Links replaces the linked folders; a page without a Name is created on save.
	Links *[]links.Page
	// Related replaces the related projects, by rel.
	Related *[]string
}

// ValidReviewDate reports whether s is empty or a YYYY-MM-DD date.
func ValidReviewDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// Update applies an Edit: frontmatter first, then the vault, then the page's category.
func Update(cfg *home.Config, p *tree.Project, edit Edit) error {
	fields := map[string]any{}
	if edit.Priority != "" && !contains(tree.Priorities, edit.Priority) {
		return fmt.Errorf("priority must be one of %s", strings.Join(tree.Priorities, ", "))
	}
	if edit.State != "" && !contains(tree.States, edit.State) {
		return fmt.Errorf("state must be one of %s", strings.Join(tree.States, ", "))
	}
	if edit.ReviewAfter != nil && !ValidReviewDate(*edit.ReviewAfter) {
		return fmt.Errorf("review_after must be a date like 2026-10-01")
	}
	if edit.BlockedOn != nil && *edit.BlockedOn != p.BlockedOn {
		fields["blocked_on"] = *edit.BlockedOn
	}
	if edit.ReviewAfter != nil && *edit.ReviewAfter != p.ReviewAfter {
		fields["review_after"] = *edit.ReviewAfter
	}
	if edit.DefinitionOfDone != nil && *edit.DefinitionOfDone != p.DefinitionOfDone {
		fields["definition_of_done"] = *edit.DefinitionOfDone
	}
	if edit.Name != "" && edit.Name != p.Name {
		fields["name"] = edit.Name
	}
	if edit.ClearPurpose {
		fields["purpose"] = ""
	} else if edit.Purpose != "" && edit.Purpose != p.Purpose {
		fields["purpose"] = edit.Purpose
	}
	if edit.Priority != "" && edit.Priority != p.Priority {
		fields["priority"] = edit.Priority
	}
	if edit.State != "" && edit.State != p.State {
		fields["state"] = edit.State
	}
	if edit.Links != nil {
		repos, materials, err := linkEntries(cfg, *edit.Links)
		if err != nil {
			return err
		}
		fields["repos"], fields["materials"] = repos, materials
	}
	if edit.Related != nil {
		related, err := relatedEntries(cfg, p, *edit.Related)
		if err != nil {
			return err
		}
		fields["related"] = related
	}
	if edit.Vault != "" {
		target, err := filepath.Abs(home.Expand(edit.Vault))
		if err != nil {
			return err
		}
		if target != p.VaultPath() {
			if edit.MoveVault {
				if _, err := os.Stat(target); err == nil {
					return fmt.Errorf("%s already exists; cannot move the vault there", home.Display(target))
				}
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return err
				}
				if err := os.Rename(p.VaultPath(), target); err != nil {
					return fmt.Errorf("move vault: %w", err)
				}
			} else if !vault.IsVault(target) && !vault.IsLegacy(target) {
				return fmt.Errorf("%s is not a claude-atlas vault", home.Display(target))
			}
			fields["vault"] = target
		}
	}
	if len(fields) > 0 {
		if err := tree.UpdateFrontmatter(p.Path, fields); err != nil {
			return err
		}
	}
	if edit.Category != nil && *edit.Category != p.Category() {
		if _, err := tree.Move(cfg.TreeRoot(), p, *edit.Category); err != nil {
			return err
		}
	}
	return nil
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func orEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// linkEntries turns pages into the repos and materials lists, creating the page for any
// folder that has none yet.
func linkEntries(cfg *home.Config, pages []links.Page) (repos, materials []string, err error) {
	repos, materials = []string{}, []string{}
	existing, _, err := links.Walk(cfg.AtlasVault)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	for _, page := range pages {
		if page.Name == "" {
			created, err := links.Create(cfg.AtlasVault, page.Kind, page.Path, existing)
			if err != nil {
				return nil, nil, err
			}
			existing = append(existing, created)
			page = created
		}
		if seen[page.Rel()] {
			continue
		}
		seen[page.Rel()] = true
		if page.Kind == links.Repo {
			repos = append(repos, page.Wikilink())
		} else {
			materials = append(materials, page.Wikilink())
		}
	}
	return repos, materials, nil
}

// relatedEntries turns project rels into wikilinks, in the order given.
func relatedEntries(cfg *home.Config, p *tree.Project, rels []string) ([]string, error) {
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, rel := range rels {
		q := tree.FindByRel(projects, rel)
		if q == nil {
			return nil, fmt.Errorf("no project named %q", rel)
		}
		if q.Rel == p.Rel {
			return nil, fmt.Errorf("%s cannot relate to itself", p.Name)
		}
		if !contains(out, q.Wikilink()) {
			out = append(out, q.Wikilink())
		}
	}
	return out, nil
}

// SetLinks replaces a project's linked folders.
func SetLinks(cfg *home.Config, p *tree.Project, pages []links.Page) error {
	repos, materials, err := linkEntries(cfg, pages)
	if err != nil {
		return err
	}
	return tree.UpdateFrontmatter(p.Path, map[string]any{"repos": repos, "materials": materials})
}

// currentLinks lists a project's linked folders as pages, keeping plain paths as
// pages without a name so a save gives them one.
func currentLinks(p *tree.Project) []links.Page {
	var out []links.Page
	for _, l := range p.Linked {
		if l.Path == "" {
			continue
		}
		out = append(out, links.Page{Kind: l.Kind, Name: l.Name, Path: l.Path})
	}
	return out
}

// ResolveTarget finds what a user means by a link target: the name of a page of the
// kind, or a folder on disk. kind may be empty to detect it from the folder.
func ResolveTarget(cfg *home.Config, kind, target string) (links.Page, error) {
	pages, _, err := links.Walk(cfg.AtlasVault)
	if err != nil {
		return links.Page{}, err
	}
	if !strings.ContainsAny(target, "/~") {
		for _, k := range []string{links.Repo, links.Materials} {
			if kind != "" && kind != k {
				continue
			}
			if page := links.FindByName(pages, k, target); page != nil {
				return *page, nil
			}
		}
	}
	abs, err := filepath.Abs(home.Expand(target))
	if err != nil {
		return links.Page{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return links.Page{}, fmt.Errorf("%s is neither a linked folder's page nor a path on disk", target)
	}
	if kind == "" {
		kind = links.DetectKind(abs)
	}
	if kind == links.Repo && !info.IsDir() {
		return links.Page{}, fmt.Errorf("%s is not a directory", home.Display(abs))
	}
	if page := links.FindByPath(pages, abs); page != nil {
		return *page, nil
	}
	return links.Page{Kind: kind, Path: abs}, nil
}

// AddLink links a folder to a project: target is a page name or a path; kind is
// links.Repo, links.Materials, or "" to detect it. It returns the page, created if new.
func AddLink(cfg *home.Config, p *tree.Project, kind, target string) (links.Page, error) {
	page, err := ResolveTarget(cfg, kind, target)
	if err != nil {
		return links.Page{}, err
	}
	if p.LinkedTo(page.Path) {
		return page, fmt.Errorf("%s is already linked to %s", home.Display(page.Path), p.Name)
	}
	pages := append(currentLinks(p), page)
	if err := SetLinks(cfg, p, pages); err != nil {
		return links.Page{}, err
	}
	if page.Name == "" {
		existing, _, _ := links.Walk(cfg.AtlasVault)
		if created := links.FindByPath(existing, page.Path); created != nil {
			page = *created
		}
	}
	return page, nil
}

// RemoveLink drops a folder from a project page, by path or page name. The page and the
// folder are untouched.
func RemoveLink(cfg *home.Config, p *tree.Project, target string) error {
	abs, _ := filepath.Abs(home.Expand(target))
	var keep []links.Page
	found := false
	for _, l := range p.Linked {
		match := (l.Path != "" && filepath.Clean(l.Path) == filepath.Clean(abs)) || (l.Name != "" && strings.EqualFold(l.Name, target))
		if match {
			found = true
			continue
		}
		if l.Path == "" {
			continue
		}
		keep = append(keep, links.Page{Kind: l.Kind, Name: l.Name, Path: l.Path})
	}
	if !found {
		return fmt.Errorf("%s is not linked to %s", target, p.Name)
	}
	return SetLinks(cfg, p, keep)
}

// LinkEdit changes a link page. Empty fields are unchanged.
type LinkEdit struct {
	Name string
	Kind string // links.Repo or links.Materials
	Path string
}

// UpdateLink renames a link page, moves it between repos/ and materials/, or points it
// at another folder, and rewrites every project page that links it.
func UpdateLink(cfg *home.Config, page links.Page, edit LinkEdit) (links.Page, error) {
	before, _, err := links.Walk(cfg.AtlasVault)
	if err != nil {
		return page, err
	}
	if page.File == "" {
		page.File = filepath.Join(cfg.AtlasVault, links.Dir(page.Kind), page.Name+".md")
	}
	if _, err := os.Stat(page.File); err != nil {
		return page, fmt.Errorf("no page %s.md", page.Rel())
	}
	updated := page
	if edit.Kind != "" {
		if edit.Kind != links.Repo && edit.Kind != links.Materials {
			return page, fmt.Errorf("kind must be %s or %s", links.Repo, links.Materials)
		}
		updated.Kind = edit.Kind
	}
	if edit.Name != "" {
		name := links.CleanName(edit.Name)
		if name == "" {
			return page, fmt.Errorf("%q leaves no usable page name", edit.Name)
		}
		updated.Name = name
	}
	if edit.Path != "" {
		abs, err := filepath.Abs(home.Expand(edit.Path))
		if err != nil {
			return page, err
		}
		if other := links.FindByPath(before, abs); other != nil && other.File != page.File {
			return page, fmt.Errorf("%s is already the folder of %s", home.Display(abs), other.Rel())
		}
		updated.Path = abs
	}
	info, err := os.Stat(updated.Path)
	if err != nil {
		return page, fmt.Errorf("%s: not found", home.Display(updated.Path))
	}
	if updated.Kind == links.Repo && !info.IsDir() {
		return page, fmt.Errorf("%s is not a directory, so it cannot be a repo", home.Display(updated.Path))
	}
	updated.File = filepath.Join(cfg.AtlasVault, links.Dir(updated.Kind), updated.Name+".md")
	if updated.File != page.File {
		if _, err := os.Stat(updated.File); err == nil {
			return page, fmt.Errorf("%s.md already exists", updated.Rel())
		}
		if err := os.MkdirAll(filepath.Dir(updated.File), 0o755); err != nil {
			return page, err
		}
		if err := os.Rename(page.File, updated.File); err != nil {
			return page, err
		}
	}
	if updated.Path != page.Path {
		if err := tree.UpdateFrontmatter(updated.File, map[string]any{"path": updated.Path}); err != nil {
			return page, err
		}
	}
	if updated.Rel() == page.Rel() {
		return updated, nil
	}
	// Every project that linked the old page now links the new one, in the right list.
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return updated, err
	}
	for _, p := range projects {
		repos, materials, changed := []string{}, []string{}, false
		for _, kind := range []string{links.Repo, links.Materials} {
			entries := p.Repos
			if kind == links.Materials {
				entries = p.Materials
			}
			for _, entry := range entries {
				target := kind
				if r, _ := links.Resolve(before, kind, entry); r != nil && r.File == page.File {
					entry, target, changed = updated.Wikilink(), updated.Kind, true
				}
				if target == links.Repo {
					repos = append(repos, entry)
				} else {
					materials = append(materials, entry)
				}
			}
		}
		if !changed {
			continue
		}
		if err := tree.UpdateFrontmatter(p.Path, map[string]any{"repos": repos, "materials": materials}); err != nil {
			return updated, err
		}
	}
	return updated, nil
}

// Relate records that a and b belong together. One side holds the link; the other
// side's page shows it as a backlink, and the atlas reads both directions.
func Relate(cfg *home.Config, a, b *tree.Project) error {
	if a.Rel == b.Rel {
		return fmt.Errorf("%s cannot relate to itself", a.Name)
	}
	if contains(a.RelatedTo, b.Rel) || contains(b.RelatedTo, a.Rel) {
		return fmt.Errorf("%s and %s are already related", a.Name, b.Name)
	}
	related, err := relatedEntries(cfg, a, append(append([]string{}, a.RelatedTo...), b.Rel))
	if err != nil {
		return err
	}
	return tree.UpdateFrontmatter(a.Path, map[string]any{"related": related})
}

// Unrelate removes the relation between a and b from whichever side holds it.
func Unrelate(cfg *home.Config, a, b *tree.Project) error {
	found := false
	for _, pair := range [][2]*tree.Project{{a, b}, {b, a}} {
		from, to := pair[0], pair[1]
		if !contains(from.RelatedTo, to.Rel) {
			continue
		}
		found = true
		var keep []string
		for _, rel := range from.RelatedTo {
			if rel != to.Rel {
				keep = append(keep, rel)
			}
		}
		related, err := relatedEntries(cfg, from, keep)
		if err != nil {
			return err
		}
		if err := tree.UpdateFrontmatter(from.Path, map[string]any{"related": related}); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("%s and %s are not related", a.Name, b.Name)
	}
	return nil
}

// Unlink removes a project from the atlas. The vault stays on disk.
func Unlink(p *tree.Project) error {
	return tree.Unlink(p)
}

// UpgradeLinks gives every plain folder path in a repos or materials list a page and
// rewrites the entry as a wikilink, so the graph shows the folder. It returns the rels
// of the pages it changed. Pages that already use wikilinks are untouched.
func UpgradeLinks(cfg *home.Config) ([]string, error) {
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return nil, err
	}
	var upgraded []string
	for _, p := range projects {
		plain := false
		for _, l := range p.Linked {
			if l.Name == "" && l.Path != "" {
				plain = true
			}
		}
		if !plain {
			continue
		}
		if err := SetLinks(cfg, p, currentLinks(p)); err != nil {
			return upgraded, fmt.Errorf("%s: %w", p.Rel, err)
		}
		upgraded = append(upgraded, p.Rel)
	}
	return upgraded, nil
}
