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
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var ErrCancelled = errors.New("cancelled")

// CheckNewPath says why a new vault cannot go at path: something is there already, the
// path is inside another vault, or the git repository that holds it ignores it. A path
// inside a project, or inside any repository, is fine: the vault commits into that
// repository.
func CheckNewPath(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; adopt it if it is a vault", home.Display(path))
	}
	if outer := vault.FindAbove(filepath.Dir(path)); outer != "" {
		return fmt.Errorf("%s is inside the knowledge base %s; choose another path", home.Display(path), home.Display(outer))
	}
	_, err := vault.HostFor(path)
	return err
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
		if err := preview(c, path, opts, repoNote(path)); err != nil {
			return nil, err
		}
	}
	return vault.Init(path, opts, time.Now())
}

// repoNote says where a new vault at path will commit: into the repository that holds
// it, or into a new one of its own.
func repoNote(path string) string {
	if host, _ := vault.HostFor(path); host != "" {
		return "in the git repository " + home.Display(host)
	}
	return "and a git repository"
}

// preview lists the files a new vault will hold and asks to go ahead.
func preview(c *console.Console, path string, opts vault.Options, where string) error {
	files := append(vault.TemplateFiles(), vault.Marker, vault.LedgerPath)
	c.Say("claude-atlas will create the knowledge base %s (%s mode) with %d files %s:", home.Display(path), opts.Mode, len(files), where)
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
