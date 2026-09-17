package vaults

import (
	"os"
	"path/filepath"
	"time"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// AdoptRepos links every git repository sitting under <project>/repos/ that the identity
// file does not already name, so moving a repository into the folder is the whole
// gesture. Each adoption is one `add repository` operation and takes the atlas's default
// change policy. A folder that is not a git repository, or whose name is already taken,
// is skipped in silence: repos/ is the user's to arrange.
func AdoptRepos(h home.Home, cfg *home.Config, e registry.Entry, now time.Time) ([]vault.Repo, error) {
	if err := requireProject(e); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(e.Path, vault.ReposDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var adopted []vault.Repo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := links.CleanName(entry.Name())
		if name == "" || name != entry.Name() {
			// A folder Obsidian cannot name is left alone; linking it would file it
			// under a name that no longer points at the folder.
			continue
		}
		path := e.RepoDir(name)
		if !links.IsRepo(path) {
			continue
		}
		if err := checkRepoTarget(e, name, path); err != nil {
			continue
		}
		repo, _, err := recordRepo(h, cfg, e, name, path, links.RemoteURL(path), now)
		if err != nil {
			return adopted, err
		}
		e.Repos = append(e.Repos, registry.Repo{Name: repo.Name, Path: path, Remote: repo.Remote, Changes: repo.Changes})
		adopted = append(adopted, repo)
	}
	return adopted, nil
}
