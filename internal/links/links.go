// Package links inspects the folders a project points at besides its vault: git
// repositories and directories of static material. Atlas only reads them.
package links

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/home"
)

const (
	Repo      = "repo"
	Materials = "materials"
)

// Link is the derived view of one linked folder.
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
	// Materials facts.
	Files  *int   `json:"files,omitempty"`
	Bytes  *int64 `json:"bytes,omitempty"`
	Newest string `json:"newest,omitempty"`
}

// Touched is the latest date the link shows activity on, if any.
func (l Link) Touched() (time.Time, bool) {
	for _, s := range []string{l.LastCommit, l.Newest} {
		if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// DetectKind says what a path is: a repo when it is a directory holding .git, else materials.
func DetectKind(path string) string {
	if _, err := os.Stat(filepath.Join(home.Expand(path), ".git")); err == nil {
		return Repo
	}
	return Materials
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
		if kind == Repo {
			link.Error = "not a directory"
			return link
		}
		// A single file is material too: one file, its size, its date.
		link.OK = true
		one, size := 1, info.Size()
		link.Files, link.Bytes, link.Newest = &one, &size, info.ModTime().Format("2006-01-02")
		return link
	}
	link.OK = true
	switch kind {
	case Repo:
		inspectRepo(&link, abs)
	default:
		inspectMaterials(&link, abs)
	}
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

func inspectMaterials(link *Link, dir string) {
	files, size := 0, int64(0)
	var newest time.Time
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files++
		size += info.Size()
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	link.Files = &files
	link.Bytes = &size
	if files > 0 {
		link.Newest = newest.Format("2006-01-02")
	}
}

// HumanBytes renders a size the way a file manager would.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + string("KMGTPE"[exp]) + "B"
}
