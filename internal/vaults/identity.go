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
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// kindDir is where a vault of kind sits under the vaults directory.
func kindDir(kind vault.Kind) string {
	if kind == vault.Knowledge {
		return "knowledge"
	}
	return "projects"
}

// PathFor is where a new vault of a kind goes by default.
func PathFor(vaultsDir string, kind vault.Kind, name string) string {
	return filepath.Join(vaultsDir, kindDir(kind), name)
}

// ResolvePath takes anything path-like as the vault's path and puts a bare name at
// PathFor.
func ResolvePath(arg, vaultsDir string, kind vault.Kind) (string, error) {
	if strings.Contains(arg, string(filepath.Separator)) || strings.HasPrefix(arg, "~") || strings.HasPrefix(arg, ".") {
		return filepath.Abs(home.Expand(arg))
	}
	return PathFor(vaultsDir, kind, arg), nil
}

// Register lists a vault outside the vaults directory in the config; it reports whether
// the config changed. A vault inside the vaults directory needs no entry.
func Register(h home.Home, cfg *home.Config, root string) (bool, error) {
	abs, err := filepath.Abs(home.Expand(root))
	if err != nil {
		return false, err
	}
	if !vault.IsVault(abs) {
		return false, fmt.Errorf("%s is not a claude-atlas vault (no %s)", home.Display(abs), vault.Marker)
	}
	if cfg.Inside(abs) {
		return false, nil
	}
	if !cfg.AddVault(abs) {
		return false, nil
	}
	if err := h.Save(cfg); err != nil {
		return false, err
	}
	return true, nil
}

// CheckForget reports why a vault cannot be forgotten. A vault inside the vaults directory
// cannot: the scan finds it; the error says to move or delete the folder. The folder
// itself need not exist; a registered path outlives it.
func CheckForget(cfg *home.Config, root string) error {
	abs, err := filepath.Abs(home.Expand(root))
	if err != nil {
		return err
	}
	if cfg.Inside(abs) {
		return fmt.Errorf("%s is inside the vaults directory; the scan finds it there, so move or delete the folder to forget it", home.Display(abs))
	}
	return nil
}

// Unregister removes a vault from the config.
func Unregister(h home.Home, cfg *home.Config, root string) error {
	abs, err := filepath.Abs(home.Expand(root))
	if err != nil {
		return err
	}
	if err := CheckForget(cfg, abs); err != nil {
		return err
	}
	if !cfg.RemoveVault(abs) {
		return fmt.Errorf("%s is not registered", home.Display(abs))
	}
	return h.Save(cfg)
}

// Edit changes a vault's own facts. Nil means unchanged.
type Edit struct {
	Name   string
	Tags   *[]string // project
	Scope  *string   // knowledge base
	Access *string   // knowledge base: open or guarded
}

// cleanTags trims each tag and drops the ones left empty.
func cleanTags(tags []string) []string {
	var out []string
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

// EditIdentity changes a vault's identity file: one operation, one commit, whatever the
// edit touches. vault.UpdateConfig validates every field against the vault's kind before
// it writes anything, so a field that does not belong to e's kind (scope on a project,
// tags on a knowledge base) fails without touching the file.
func EditIdentity(e registry.Entry, edit Edit, now time.Time) error {
	var fields []string
	if edit.Name != "" {
		fields = append(fields, "name")
	}
	if edit.Tags != nil {
		fields = append(fields, "tags")
	}
	if edit.Scope != nil {
		fields = append(fields, "scope")
	}
	if edit.Access != nil {
		fields = append(fields, "access")
	}
	if len(fields) == 0 {
		return nil
	}
	return vault.UpdateConfig(e.Path, "edit "+strings.Join(fields, ", "), now, func(c *vault.Config) error {
		if edit.Name != "" {
			c.Name = edit.Name
		}
		if edit.Tags != nil {
			c.Tags = cleanTags(*edit.Tags)
		}
		if edit.Scope != nil {
			c.Scope = *edit.Scope
		}
		if edit.Access != nil {
			c.Access = *edit.Access
		}
		return nil
	})
}

// requireProject refuses a repository operation on a knowledge base.
func requireProject(e registry.Entry) error {
	if e.Kind != vault.Project {
		return errors.New("a knowledge base has no repositories")
	}
	return nil
}

// hostName is the name of the repository a project lives in, or "" when the project is
// its own repository. The scan derives it; no identity file records it.
func hostName(e registry.Entry) string {
	if e.Host == "" {
		return ""
	}
	return registry.HostName(e.Host)
}

// isHost reports whether name is the repository the project lives in: the name the scan
// derives, or the folder's own name before it was cleaned.
func isHost(e registry.Entry, name string) bool {
	host := hostName(e)
	if host == "" {
		return false
	}
	return strings.EqualFold(name, host) || strings.EqualFold(name, filepath.Base(e.Host))
}

// takesHostRow reports whether a repository under this name would land on the host's row:
// the name the scan derives, the host folder's own name, or a name that cleans to either.
// Two repositories cannot share one row.
func takesHostRow(e registry.Entry, name string) bool {
	host := hostName(e)
	if host == "" {
		return false
	}
	return isHost(e, name) || strings.EqualFold(links.CleanName(name), host)
}

// isHostFolder reports whether path is the host's own folder.
func isHostFolder(e registry.Entry, path string) bool {
	if e.Host == "" || path == "" {
		return false
	}
	rel, ok := under(e.Host, path)
	return ok && rel == "."
}

// hostRepoError is what a caller reads when it tries to link, create, clone, or drop the
// repository the project lives in. The project's folder sits inside it, so it is already
// the project's first repository, and neither linking nor unlinking changes that.
func hostRepoError(name string) error {
	return fmt.Errorf("%s is the repository this project lives in; it is already the project's first repository, so it cannot be linked or unlinked", name)
}

// nameCollisionError is what a caller reads when a repository's name would take the host's
// row, though the folder is another one.
func nameCollisionError(e registry.Entry, name string) error {
	return fmt.Errorf("%q is filed as %q, the name of the repository this project lives in; use another name", name, hostName(e))
}

// hasRepo reports whether e's identity file already names a repository name.
func hasRepo(e registry.Entry, name string) bool {
	for _, r := range e.Repos {
		if r.Name == name {
			return true
		}
	}
	return false
}

// checkContainment refuses a path that sits inside the vault but not under repos/<name>:
// a repository goes under repos/ or outside the vault entirely.
func checkContainment(e registry.Entry, name, path string) error {
	if _, ok := under(e.Path, path); ok && path != e.RepoDir(name) {
		return errors.New("a repository goes under repos/ or outside the vault")
	}
	return nil
}

// alreadyHasRepo is what a caller reads when a project already names that repository.
func alreadyHasRepo(e registry.Entry, name string) error {
	return fmt.Errorf("%s already has a repository named %q", e.Name, name)
}

// checkRepoTarget validates a new repository before it is recorded: its name must be
// unique on e, and a path that sits inside the vault must sit under repos/. recordRepo
// checks the name again against the identity file, which is the one that decides.
func checkRepoTarget(e registry.Entry, name, path string) error {
	if isHostFolder(e, path) {
		return hostRepoError(hostName(e))
	}
	if takesHostRow(e, name) {
		return nameCollisionError(e, name)
	}
	if hasRepo(e, name) {
		return alreadyHasRepo(e, name)
	}
	return checkContainment(e, name, path)
}

// recordRepo appends name's repository to e's identity file, and, when path is not the
// project's own repos/<name>, records the path in the config too. It checks the name
// against the file itself, since e may be older than the file.
func recordRepo(h home.Home, cfg *home.Config, e registry.Entry, name, path, remote string, now time.Time) (vault.Repo, string, error) {
	repo := vault.Repo{Name: name, Remote: remote}
	if err := vault.UpdateConfig(e.Path, "add repository "+name, now, func(c *vault.Config) error {
		for _, r := range c.Repos {
			if strings.EqualFold(r.Name, name) {
				return alreadyHasRepo(e, name)
			}
		}
		c.Repos = append(c.Repos, repo)
		return nil
	}); err != nil {
		return vault.Repo{}, "", err
	}
	if path != e.RepoDir(name) {
		cfg.SetRepoPath(e.ID, name, path)
		if err := h.Save(cfg); err != nil {
			return repo, path, err
		}
	}
	return repo, path, nil
}

// AddRepo links a folder to a project: under <project>/repos/ it needs no config entry;
// elsewhere the config records its path. A folder that is not a git repository is
// refused with NotRepoError unless initGit is set. It returns the identity entry and the
// path.
func AddRepo(h home.Home, cfg *home.Config, e registry.Entry, target string, initGit bool, now time.Time) (vault.Repo, string, error) {
	if err := requireProject(e); err != nil {
		return vault.Repo{}, "", err
	}
	path, err := filepath.Abs(home.Expand(target))
	if err != nil {
		return vault.Repo{}, "", err
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return vault.Repo{}, "", fmt.Errorf("%s is not a directory", home.Display(path))
	}
	name := links.CleanName(filepath.Base(path))
	if name == "" {
		return vault.Repo{}, "", fmt.Errorf("%s leaves no usable repository name", home.Display(path))
	}
	if err := checkRepoTarget(e, name, path); err != nil {
		return vault.Repo{}, "", err
	}
	if !links.IsRepo(path) {
		if !initGit {
			return vault.Repo{}, "", &NotRepoError{Path: path}
		}
		if err := links.InitRepo(path, name); err != nil {
			return vault.Repo{}, "", err
		}
	}
	return recordRepo(h, cfg, e, name, path, links.RemoteURL(path), now)
}

// CreateRepo makes a repository at <project>/repos/<name>, or at `at`, and links it.
func CreateRepo(h home.Home, cfg *home.Config, e registry.Entry, name, at string, now time.Time) (vault.Repo, string, error) {
	if err := requireProject(e); err != nil {
		return vault.Repo{}, "", err
	}
	name = links.CleanName(name)
	if name == "" {
		return vault.Repo{}, "", errors.New("the repository needs a name")
	}
	dir := e.RepoDir(name)
	if at != "" {
		abs, err := filepath.Abs(home.Expand(at))
		if err != nil {
			return vault.Repo{}, "", err
		}
		dir = abs
	}
	if err := checkRepoTarget(e, name, dir); err != nil {
		return vault.Repo{}, "", err
	}
	if err := links.CreateRepo(dir, name); err != nil {
		return vault.Repo{}, "", err
	}
	return recordRepo(h, cfg, e, name, dir, links.RemoteURL(dir), now)
}

// CloneRepo clones url into <project>/repos/<name>, or at `at`, and links it with the
// remote recorded.
func CloneRepo(h home.Home, cfg *home.Config, e registry.Entry, url, at string, now time.Time) (vault.Repo, string, error) {
	if err := requireProject(e); err != nil {
		return vault.Repo{}, "", err
	}
	url = strings.TrimSpace(url)
	name := links.NameFromURL(url)
	if name == "" {
		return vault.Repo{}, "", fmt.Errorf("cannot tell a repository name from %q", url)
	}
	dir := e.RepoDir(name)
	if at != "" {
		abs, err := filepath.Abs(home.Expand(at))
		if err != nil {
			return vault.Repo{}, "", err
		}
		dir = abs
	}
	if err := checkRepoTarget(e, name, dir); err != nil {
		return vault.Repo{}, "", err
	}
	if err := links.Clone(url, dir); err != nil {
		return vault.Repo{}, "", err
	}
	return recordRepo(h, cfg, e, name, dir, url, now)
}

// RemoveRepo drops a repository from the identity file and the config; the folder stays.
func RemoveRepo(h home.Home, cfg *home.Config, e registry.Entry, name string, now time.Time) error {
	if err := requireProject(e); err != nil {
		return err
	}
	if isHost(e, name) {
		return hostRepoError(name)
	}
	if !hasRepo(e, name) {
		return fmt.Errorf("%s has no repository named %q", e.Name, name)
	}
	if err := vault.UpdateConfig(e.Path, "remove repository "+name, now, func(c *vault.Config) error {
		var keep []vault.Repo
		for _, r := range c.Repos {
			if r.Name != name {
				keep = append(keep, r)
			}
		}
		c.Repos = keep
		return nil
	}); err != nil {
		return err
	}
	if cfg.RepoPath(e.ID, name) != "" {
		cfg.SetRepoPath(e.ID, name, "")
		if err := h.Save(cfg); err != nil {
			return err
		}
	}
	return nil
}

// RepoEdit changes a repository's identity entry. Remote and Changes point at "" to
// clear; Path moves the entry to another folder.
type RepoEdit struct {
	Remote  *string // "" clears
	Changes *string // pr, commit, or "" for the default
	Path    string  // point the entry at another folder
}

// EditRepo changes a repository's remote, change policy, or the folder it links.
func EditRepo(h home.Home, cfg *home.Config, e registry.Entry, name string, edit RepoEdit, now time.Time) (vault.Repo, error) {
	if err := requireProject(e); err != nil {
		return vault.Repo{}, err
	}
	// The host's name is derived, so an edit of it takes the name the scan derived,
	// whatever the caller typed; the identity entry must fold back into that row.
	if isHost(e, name) {
		name = hostName(e)
	}
	if !hasRepo(e, name) {
		return vault.Repo{}, fmt.Errorf("%s has no repository named %q", e.Name, name)
	}
	if edit.Changes != nil && *edit.Changes != "" && !contains(links.Policies, *edit.Changes) {
		return vault.Repo{}, fmt.Errorf("changes must be %s, %s, or empty", links.ChangesPR, links.ChangesCommit)
	}
	newPath := ""
	if edit.Path != "" {
		if isHost(e, name) {
			return vault.Repo{}, errors.New("the repository this project lives in has no separate path")
		}
		abs, err := filepath.Abs(home.Expand(edit.Path))
		if err != nil {
			return vault.Repo{}, err
		}
		if err := checkContainment(e, name, abs); err != nil {
			return vault.Repo{}, err
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return vault.Repo{}, fmt.Errorf("%s is not a directory", home.Display(abs))
		}
		if !links.IsRepo(abs) {
			return vault.Repo{}, &NotRepoError{Path: abs}
		}
		newPath = abs
	}
	apply := func(r *vault.Repo) {
		if edit.Remote != nil {
			r.Remote = *edit.Remote
		}
		if edit.Changes != nil {
			r.Changes = *edit.Changes
		}
	}
	// An entry under the host's own folder name stands for the host too, as it does in the
	// scan, so an edit changes it instead of adding a second entry for one folder.
	host := isHost(e, name)
	mine := func(entry string) bool { return entry == name || (host && isHost(e, entry)) }
	var updated vault.Repo
	if err := vault.UpdateConfig(e.Path, "edit repository "+name, now, func(c *vault.Config) error {
		found := false
		for i := range c.Repos {
			if !mine(c.Repos[i].Name) {
				continue
			}
			found = true
			apply(&c.Repos[i])
			updated = c.Repos[i]
		}
		// The host repository is derived, so the identity file names it only once an edit
		// has something to record about it.
		if !found && host {
			repo := vault.Repo{Name: name}
			apply(&repo)
			updated = repo
			if repo != (vault.Repo{Name: name}) {
				c.Repos = append(c.Repos, repo)
			}
		}
		return nil
	}); err != nil {
		return vault.Repo{}, err
	}
	if newPath != "" {
		target := newPath
		if target == e.RepoDir(name) {
			target = ""
		}
		cfg.SetRepoPath(e.ID, name, target)
		if err := h.Save(cfg); err != nil {
			return updated, err
		}
	}
	return updated, nil
}
