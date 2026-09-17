// Package repomap holds the facts that tie a repository to the wiki: the page that
// describes it, the commit that page was written from, and how far the repository has
// moved since. It reads pages and git and writes nothing.
package repomap

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

// EntityType is the entity_type a page describing a repository carries.
const EntityType = "repository"

// BehindThreshold is how many commits a repository may move past its page before status
// says the page fell behind.
const BehindThreshold = 20

// ClaudeMD returns the path of the repository's CLAUDE.md, or "" when it has none.
func ClaudeMD(r registry.Repo) string {
	if r.Path == "" {
		return ""
	}
	p := filepath.Join(r.Path, "CLAUDE.md")
	if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
		return p
	}
	return ""
}

// Describe finds the page that describes repository r: an entity page with
// `entity_type: repository` whose `repo` property is r's remote or r's name, in the
// project's own wiki or in a mounted knowledge base. A page in a knowledge base wins over
// one in the project, and among those the one nearest HEAD. nil when no page matches.
func Describe(project registry.Entry, r registry.Repo) *registry.RepoDescription {
	var found []registry.RepoDescription
	for _, d := range matches(filepath.Join(project.Path, vault.WikiDir), project.Name, r) {
		found = append(found, d)
	}
	var inMount []registry.RepoDescription
	for _, m := range project.Mounts {
		if m.Error != "" || m.Path == "" {
			continue
		}
		inMount = append(inMount, matches(m.Path, m.Name, r)...)
	}
	if len(inMount) > 0 {
		found = inMount
	}
	if len(found) == 0 {
		return nil
	}
	var git *gitx.Repo
	if r.Path != "" {
		git = &gitx.Repo{Dir: r.Path}
	}
	best := -1
	for i := range found {
		found[i].Behind = behind(git, found[i].Commit)
		if best < 0 || nearer(found[i], found[best]) {
			best = i
		}
	}
	return &found[best]
}

// nearer prefers a page whose commit is in the history, then the one fewest commits
// behind, then the lexically first path, so two runs pick alike.
func nearer(a, b registry.RepoDescription) bool {
	switch {
	case (a.Behind >= 0) != (b.Behind >= 0):
		return a.Behind >= 0
	case a.Behind != b.Behind:
		return a.Behind < b.Behind
	}
	return a.Page < b.Page
}

func behind(git *gitx.Repo, commit string) int {
	if git == nil || commit == "" || !git.HasCommit(commit) {
		return -1
	}
	n, err := git.Behind(commit)
	if err != nil {
		return -1
	}
	return n
}

// matches walks one wiki for pages that describe r. wiki is the wiki's absolute path;
// in names the vault for the description.
func matches(wiki, in string, r registry.Repo) []registry.RepoDescription {
	var out []registry.RepoDescription
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
		if !names(vault.StringField(fields, "repo"), r) {
			return nil
		}
		rel, err := filepath.Rel(filepath.Dir(wiki), p)
		if err != nil {
			return nil
		}
		out = append(out, registry.RepoDescription{Page: path.Clean(filepath.ToSlash(rel)), In: in, Commit: vault.StringField(fields, "commit")})
		return nil
	})
	return out
}

// names reports whether a page's repo property means r: its remote, or its name.
func names(prop string, r registry.Repo) bool {
	prop = strings.TrimSpace(prop)
	if prop == "" {
		return false
	}
	if r.Remote != "" && sameRemote(prop, r.Remote) {
		return true
	}
	return prop == r.Name
}

// sameRemote compares two remotes loosely: scheme, a trailing .git, and case do not
// matter, and git@host:path equals host/path.
func sameRemote(a, b string) bool {
	return canon(a) == canon(b)
}

func canon(remote string) string {
	s := strings.TrimSpace(strings.ToLower(remote))
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://"} {
		s = strings.TrimPrefix(s, prefix)
	}
	if rest, ok := strings.CutPrefix(s, "git@"); ok {
		s = strings.Replace(rest, ":", "/", 1)
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	return s
}
