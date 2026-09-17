package describe

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

// SnapshotType is the type property of a snapshot file, which tells the ingest skills it
// belongs to the describe skill.
const SnapshotType = "project-snapshot"

// MaxListedFiles is how many paths a snapshot lists one by one; past it, the snapshot
// lists folders only.
const MaxListedFiles = 2000

// MaxLoggedCommits caps the log a snapshot carries since the previous one.
const MaxLoggedCommits = 200

// MaxWalkedFiles bounds the walk of a work folder that is not a repository.
const MaxWalkedFiles = 20000

// Snapshot is what the atlas captures of a project: a small markdown file, written at
// one commit when the work is a repository, that a page about the project can cite. The
// work itself stays where it is.
type Snapshot struct {
	Project string // the project's id
	Name    string
	Commit  string // "" when the work is not a repository
	Branch  string
	Since   string // the commit the previous snapshot was taken at, or ""
	Taken   time.Time
	// FileName is the inbox name: <name>-<short commit>.md, or <name>-<date>.md.
	FileName string
	Content  []byte
}

// TakeSnapshot reads the work of project e: its tracked files at HEAD when it is a
// repository, else its files as they are. since, when it names a commit in the history,
// adds the log from it to HEAD.
func TakeSnapshot(e registry.Entry, since string, now time.Time) (*Snapshot, error) {
	if e.Path == "" {
		return nil, fmt.Errorf("project %s has no folder", e.Name)
	}
	s := &Snapshot{Project: e.ID, Name: e.Name, Taken: now}
	git := gitx.Repo{Dir: e.Path}
	var files []string
	var err error
	if git.IsRepo() && git.HasHead() {
		if s.Commit, err = git.Head(); err != nil {
			return nil, err
		}
		s.Branch, _ = git.Branch()
		if files, err = git.LsFiles(); err != nil {
			return nil, err
		}
		if since != "" && since != s.Commit && git.HasCommit(since) {
			s.Since = since
		}
		s.FileName = fmt.Sprintf("%s-%s.md", e.Name, s.Commit[:7])
	} else {
		if files, err = walkFiles(e.Path); err != nil {
			return nil, err
		}
		s.FileName = fmt.Sprintf("%s-%s.md", e.Name, now.Format("2006-01-02"))
	}
	// The project's own atlas/ folder is state, not work.
	files = without(files, project.Dir+"/")

	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: %q\ntype: %s\nproject: %s\nname: %q\ncommit: %q\nbranch: %q\ntaken: %s\nsince: %q\n---\n\n",
		snapshotTitle(s), SnapshotType, e.ID, e.Name, s.Commit, s.Branch, now.Format("2006-01-02"), s.Since)
	fmt.Fprintf(&b, "# %s\n\n", snapshotTitle(s))
	if s.Commit != "" {
		fmt.Fprintf(&b, "A snapshot of the project %s (id %s) at commit %s on branch %s, taken %s by claude-atlas. The work is in the repository; this file is what a page about it cites.\n", e.Name, e.ID, s.Commit, s.Branch, now.Format("2006-01-02"))
	} else {
		fmt.Fprintf(&b, "A snapshot of the project %s (id %s), a folder that is not a git repository, taken %s by claude-atlas. The work is in the folder; this file is what a page about it cites.\n", e.Name, e.ID, now.Format("2006-01-02"))
	}
	if e.Description != "" {
		fmt.Fprintf(&b, "\nThe project describes itself as: %s\n", e.Description)
	}
	for _, name := range []string{"CLAUDE.md", readmeName(files)} {
		if name == "" {
			continue
		}
		if text, err := os.ReadFile(filepath.Join(e.Path, name)); err == nil {
			// A four-backtick fence keeps the file verbatim, its own fences included, and
			// its headings out of the snapshot's outline.
			fmt.Fprintf(&b, "\n## %s\n\n````markdown\n%s\n````\n", name, strings.TrimRight(string(text), "\n"))
		}
	}
	b.WriteString("\n## Files\n\n")
	if len(files) > MaxListedFiles {
		fmt.Fprintf(&b, "%d files; folders only:\n\n", len(files))
		for _, d := range folders(files) {
			fmt.Fprintf(&b, "- %s/\n", d)
		}
	} else {
		fmt.Fprintf(&b, "%d files:\n\n", len(files))
		for _, f := range files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	if docs := docHeadings(e.Path, files); len(docs) > 0 {
		b.WriteString("\n## Docs\n\n")
		for _, d := range docs {
			b.WriteString("- " + d + "\n")
		}
	}
	if s.Since != "" {
		if log, err := git.LogStat(s.Since, MaxLoggedCommits); err == nil && strings.TrimSpace(log) != "" {
			fmt.Fprintf(&b, "\n## Changes since %s\n\n```text\n%s```\n", s.Since[:min(7, len(s.Since))], log)
		}
	}
	s.Content = []byte(b.String())
	return s, nil
}

func snapshotTitle(s *Snapshot) string {
	if s.Commit != "" {
		return fmt.Sprintf("%s at %s", s.Name, s.Commit[:7])
	}
	return fmt.Sprintf("%s on %s", s.Name, s.Taken.Format("2006-01-02"))
}

// walkFiles lists the regular files under a folder that is not a repository, skipping
// dot folders and the usual build state.
func walkFiles(root string) ([]string, error) {
	skip := map[string]bool{"node_modules": true, "build": true, "dist": true, "target": true, "__pycache__": true, "venv": true, ".venv": true}
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		if p == root {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || skip[name] {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, filepath.ToSlash(rel))
		if len(out) > MaxWalkedFiles {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func without(files []string, prefix string) []string {
	out := files[:0]
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) {
			out = append(out, f)
		}
	}
	return out
}

// readmeName is the README at the top level, whatever its spelling, or "".
func readmeName(files []string) string {
	for _, f := range files {
		if strings.Contains(f, "/") {
			continue
		}
		base := strings.ToLower(f)
		if base == "readme" || strings.HasPrefix(base, "readme.") {
			return f
		}
	}
	return ""
}

// folders lists the top two levels of folders the files sit in.
func folders(files []string) []string {
	seen := map[string]bool{}
	for _, f := range files {
		parts := strings.Split(f, "/")
		if len(parts) < 2 {
			continue
		}
		seen[parts[0]] = true
		if len(parts) > 2 {
			seen[parts[0]+"/"+parts[1]] = true
		}
	}
	var out []string
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// docHeadings pairs each markdown file under docs/ with its first heading.
func docHeadings(root string, files []string) []string {
	var out []string
	for _, f := range files {
		if !strings.HasPrefix(f, "docs/") || path.Ext(f) != ".md" {
			continue
		}
		heading := ""
		if text, err := os.ReadFile(filepath.Join(root, f)); err == nil {
			for _, line := range strings.SplitN(string(text), "\n", 200) {
				if strings.HasPrefix(line, "# ") {
					heading = strings.TrimSpace(line[2:])
					break
				}
			}
		}
		if heading == "" {
			out = append(out, f)
		} else {
			out = append(out, f+" — "+heading)
		}
	}
	return out
}
