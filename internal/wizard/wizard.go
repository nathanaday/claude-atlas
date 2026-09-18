// Package wizard is the guided `claude-atlas setup` flow.
package wizard

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/nathanaday/claude-atlas/internal/claudecode"
	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// Options come from setup's flags.
type Options struct {
	Version      string
	FirstVault   string // a name, a folder in the current directory, or a path
	PluginSource string // marketplace source override, e.g. a local checkout
	WithPlugin   bool
}

func plan(c *console.Console, label, action, target string) {
	c.Say("  %-16s %-10s %s", label, action, target)
}

// Run executes setup. It returns 1 when the user declines the plan.
func Run(h home.Home, c *console.Console, opts Options) (int, error) {
	fresh := !h.Exists()
	var cfg *home.Config
	if fresh {
		cfg = h.Default()
	} else {
		loaded, err := h.Load()
		if err != nil {
			return 1, err
		}
		cfg = loaded
	}
	if opts.PluginSource != "" {
		cfg.Plugin.Source = home.Expand(opts.PluginSource)
	}
	if !gitx.Available() {
		return 1, fmt.Errorf("git is required and is not on PATH; install it (on macOS: xcode-select --install) and run setup again")
	}

	installed, _ := claudecode.InstalledPlugin(cfg.Plugin.ID)
	claude := claudecode.CLI()
	ix, err := registry.Scan(cfg)
	if err != nil {
		return 1, err
	}
	firstPath := ""
	if len(ix.Knowledge()) == 0 {
		arg := opts.FirstVault
		if arg == "" {
			arg = c.Ask("Your first knowledge base: a path, or a name for a folder here", "notes")
		}
		path, err := vaults.ResolvePath(arg)
		if err != nil {
			return 1, err
		}
		if err := vaults.CheckNewPath(path); err != nil {
			return 1, err
		}
		firstPath = path
	}

	c.Say("")
	c.Say("claude-atlas setup")
	c.Say("")
	plan(c, "home", ternary(fresh, "create", "exists"), home.Display(h.Root))
	switch {
	case installed != nil:
		plan(c, "plugin", "installed", fmt.Sprintf("%s v%s", cfg.Plugin.ID, installed.Version))
	case !opts.WithPlugin:
		plan(c, "plugin", "skip", "--no-plugin")
	case claude == "":
		plan(c, "plugin", "skip", "`claude` is not on PATH; manual commands will be printed")
	default:
		plan(c, "plugin", "install", fmt.Sprintf("%s from %s via `claude plugin`", cfg.Plugin.ID, cfg.Plugin.Source))
	}
	if firstPath != "" {
		plan(c, "knowledge base", "create", home.Display(firstPath))
	} else {
		needAdopting := 0
		for _, e := range ix.Entries {
			if e.Reason == registry.ReasonV1 {
				needAdopting++
			}
		}
		note := fmt.Sprintf("%d listed", len(ix.Knowledge()))
		if needAdopting > 0 {
			note += fmt.Sprintf(", %d need adopting", needAdopting)
		}
		plan(c, "knowledge", "keep", note)
	}
	c.Say("")
	ok, err := c.Confirm("Proceed?", true)
	if err != nil {
		return 1, err
	}
	if !ok {
		return 1, nil
	}
	c.Say("")

	if err := h.Save(cfg); err != nil {
		return 1, err
	}
	c.Step(console.OK, "home", home.Display(h.Root))

	if installed == nil && opts.WithPlugin && claude != "" {
		ran, err := claudecode.InstallPlugin(cfg.Plugin.Source, cfg.Plugin.ID)
		for _, cmd := range ran {
			c.Step(console.OK, "ran", cmd)
		}
		if err != nil {
			c.Step(console.Fail, "plugin", err.Error())
		} else {
			installed, _ = claudecode.InstalledPlugin(cfg.Plugin.ID)
		}
	}
	switch {
	case installed != nil && installed.Version != "" && installed.Version != opts.Version && opts.Version != "dev":
		c.Step(console.OK, "plugin", fmt.Sprintf("v%s installed; this binary is %s. Keep them in step.", installed.Version, opts.Version))
	case installed != nil:
		c.Step(console.OK, "plugin", fmt.Sprintf("v%s", installed.Version))
	default:
		c.Step(console.Skip, "plugin", "not installed; Claude Code will not have the atlas tools until it is")
	}

	if firstPath != "" {
		if _, err := vaults.Create(firstPath, vault.Options{Name: filepath.Base(firstPath)}, c, false); err != nil {
			return 1, err
		}
		if _, err := vaults.Register(h, cfg, firstPath); err != nil {
			return 1, err
		}
		c.Step(console.OK, "knowledge base", home.Display(firstPath))
	}
	entries, _, err := refresh.Registry(h, cfg, h.StateDir(), time.Now())
	if err != nil {
		return 1, err
	}
	read := 0
	for _, e := range entries {
		if e.Error != "" {
			c.Step(console.Fail, string(e.Kind), home.Display(e.Path)+": "+e.Error)
			continue
		}
		read++
	}
	c.Step(console.OK, "refreshed", fmt.Sprintf("%d entr%s", read, map[bool]string{true: "y", false: "ies"}[read == 1]))

	c.Say("")
	c.Say("Setup complete.")
	c.Say("")
	if firstPath != "" {
		c.Say("  Knowledge base  %s", home.Display(firstPath))
	}
	c.Say("")
	c.Say("Open a knowledge base in Obsidian with `claude-atlas open-vault NAME`.")
	c.Say("")
	c.Say("Next:")
	c.Say("  cd <your work> && claude-atlas init   make a folder or repository a project")
	c.Say("  claude-atlas new-knowledge PATH       create another knowledge base")
	c.Say("  claude-atlas open-claude NAME         start Claude Code in a knowledge base or project")
	c.Say("  claude-atlas refresh                  read everything again")
	if installed == nil {
		c.Say("")
		c.Say("The plugin is not installed. Install it, then run setup again:")
		for _, cmd := range claudecode.Commands(cfg.Plugin.Source, cfg.Plugin.ID) {
			c.Say("  %s", cmd)
		}
	}
	c.Say("")
	return 0, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
