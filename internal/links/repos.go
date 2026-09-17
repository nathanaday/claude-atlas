package links

import (
	"os"
	"regexp"
	"strings"

	"github.com/nathanaday/claude-atlas/internal/gitx"
)

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

var badNameChars = regexp.MustCompile(`[\\/:*?"<>|#^\[\]]+`)

// CleanName makes a page name safe for Obsidian and for wikilinks; empty when nothing
// usable is left.
func CleanName(name string) string {
	return strings.Trim(badNameChars.ReplaceAllString(name, "-"), " .-")
}
