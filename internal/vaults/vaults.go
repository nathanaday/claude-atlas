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
		files := append(vault.TemplateFiles(opts.Kind), vault.Marker, vault.LedgerPath)
		if opts.Kind == vault.Project {
			files = append(files, vault.TaskLedgerPath)
		}
		c.Say("claude-atlas will create the %s %s (%s mode) with %d files and a git repository:", opts.Kind.Noun(), home.Display(path), opts.Mode, len(files))
		for _, item := range files {
			c.Say("    %s", item)
		}
		c.Say("")
		ok, err := c.Confirm("Create this vault?", true)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrCancelled
		}
	}
	return vault.Init(path, opts, time.Now())
}
