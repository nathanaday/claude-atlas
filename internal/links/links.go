// Package links inspects the git repositories a project points at besides its vault.
// Atlas only reads them.
package links

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/home"
)

const Repo = "repo"

// Link is the derived view of one linked repository.
type Link struct {
	Kind  string `json:"kind"`
	Name  string `json:"name,omitempty"` // the link page, when the folder has one
	Path  string `json:"path"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// Repo facts.
	Branch     string `json:"branch,omitempty"`
	LastCommit string `json:"last_commit,omitempty"`
	Dirty      *int   `json:"dirty,omitempty"`
}

// Touched is the latest date the link shows activity on, if any.
func (l Link) Touched() (time.Time, bool) {
	if t, err := time.ParseInLocation("2006-01-02", l.LastCommit, time.Local); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// Inspect derives the facts for one linked path.
func Inspect(kind, path string) Link {
	link := Link{Kind: kind, Path: path}
	abs := home.Expand(path)
	info, err := os.Stat(abs)
	if err != nil {
		link.Error = "not found"
		return link
	}
	if !info.IsDir() {
		link.Error = "not a directory"
		return link
	}
	link.OK = true
	inspectRepo(&link, abs)
	return link
}

func git(dir string, args ...string) (string, bool) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return strings.TrimSpace(out.String()), true
}

func inspectRepo(link *Link, dir string) {
	if _, err := exec.LookPath("git"); err != nil {
		return
	}
	if _, ok := git(dir, "rev-parse", "--is-inside-work-tree"); !ok {
		link.OK = false
		link.Error = "not a git repository"
		return
	}
	if branch, ok := git(dir, "rev-parse", "--abbrev-ref", "HEAD"); ok {
		link.Branch = branch
	}
	if date, ok := git(dir, "log", "-1", "--format=%cs"); ok {
		link.LastCommit = date
	}
	if status, ok := git(dir, "status", "--porcelain"); ok {
		n := 0
		if status != "" {
			n = strings.Count(status, "\n") + 1
		}
		link.Dirty = &n
	}
}
