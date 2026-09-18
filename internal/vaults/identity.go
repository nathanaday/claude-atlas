package vaults

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// ResolvePath is the absolute path a knowledge base argument names. A bare name is a
// folder in the current directory, as for any other command that takes a path.
func ResolvePath(arg string) (string, error) {
	if strings.TrimSpace(arg) == "" {
		return "", fmt.Errorf("a knowledge base needs a name or a path")
	}
	return filepath.Abs(home.Expand(arg))
}

// Register lists a knowledge base in the config; it reports whether the config changed.
func Register(h home.Home, cfg *home.Config, root string) (bool, error) {
	abs, err := filepath.Abs(home.Expand(root))
	if err != nil {
		return false, err
	}
	if !vault.IsVault(abs) {
		return false, fmt.Errorf("%s is not a claude-atlas vault (no %s)", home.Display(abs), vault.Marker)
	}
	if !cfg.AddKnowledge(abs) {
		return false, nil
	}
	if err := h.Save(cfg); err != nil {
		return false, err
	}
	return true, nil
}

// Unregister removes a knowledge base from the config. The folder stays.
func Unregister(h home.Home, cfg *home.Config, root string) error {
	abs, err := filepath.Abs(home.Expand(root))
	if err != nil {
		return err
	}
	if !cfg.RemoveKnowledge(abs) {
		return fmt.Errorf("%s is not a registered knowledge base", home.Display(abs))
	}
	return h.Save(cfg)
}

// RegisterKnowledge makes sure the config lists the knowledge base at v.Root, by the
// same rule RegisterProject heals a project: it adds one the config does not know, and
// moves one whose id the config knows at another path or, failing that, the only listed
// knowledge base whose folder is gone.
func RegisterKnowledge(h home.Home, cfg *home.Config, v *vault.Vault) (Heal, error) {
	if cfg.HasKnowledge(v.Root) {
		return HealNone, nil
	}
	drop, heal := healPaths(cfg.Knowledge, v.Config.ID, func(path string) (string, bool) {
		c, ok := vault.ReadConfig(path)
		return c.ID, ok
	})
	for _, path := range drop {
		cfg.RemoveKnowledge(path)
	}
	cfg.AddKnowledge(v.Root)
	return heal, h.Save(cfg)
}

// Edit changes a knowledge base's own facts. Nil means unchanged.
type Edit struct {
	Name  string
	Scope *string
}

// EditIdentity changes a knowledge base's identity file: one operation, one commit,
// whatever the edit touches. A new name moves the vault's folder to match; the returned
// path is where the vault is afterwards.
func EditIdentity(h home.Home, cfg *home.Config, e registry.Entry, edit Edit, now time.Time) (string, error) {
	path := e.Path
	if edit.Name != "" {
		moved, err := renameFolder(h, cfg, e, edit.Name)
		if err != nil {
			return e.Path, err
		}
		path = moved
	}
	if err := writeIdentity(path, edit, now); err != nil {
		if path != e.Path {
			// The name and the folder never disagree: put the folder back.
			os.Rename(path, e.Path)
			moveConfigPath(h, cfg, path, e.Path)
		}
		return e.Path, err
	}
	return path, nil
}

// renameFolder moves e's folder so its name is the new one, cleaned so Obsidian can
// name it. It returns the folder the vault sits in afterwards, which is the one it
// already had when the name needs no move.
func renameFolder(h home.Home, cfg *home.Config, e registry.Entry, name string) (string, error) {
	folder := links.CleanName(name)
	if folder == "" {
		return "", fmt.Errorf("%q leaves no usable folder name", name)
	}
	if folder == filepath.Base(e.Path) {
		return e.Path, nil
	}
	target := filepath.Join(filepath.Dir(e.Path), folder)
	if taken, err := os.Stat(target); err == nil {
		// On a case-insensitive filesystem the vault's own folder answers to the new
		// name already; renaming it to change its case is still a rename.
		here, err := os.Stat(e.Path)
		if err != nil || !os.SameFile(taken, here) {
			return "", fmt.Errorf("%s already exists; rename it or choose another name", home.Display(target))
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(e.Path, target); err != nil {
		return "", err
	}
	if err := moveConfigPath(h, cfg, e.Path, target); err != nil {
		os.Rename(target, e.Path)
		return "", err
	}
	return target, nil
}

// moveConfigPath points the config's entry for a knowledge base at the folder it moved
// to.
func moveConfigPath(h home.Home, cfg *home.Config, from, to string) error {
	cfg.RemoveKnowledge(from)
	cfg.AddKnowledge(to)
	return h.Save(cfg)
}

func writeIdentity(path string, edit Edit, now time.Time) error {
	var fields []string
	if edit.Name != "" {
		fields = append(fields, "name")
	}
	if edit.Scope != nil {
		fields = append(fields, "scope")
	}
	if len(fields) == 0 {
		return nil
	}
	return vault.UpdateConfig(path, "edit "+strings.Join(fields, ", "), now, func(c *vault.Config) error {
		if edit.Name != "" {
			c.Name = edit.Name
		}
		if edit.Scope != nil {
			c.Scope = *edit.Scope
		}
		return nil
	})
}
