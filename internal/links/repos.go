package links

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
)

// Change policies.
const (
	ChangesPR     = "pr"
	ChangesCommit = "commit"
)

// Policies lists the change policies a page may carry.
var Policies = []string{ChangesPR, ChangesCommit}

// Policy is how sessions land changes: the recorded policy, else pr when there is a
// remote and commit when there is none.
func Policy(changes, remote string) string {
	if changes != "" {
		return changes
	}
	if remote != "" {
		return ChangesPR
	}
	return ChangesCommit
}

// PolicyText says what a policy means, for a session.
func PolicyText(policy string) string {
	if policy == ChangesPR {
		return "changes land as pull requests: work on a branch, commit there, and open a pull request; never push to the default branch"
	}
	return "changes land as commits on the current branch"
}

// RemoteURL is the origin remote of the repository at path, or "".
func RemoteURL(path string) string {
	return gitx.Repo{Dir: path}.RemoteURL()
}

// IsRepo reports whether a folder is the top of a git working tree.
func IsRepo(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	return gitx.Repo{Dir: path}.IsRepo()
}

// InitRepo makes an existing folder a git repository with one commit of what it holds,
// or of a README when it is empty.
func InitRepo(path, name string) error {
	if !gitx.Available() {
		return fmt.Errorf("git is required and is not on PATH")
	}
	repo := gitx.Repo{Dir: path}
	if repo.IsRepo() {
		return nil
	}
	if err := repo.Init(); err != nil {
		return err
	}
	if err := repo.AddAll(); err != nil {
		return err
	}
	staged, err := repo.Status()
	if err != nil {
		return err
	}
	subject := "initial: existing files"
	if len(staged) == 0 {
		// Nothing git can track yet: a README gives the repository its first commit.
		if err := os.WriteFile(filepath.Join(path, "README.md"), []byte(readme(name)), 0o644); err != nil {
			return err
		}
		if err := repo.AddAll(); err != nil {
			return err
		}
		subject = "initial"
	}
	_, err = repo.Commit(subject)
	return err
}

// CreateRepo makes a new git repository at dir, which must not exist or must be empty.
func CreateRepo(dir, name string) error {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty", home.Display(dir))
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return InitRepo(dir, name)
}

func readme(name string) string {
	return "# " + name + "\n\nDeliverables live here. The knowledge behind them lives in the claude-atlas vault this repository is linked to.\n"
}

// IsRemoteURL reports whether s names a repository to clone rather than a folder: an
// https, ssh, git, or file URL, or an scp-style git@host:path.
func IsRemoteURL(s string) bool {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://", "file://"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return strings.HasPrefix(s, "git@") && strings.Contains(s, ":")
}

// NameFromURL is the repository's name as a clone would name its folder.
func NameFromURL(url string) string {
	s := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(url), "/"), ".git")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	return CleanName(s)
}

// Clone clones url into dir, which must not exist or must be empty.
func Clone(url, dir string) error {
	if !gitx.Available() {
		return fmt.Errorf("git is required and is not on PATH")
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty", home.Display(dir))
	}
	return gitx.Repo{Dir: dir}.Clone(url)
}

var badNameChars = regexp.MustCompile(`[\\/:*?"<>|#^\[\]]+`)

// CleanName makes a page name safe for Obsidian and for wikilinks; empty when nothing
// usable is left.
func CleanName(name string) string {
	return strings.Trim(badNameChars.ReplaceAllString(name, "-"), " .-")
}
