package vaults

import (
	"errors"
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

// TreeEdit is a set of changes to one project. Empty strings mean "unchanged" except
// Purpose, which may be cleared by setting ClearPurpose. Pointer fields are unchanged
// when nil and cleared when they point at "".
type TreeEdit struct {
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

// Update applies a TreeEdit: frontmatter first, then the vault, then the page's category.
func Update(cfg *home.Config, p *tree.Project, edit TreeEdit) error {
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
				if err := moveVault(cfg, p.VaultPath(), target); err != nil {
					return err
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

// moveVault renames a vault's folder to target, repoints the repository pages whose
// folders sit inside it, and removes the folders it leaves empty in the vaults directory.
func moveVault(cfg *home.Config, from, target string) error {
	if err := CheckMove(from, target); err != nil {
		return err
	}
	pages, _, err := links.Walk(cfg.AtlasVault)
	if err != nil {
		return err
	}
	source := from
	if _, ok := under(from, target); ok {
		// A vault moving into its own folder, as admin to admin/admin, steps aside first.
		source = from + ".moving"
		if _, err := os.Stat(source); err == nil {
			return fmt.Errorf("%s is in the way; remove it and move again", home.Display(source))
		}
		if err := os.Rename(from, source); err != nil {
			return fmt.Errorf("move vault: %w", err)
		}
	}
	err = os.MkdirAll(filepath.Dir(target), 0o755)
	if err == nil {
		err = os.Rename(source, target)
	}
	if err != nil {
		if source != from {
			os.Rename(source, from)
		}
		return fmt.Errorf("move vault: %w", err)
	}
	for _, page := range pages {
		if rel, ok := under(from, page.Path); ok {
			if err := tree.UpdateFrontmatter(page.File, map[string]any{"path": filepath.Join(target, rel)}); err != nil {
				return err
			}
		}
	}
	pruneEmpty(cfg.VaultsDir, filepath.Dir(from))
	return nil
}

// CheckMove says why the vault at from cannot move to target: something is there, or
// target is inside another vault. A vault may move into its own folder.
func CheckMove(from, target string) error {
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("%s already exists; cannot move the vault there", home.Display(target))
	}
	if outer := vault.FindAbove(filepath.Dir(target)); outer != "" && outer != from {
		return fmt.Errorf("%s is inside the vault %s; cannot move the vault there", home.Display(target), home.Display(outer))
	}
	return nil
}

// CategoryPath is where a project's vault goes when its page moves to category: the same
// folder name in the new category's folder. It is empty when the category stays, or when
// the vault does not sit in its category's folder, because then the user placed it.
func CategoryPath(vaultsDir string, p *tree.Project, category string) string {
	category, err := tree.CleanCategory(category)
	if err != nil || category == p.Category() || vaultsDir == "" {
		return ""
	}
	current := filepath.Clean(p.VaultPath())
	if filepath.Dir(current) != filepath.Join(vaultsDir, filepath.FromSlash(p.Category())) {
		return ""
	}
	return filepath.Join(vaultsDir, filepath.FromSlash(category), filepath.Base(current))
}

// under gives path relative to root when path is root or inside it.
func under(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// pruneEmpty removes dir and the folders above it while they are empty, stopping at the
// vaults directory.
func pruneEmpty(vaultsDir, dir string) {
	for {
		rel, ok := under(vaultsDir, dir)
		if !ok || rel == "." || os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
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

// NotRepoError says a folder exists but is not a git repository; the caller may ask
// the user and link again with init.
type NotRepoError struct{ Path string }

func (e *NotRepoError) Error() string {
	return home.Display(e.Path) + " is not a git repository; a link is a mounted repository (initialize one there, or pass --init)"
}

// ResolveTarget finds what a user means by a link target: the name of a page, or a
// folder on disk.
func ResolveTarget(cfg *home.Config, target string) (links.Page, error) {
	pages, _, err := links.Walk(cfg.AtlasVault)
	if err != nil {
		return links.Page{}, err
	}
	if !strings.ContainsAny(target, "/~") {
		if page, err := links.FindPage(pages, target); err == nil {
			return *page, nil
		}
	}
	abs, err := filepath.Abs(home.Expand(target))
	if err != nil {
		return links.Page{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return links.Page{}, fmt.Errorf("%s is neither a repository's page nor a folder on disk", target)
	}
	if !info.IsDir() {
		return links.Page{}, fmt.Errorf("%s is not a directory", home.Display(abs))
	}
	if page := links.FindByPath(pages, abs); page != nil {
		return *page, nil
	}
	return links.Page{Kind: links.Repo, Path: abs}, nil
}

// AddLink mounts a repository on a project: target is a page name or a folder. A folder
// that is not a git repository is refused with a NotRepoError unless initGit is set, in
// which case it becomes one first. It returns the page, created if new.
func AddLink(cfg *home.Config, p *tree.Project, target string, initGit bool) (links.Page, error) {
	page, err := ResolveTarget(cfg, target)
	if err != nil {
		return links.Page{}, err
	}
	if p.LinkedTo(page.Path) {
		return page, fmt.Errorf("%s is already linked to %s", home.Display(page.Path), p.Name)
	}
	if page.Kind == links.Repo && !links.IsRepo(page.Path) {
		if !initGit {
			return links.Page{}, &NotRepoError{Path: page.Path}
		}
		if err := links.InitRepo(page.Path, filepath.Base(page.Path)); err != nil {
			return links.Page{}, err
		}
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

// repoDir decides where a new or cloned repository goes: at, or a folder of that name
// in the vault's root, beside the wiki. Inside the vault it must sit beside the wiki,
// and the vault's git ignores it so the two histories stay apart.
func repoDir(p *tree.Project, name, at string) (string, error) {
	dir := filepath.Join(p.VaultPath(), name)
	if at != "" {
		abs, err := filepath.Abs(home.Expand(at))
		if err != nil {
			return "", err
		}
		dir = abs
	}
	if _, err := os.Stat(dir); err == nil {
		if entries, _ := os.ReadDir(dir); len(entries) > 0 {
			return "", fmt.Errorf("%s already exists; link it instead", home.Display(dir))
		}
	}
	if rel, ok := under(p.VaultPath(), dir); ok {
		if rel == "." || strings.HasPrefix(rel, vault.WikiDir+"/") || rel == vault.WikiDir || strings.HasPrefix(rel, ".") {
			return "", fmt.Errorf("a repository goes beside the wiki, not in %s", home.Display(dir))
		}
		if _, err := vault.Ignore(p.VaultPath(), "/"+filepath.ToSlash(rel)+"/", time.Now()); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// NewRepo creates a git repository for a project's deliverables and mounts it.
func NewRepo(cfg *home.Config, p *tree.Project, name, at string) (links.Page, error) {
	name = links.CleanName(name)
	if name == "" {
		return links.Page{}, errors.New("the repository needs a name")
	}
	dir, err := repoDir(p, name, at)
	if err != nil {
		return links.Page{}, err
	}
	if err := links.CreateRepo(dir, name); err != nil {
		return links.Page{}, err
	}
	return AddLink(cfg, p, dir, false)
}

// CloneRepoPage clones a repository from url, into at or beside the wiki, and mounts it.
// The page records the remote; changes defaults to pull requests until set.
func CloneRepoPage(cfg *home.Config, p *tree.Project, url, at string) (links.Page, error) {
	url = strings.TrimSpace(url)
	name := links.NameFromURL(url)
	if name == "" {
		return links.Page{}, fmt.Errorf("cannot tell a repository name from %q", url)
	}
	dir, err := repoDir(p, name, at)
	if err != nil {
		return links.Page{}, err
	}
	if err := links.Clone(url, dir); err != nil {
		return links.Page{}, err
	}
	page, err := AddLink(cfg, p, dir, false)
	if err != nil {
		return links.Page{}, err
	}
	if page.Remote == "" {
		remote := url
		return UpdateLink(cfg, page, LinkEdit{Remote: &remote})
	}
	return page, nil
}

// SetChanges records how claude-atlas sessions land changes in a repository.
func SetChanges(cfg *home.Config, page links.Page, policy string) (links.Page, error) {
	return UpdateLink(cfg, page, LinkEdit{Changes: &policy})
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

// LinkEdit changes a repository page. Empty fields are unchanged; Remote and Changes
// point at "" to clear.
type LinkEdit struct {
	Name    string
	Kind    string // links.Repo or links.Materials
	Path    string
	Remote  *string
	Changes *string // links.ChangesPR, links.ChangesCommit, or ""
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
	if updated.Kind == links.Repo && edit.Path != "" && !links.IsRepo(updated.Path) {
		return page, &NotRepoError{Path: updated.Path}
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
	fields := map[string]any{}
	if updated.Path != page.Path {
		fields["path"] = updated.Path
	}
	if edit.Remote != nil && *edit.Remote != page.Remote {
		updated.Remote = strings.TrimSpace(*edit.Remote)
		fields["remote"] = updated.Remote
	}
	if edit.Changes != nil && *edit.Changes != page.Changes {
		if *edit.Changes != "" && !contains(links.Policies, *edit.Changes) {
			return page, fmt.Errorf("changes must be %s, %s, or empty", links.ChangesPR, links.ChangesCommit)
		}
		updated.Changes = *edit.Changes
		fields["changes"] = updated.Changes
	}
	if len(fields) > 0 {
		if err := tree.UpdateFrontmatter(updated.File, fields); err != nil {
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

// UpgradeLinks brings older pages to the current shape: a plain folder path in a repos
// or materials list gets a page and a wikilink, and a materials page whose folder is a
// git repository moves under repos/. It returns what it changed.
func UpgradeLinks(cfg *home.Config) ([]string, error) {
	pages, _, err := links.Walk(cfg.AtlasVault)
	if err != nil {
		return nil, err
	}
	var upgraded []string
	for _, page := range pages {
		if page.Kind == links.Materials && links.IsRepo(page.Path) {
			moved, err := UpdateLink(cfg, page, LinkEdit{Kind: links.Repo})
			if err != nil {
				return upgraded, fmt.Errorf("%s: %w", page.Rel(), err)
			}
			upgraded = append(upgraded, page.Rel()+" → "+moved.Rel())
		}
	}
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return nil, err
	}
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
		upgraded = append(upgraded, "tree/"+p.Rel)
	}
	return upgraded, nil
}
