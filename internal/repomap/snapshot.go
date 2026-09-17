package repomap

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

// SnapshotType is the type property of a snapshot file, which tells the ingest skills it
// belongs to repo-map.
const SnapshotType = "repo-snapshot"

// MaxListedFiles is how many tracked paths a snapshot lists one by one; past it, the
// snapshot lists folders only.
const MaxListedFiles = 2000

// MaxLoggedCommits caps the log a snapshot carries since the previous one.
const MaxLoggedCommits = 200

// Snapshot is what the atlas captures of a repository: a small markdown file, written at
// one commit, that a page about the repository can cite. The code itself stays in its
// own git.
type Snapshot struct {
	Repo   string // the remote, or the name when there is none
	Name   string
	Commit string
	Branch string
	Since  string // the commit the previous snapshot was taken at, or ""
	Taken  time.Time
	// FileName is the inbox name: <name>-<short commit>.md.
	FileName string
	Content  []byte
}

// TakeSnapshot reads repository r at HEAD. since, when it names a commit in the history,
// adds the log from it to HEAD.
func TakeSnapshot(r registry.Repo, since string, now time.Time) (*Snapshot, error) {
	if r.Path == "" {
		return nil, fmt.Errorf("repository %s has no folder", r.Name)
	}
	git := gitx.Repo{Dir: r.Path}
	if !git.IsRepo() {
		return nil, fmt.Errorf("%s is not a git repository", r.Path)
	}
	if !git.HasHead() {
		return nil, fmt.Errorf("repository %s has no commits yet", r.Name)
	}
	head, err := git.Head()
	if err != nil {
		return nil, err
	}
	branch, _ := git.Branch()
	files, err := git.LsFiles()
	if err != nil {
		return nil, err
	}
	s := &Snapshot{Repo: r.Remote, Name: r.Name, Commit: head, Branch: branch, Taken: now}
	if s.Repo == "" {
		s.Repo = r.Name
	}
	if since != "" && since != head && git.HasCommit(since) {
		s.Since = since
	}
	s.FileName = fmt.Sprintf("%s-%s.md", r.Name, head[:7])

	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: \"%s at %s\"\ntype: %s\nrepo: %s\nname: %s\ncommit: %s\nbranch: %s\ntaken: %s\nsince: \"%s\"\n---\n\n",
		r.Name, head[:7], SnapshotType, s.Repo, r.Name, head, branch, now.Format("2006-01-02"), s.Since)
	fmt.Fprintf(&b, "# %s at %s\n\nA snapshot of the repository %s at commit %s on branch %s, taken %s by claude-atlas. The code is in the repository; this file is what a page about it cites.\n",
		r.Name, head[:7], s.Repo, head, branch, now.Format("2006-01-02"))
	for _, name := range []string{"CLAUDE.md", readmeName(files)} {
		if name == "" {
			continue
		}
		if text, err := os.ReadFile(filepath.Join(r.Path, name)); err == nil {
			// A four-backtick fence keeps the file verbatim, its own fences included, and
			// its headings out of the snapshot's outline.
			fmt.Fprintf(&b, "\n## %s\n\n````markdown\n%s\n````\n", name, strings.TrimRight(string(text), "\n"))
		}
	}
	b.WriteString("\n## Files\n\n")
	if len(files) > MaxListedFiles {
		fmt.Fprintf(&b, "%d tracked files; folders only:\n\n", len(files))
		for _, d := range folders(files) {
			fmt.Fprintf(&b, "- %s/\n", d)
		}
	} else {
		fmt.Fprintf(&b, "%d tracked files:\n\n", len(files))
		for _, f := range files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	if docs := docHeadings(r.Path, files); len(docs) > 0 {
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

// folders lists the top two levels of folders the tracked files sit in.
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
