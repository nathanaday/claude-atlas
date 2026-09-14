// Package discover finds the vault a directory belongs to through the atlas: the
// project that links a folder holding the directory. It lets a session started in a
// repository reach its vault without any file in the repository.
package discover

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/tree"
)

// Match is a project whose linked folder holds the directory.
type Match struct {
	Project *tree.Project
	Vault   string
	Folder  string
}

// Vault looks a directory up in the atlas. One project linking a folder above dir gives
// a match; several give them all as candidates and no match; no atlas, or no project,
// gives nothing.
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
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return nil, nil, err
	}
	var matches []Match
	for _, p := range projects {
		var best string
		for _, l := range p.Linked {
			if l.Path != "" && inside(l.Path, abs) && len(l.Path) > len(best) {
				best = l.Path
			}
		}
		if best != "" {
			matches = append(matches, Match{Project: p, Vault: p.VaultPath(), Folder: best})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Project.Rel < matches[j].Project.Rel })
	if len(matches) == 1 {
		return &matches[0], nil, nil
	}
	return nil, matches, nil
}

func inside(root, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// Describe names candidates for an error or a hint.
func Describe(candidates []Match) string {
	var parts []string
	for _, m := range candidates {
		parts = append(parts, fmt.Sprintf("%s (%s)", m.Project.Name, m.Vault))
	}
	return strings.Join(parts, ", ")
}
