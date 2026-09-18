// Package describe holds the facts that tie a project to its knowledge base: the page
// that describes it, the commit that page was written from, and how far the work has
// moved since. It reads pages and git and writes nothing; the describe skill writes the
// page, and capture writes the snapshot it cites.
package describe

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// EntityType is the entity_type a page describing a project carries.
const EntityType = "project"

// BehindThreshold is how many commits a repository may move past its page before status
// says the page fell behind.
const BehindThreshold = 20

// ClaudeMD returns the path of the work's CLAUDE.md, or "" when it has none.
func ClaudeMD(work string) string {
	p := filepath.Join(work, "CLAUDE.md")
	if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
		return p
	}
	return ""
}

// Page finds the page that describes project e in its knowledge base: an entity page
// with `entity_type: project` whose `project` property is e's id or name. Among several
// the one nearest HEAD wins. nil when the project uses no knowledge base or no page
// matches.
func Page(e registry.Entry) *registry.Description {
	kb := e.KnowledgePath()
	if kb == "" {
		return nil
	}
	found := matches(filepath.Join(kb, vault.WikiDir), e)
	if len(found) == 0 {
		return nil
	}
	var git *gitx.Repo
	var skip []string
	if repo := (gitx.Repo{Dir: e.Path}); repo.IsRepo() {
		git = &repo
		skip = KnowledgeDirs(repo)
	}
	best := -1
	for i := range found {
		found[i].Behind = behind(git, found[i].Commit, skip)
		if best < 0 || nearer(found[i], found[best]) {
			best = i
		}
	}
	return &found[best]
}

// nearer prefers a page whose commit is in the history, then the one fewest commits
// behind, then the lexically first path, so two runs pick alike.
func nearer(a, b registry.Description) bool {
	switch {
	case (a.Behind >= 0) != (b.Behind >= 0):
		return a.Behind >= 0
	case a.Behind != b.Behind:
		return a.Behind < b.Behind
	}
	return a.Page < b.Page
}

// KnowledgeDirs lists the folders in the work that hold a knowledge base, by its tracked
// identity file. A knowledge base inside the work commits into the work's repository, and
// its files and commits are not the work.
func KnowledgeDirs(git gitx.Repo) []string {
	markers, _ := git.Named(vault.Marker)
	var dirs []string
	for _, m := range markers {
		if dir := path.Dir(m); dir != "." {
			dirs = append(dirs, dir+"/")
		}
	}
	return dirs
}

func behind(git *gitx.Repo, commit string, skip []string) int {
	if git == nil || commit == "" || !git.HasCommit(commit) {
		return -1
	}
	n, err := git.Behind(commit, skip...)
	if err != nil {
		return -1
	}
	return n
}

// matches walks one wiki for pages that describe e.
func matches(wiki string, e registry.Entry) []registry.Description {
	var out []registry.Description
	filepath.WalkDir(wiki, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != wiki && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		content, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(content), EntityType) {
			return nil
		}
		fields, _, err := vault.Frontmatter(string(content))
		if err != nil || fields == nil {
			return nil
		}
		if vault.StringField(fields, "type") != "entity" || vault.StringField(fields, "entity_type") != EntityType {
			return nil
		}
		if !names(vault.StringField(fields, "project"), e) {
			return nil
		}
		rel, err := filepath.Rel(filepath.Dir(wiki), p)
		if err != nil {
			return nil
		}
		out = append(out, registry.Description{Page: path.Clean(filepath.ToSlash(rel)), Commit: vault.StringField(fields, "commit")})
		return nil
	})
	return out
}

// names reports whether a page's project property means e: its id, or its name.
func names(prop string, e registry.Entry) bool {
	prop = strings.TrimSpace(prop)
	if prop == "" {
		return false
	}
	return prop == e.ID || strings.EqualFold(prop, e.Name)
}
