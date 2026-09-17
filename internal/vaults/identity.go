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

// PathFor is where a new knowledge base goes by default: straight under the vaults
// directory.
func PathFor(vaultsDir, name string) string {
	return filepath.Join(vaultsDir, name)
}

// ResolvePath takes anything path-like as the vault's path and puts a bare name at
// PathFor.
func ResolvePath(arg, vaultsDir string) (string, error) {
	if strings.Contains(arg, string(filepath.Separator)) || strings.HasPrefix(arg, "~") || strings.HasPrefix(arg, ".") {
		return filepath.Abs(home.Expand(arg))
	}
	return PathFor(vaultsDir, arg), nil
}

// Register lists a vault outside the vaults directory in the config; it reports whether
// the config changed. A vault inside the vaults directory needs no entry.
func Register(h home.Home, cfg *home.Config, root string) (bool, error) {
	abs, err := filepath.Abs(home.Expand(root))
	if err != nil {
		return false, err
	}
	if !vault.IsVault(abs) {
		return false, fmt.Errorf("%s is not a claude-atlas vault (no %s)", home.Display(abs), vault.Marker)
	}
	if cfg.Inside(abs) {
		return false, nil
	}
	if !cfg.AddKnowledge(abs) {
		return false, nil
	}
	if err := h.Save(cfg); err != nil {
		return false, err
	}
	return true, nil
}

// CheckForget reports why a vault cannot be forgotten. A vault inside the vaults directory
// cannot: the scan finds it; the error says to move or delete the folder. The folder
// itself need not exist; a registered path outlives it.
func CheckForget(cfg *home.Config, root string) error {
	abs, err := filepath.Abs(home.Expand(root))
	if err != nil {
		return err
	}
	if cfg.Inside(abs) {
		return fmt.Errorf("%s is inside the vaults directory; the scan finds it there, so move or delete the folder to forget it", home.Display(abs))
	}
	return nil
}

// Unregister removes a vault from the config.
func Unregister(h home.Home, cfg *home.Config, root string) error {
	abs, err := filepath.Abs(home.Expand(root))
	if err != nil {
		return err
	}
	if err := CheckForget(cfg, abs); err != nil {
		return err
	}
	if !cfg.RemoveKnowledge(abs) {
		return fmt.Errorf("%s is not registered", home.Display(abs))
	}
	return h.Save(cfg)
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

// moveConfigPath points a config entry for a vault outside the vaults directory at
// the folder it moved to. A vault inside the vaults directory has no entry to fix.
func moveConfigPath(h home.Home, cfg *home.Config, from, to string) error {
	if !cfg.RemoveKnowledge(from) {
		return nil
	}
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
