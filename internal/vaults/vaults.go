// Package vaults creates and adopts vaults and registers them with the atlas.
package vaults

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var ErrCancelled = errors.New("cancelled")

// CheckNewPath says why a new vault cannot go at path: something is there already, or
// the path is inside another vault, whose git would take in the new vault's files.
func CheckNewPath(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; adopt it if it is a vault", home.Display(path))
	}
	if outer := vault.FindAbove(filepath.Dir(path)); outer != "" {
		return fmt.Errorf("%s is inside the vault %s; choose another category or path", home.Display(path), home.Display(outer))
	}
	return nil
}

// Create makes a new vault at path after showing what it will contain.
func Create(path string, opts vault.Options, c *console.Console, confirm bool) (*vault.InitResult, error) {
	if err := CheckNewPath(path); err != nil {
		return nil, err
	}
	if opts.Mode == "" {
		opts.Mode = vault.Generic
	}
	if confirm {
		if err := preview(c, path, opts, "and a git repository"); err != nil {
			return nil, err
		}
	}
	return vault.Init(path, opts, time.Now())
}

// CreateIn makes a project inside the repository at repoRoot, at REPO/atlas/, and
// returns the vault's path. It refuses a folder that is not a repository's top level.
func CreateIn(repoRoot string, opts vault.Options, c *console.Console) (string, error) {
	host, err := filepath.Abs(home.Expand(repoRoot))
	if err != nil {
		return "", err
	}
	path := filepath.Join(host, vault.InRepoDir)
	if err := CheckNewPath(path); err != nil {
		return "", err
	}
	// InitIn refuses a folder that is not a repository too; here the refusal comes before
	// the preview, so nothing describes a project the repository cannot hold.
	if !links.IsRepo(host) {
		return "", fmt.Errorf("%s is not the top level of a git repository", home.Display(host))
	}
	if opts.Mode == "" {
		opts.Mode = vault.Generic
	}
	if c != nil {
		if err := preview(c, path, opts, "in the repository "+filepath.Base(host)); err != nil {
			return "", err
		}
	}
	res, err := vault.InitIn(host, opts, time.Now())
	if err != nil {
		return "", err
	}
	return res.Root, nil
}

// preview lists the files a new vault will hold and asks to go ahead.
func preview(c *console.Console, path string, opts vault.Options, where string) error {
	files := append(vault.TemplateFiles(opts.Kind), vault.Marker, vault.LedgerPath)
	if opts.Kind == vault.Project {
		files = append(files, vault.TaskLedgerPath)
	}
	c.Say("claude-atlas will create the %s %s (%s mode) with %d files %s:", opts.Kind.Noun(), home.Display(path), opts.Mode, len(files), where)
	for _, item := range files {
		c.Say("    %s", item)
	}
	c.Say("")
	ok, err := c.Confirm("Create this vault?", true)
	if err != nil {
		return err
	}
	if !ok {
		return ErrCancelled
	}
	return nil
}
