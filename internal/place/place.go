// Package place finds where a session is: in a project, anywhere inside its work, or
// in a knowledge base. The hooks, the MCP server, and the CLI resolve a session the same
// way through it, and a session heals its own entry in the atlas config.
package place

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// EnvPlace names the place explicitly for the MCP server and hooks: a knowledge base's
// root or a project's work folder. It is the variable the launcher sets.
const EnvPlace = vault.EnvVault

// Place is where a session is.
type Place struct {
	// Project is set in a project session.
	Project *project.Project
	// Vault is the knowledge base in scope: the session's own, or the project's. nil
	// for a project that uses none, or whose knowledge base the atlas cannot find.
	Vault *vault.Vault
	// KnowledgeError says why Vault is nil for a project that names a knowledge base.
	KnowledgeError string
	// Entry is the registry's entry for the project or the knowledge base, with the
	// whole index behind it; nil when there is no atlas config or the scan failed.
	Entry *registry.Entry
	Index *registry.Index
	// Heal is what registering the project or the knowledge base did to the config,
	// when asked to.
	Heal vaults.Heal
}

// InProject reports whether the session is in a project.
func (p *Place) InProject() bool { return p != nil && p.Project != nil }

// ErrNoPlace means the session is in neither a project nor a knowledge base.
var ErrNoPlace = errors.New("not in a claude-atlas project or knowledge base")

// Resolve finds the place: the explicit path, then the environment, then the nearest
// project or knowledge base at or above start. A knowledge base inside a project's work is
// nearer than the project from anywhere inside it. An explicit path may be a
// knowledge base's root or a project's work folder. With register set, the session heals
// the atlas config so it lists the project or the knowledge base at this path.
func Resolve(h home.Home, explicit, envValue, start string, register bool) (*Place, error) {
	for _, given := range []string{explicit, envValue} {
		if given == "" {
			continue
		}
		abs, err := filepath.Abs(home.Expand(given))
		if err != nil {
			return nil, err
		}
		switch {
		case project.IsProject(abs):
			return resolveProject(h, abs, register)
		case vault.IsVault(abs):
			return resolveKnowledge(h, abs, register)
		}
		return nil, fmt.Errorf("%w: %s is neither", ErrNoPlace, abs)
	}
	if start == "" {
		return nil, ErrNoPlace
	}
	work, root := project.FindAbove(start), vault.FindAbove(start)
	switch {
	case root != "" && (work == "" || len(root) > len(work)):
		return resolveKnowledge(h, root, register)
	case work != "":
		return resolveProject(h, work, register)
	}
	return nil, fmt.Errorf("%w: nothing at or above %s", ErrNoPlace, start)
}

func resolveProject(h home.Home, work string, register bool) (*Place, error) {
	p, err := project.Open(work)
	if err != nil {
		return nil, err
	}
	out := &Place{Project: p}
	cfg, err := h.Load()
	if err != nil {
		if errors.Is(err, home.ErrNoAtlas) {
			out.KnowledgeError = "no atlas config on this machine; run claude-atlas setup"
			return out, nil
		}
		return nil, err
	}
	if register {
		if out.Heal, err = vaults.RegisterProject(h, cfg, p); err != nil {
			return nil, err
		}
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		return nil, err
	}
	out.Index = ix
	if e := ix.ByPath(work); e != nil && e.Error == "" {
		out.Entry = e
	}
	if p.Config.Knowledge == nil {
		return out, nil
	}
	kb := ix.ByID(p.Config.Knowledge.ID)
	if kb == nil || kb.Kind != registry.Knowledge || kb.Error != "" {
		out.KnowledgeError = fmt.Sprintf("the knowledge base %s (%s) is not on this machine", p.Config.Knowledge.Name, p.Config.Knowledge.ID)
		return out, nil
	}
	v, err := vault.Open(kb.Path)
	if err != nil {
		out.KnowledgeError = err.Error()
		return out, nil
	}
	out.Vault = v
	return out, nil
}

func resolveKnowledge(h home.Home, root string, register bool) (*Place, error) {
	v, err := vault.Open(root)
	if err != nil {
		return nil, err
	}
	out := &Place{Vault: v}
	cfg, err := h.Load()
	if err != nil {
		if errors.Is(err, home.ErrNoAtlas) {
			return out, nil
		}
		return nil, err
	}
	if register {
		if out.Heal, err = vaults.RegisterKnowledge(h, cfg, v); err != nil {
			return nil, err
		}
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		return nil, err
	}
	out.Index = ix
	if e := ix.ByPath(root); e != nil && e.Error == "" {
		out.Entry = e
	}
	return out, nil
}

// Cwd is the working directory, or "" when it cannot be read.
func Cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}
