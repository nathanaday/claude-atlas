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

// Unregister removes a vault from the config. A vault inside the vaults directory cannot
// be forgotten: the scan finds it; the error says to move or delete the folder.
func Unregister(h home.Home, cfg *home.Config, root string) error {
	abs, err := filepath.Abs(home.Expand(root))
	if err != nil {
		return err
	}
	if cfg.Inside(abs) {
		return fmt.Errorf("%s is inside the vaults directory; the scan finds it there, so move or delete the folder to forget it", home.Display(abs))
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

// EditIdentity changes a vault's identity file, one field at a time. vault.UpdateConfig
// validates each field against the vault's kind before it writes anything, so a field
// that does not belong to e's kind (scope on a project, tags on a knowledge base) fails
// without touching the file.
func EditIdentity(e registry.Entry, edit Edit, now time.Time) error {
	if edit.Name != "" {
		if err := vault.UpdateConfig(e.Path, "edit name", now, func(c *vault.Config) error {
			c.Name = edit.Name
			return nil
		}); err != nil {
			return err
		}
	}
	if edit.Tags != nil {
		tags := cleanTags(*edit.Tags)
		if err := vault.UpdateConfig(e.Path, "edit tags", now, func(c *vault.Config) error {
			c.Tags = tags
			return nil
		}); err != nil {
			return err
		}
	}
	if edit.Scope != nil {
		if err := vault.UpdateConfig(e.Path, "edit scope", now, func(c *vault.Config) error {
			c.Scope = *edit.Scope
			return nil
		}); err != nil {
			return err
		}
	}
	if edit.Access != nil {
		if err := vault.UpdateConfig(e.Path, "edit access", now, func(c *vault.Config) error {
			c.Access = *edit.Access
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// requireProject refuses a repository operation on a knowledge base.
func requireProject(e registry.Entry) error {
	if e.Kind != vault.Project {
		return errors.New("a knowledge base has no repositories")
	}
	return nil
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

// checkRepoTarget validates a new repository before it is recorded: its name must be
// unique on e, and a path that sits inside the vault must sit under repos/.
func checkRepoTarget(e registry.Entry, name, path string) error {
	if hasRepo(e, name) {
		return fmt.Errorf("%s already has a repository named %q", e.Name, name)
	}
	if _, ok := under(e.Path, path); ok && path != e.RepoDir(name) {
		return errors.New("a repository goes under repos/ or outside the vault")
	}
	return nil
}

// recordRepo appends name's repository to e's identity file, and, when path is not the
// project's own repos/<name>, records the path in the config too.
func recordRepo(h home.Home, cfg *home.Config, e registry.Entry, name, path, remote string, now time.Time) (vault.Repo, string, error) {
	repo := vault.Repo{Name: name, Remote: remote}
	if err := vault.UpdateConfig(e.Path, "add repository "+name, now, func(c *vault.Config) error {
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

// AddRepo mounts a folder on a project: under <project>/repos/ it needs no config entry;
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

// CreateRepo makes a repository at <project>/repos/<name>, or at `at`, and mounts it.
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

// CloneRepo clones url into <project>/repos/<name>, or at `at`, and mounts it with the
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

// EditRepo changes a repository's remote, change policy, or the folder it mounts.
func EditRepo(h home.Home, cfg *home.Config, e registry.Entry, name string, edit RepoEdit, now time.Time) (vault.Repo, error) {
	if err := requireProject(e); err != nil {
		return vault.Repo{}, err
	}
	if !hasRepo(e, name) {
		return vault.Repo{}, fmt.Errorf("%s has no repository named %q", e.Name, name)
	}
	if edit.Changes != nil && *edit.Changes != "" && !contains(links.Policies, *edit.Changes) {
		return vault.Repo{}, fmt.Errorf("changes must be %s, %s, or empty", links.ChangesPR, links.ChangesCommit)
	}
	newPath := ""
	if edit.Path != "" {
		abs, err := filepath.Abs(home.Expand(edit.Path))
		if err != nil {
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
	var updated vault.Repo
	if err := vault.UpdateConfig(e.Path, "edit repository "+name, now, func(c *vault.Config) error {
		for i := range c.Repos {
			if c.Repos[i].Name != name {
				continue
			}
			if edit.Remote != nil {
				c.Repos[i].Remote = *edit.Remote
			}
			if edit.Changes != nil {
				c.Repos[i].Changes = *edit.Changes
			}
			updated = c.Repos[i]
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
