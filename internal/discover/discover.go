// Package discover finds the project a directory belongs to through the registry: the
// project whose repository holds the directory. It lets a session started in a
// repository reach its vault without any file in the repository.
package discover

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// Match is a project whose repository holds the directory.
type Match struct {
	Project registry.Entry
	Repo    registry.Repo
}

// Vault looks a directory up in the registry. One repository holding dir gives a match;
// several equally specific ones give them all as candidates and no match; no atlas, or
// no repository holding dir, gives nothing.
func Vault(h home.Home, dir string) (*Match, []Match, error) {
	cfg, err := h.Load()
	if errors.Is(err, home.ErrNoAtlas) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, err
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		return nil, nil, err
	}
	var matches []Match
	best := -1
	for _, e := range ix.Projects() {
		for _, r := range e.Repos {
			if r.Path == "" || !inside(r.Path, abs) {
				continue
			}
			n := len(r.Path)
			switch {
			case n > best:
				best = n
				matches = []Match{{Project: e, Repo: r}}
			case n == best:
				matches = append(matches, Match{Project: e, Repo: r})
			}
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil, nil
	}
	if len(matches) > 1 {
		return nil, matches, nil
	}
	return nil, nil, nil
}

func inside(root, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// Repos lists the repositories of the project at root.
func Repos(h home.Home, root string) ([]registry.Repo, error) {
	cfg, err := h.Load()
	if errors.Is(err, home.ErrNoAtlas) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		return nil, err
	}
	e := ix.ByPath(root)
	if e == nil || e.Error != "" || e.Kind != vault.Project {
		return nil, nil
	}
	return e.Repos, nil
}

// Describe names candidates for an error or a hint.
func Describe(candidates []Match) string {
	var parts []string
	for _, m := range candidates {
		parts = append(parts, fmt.Sprintf("%s (%s)", m.Project.Name, m.Project.Path))
	}
	return strings.Join(parts, ", ")
}
