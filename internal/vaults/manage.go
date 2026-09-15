package vaults

import (
	"path/filepath"
	"strings"

	"github.com/nathanaday/claude-atlas/internal/home"
)

// NotRepoError says a folder exists but is not a git repository; the caller may ask
// the user and link again with init.
type NotRepoError struct{ Path string }

func (e *NotRepoError) Error() string {
	return home.Display(e.Path) + " is not a git repository; a link is a mounted repository (initialize one there, or pass --init)"
}

// under gives path relative to root when path is root or inside it.
func under(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
