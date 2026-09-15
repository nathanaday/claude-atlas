// Package wizard is the guided `claude-atlas setup` flow.
package wizard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nathanaday/claude-atlas/internal/claudecode"
	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/pages"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/tree"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// Options come from setup's flags.
type Options struct {
	Version      string
	VaultsDir    string
	AtlasVault   string
	FirstVault   string
	PluginSource string // marketplace source override, e.g. a local checkout
	WithPlugin   bool
}

func plan(c *console.Console, label, action, target string) {
	c.Say("  %-16s %-10s %s", label, action, target)
}

// EnsureAtlasVault creates the root Obsidian vault if missing; it reports whether it did.
func EnsureAtlasVault(cfg *home.Config) (bool, error) {
	obsidianDir := filepath.Join(cfg.AtlasVault, ".obsidian")
	_, err := os.Stat(obsidianDir)
	created := err != nil
	if err := os.MkdirAll(obsidianDir, 0o755); err != nil {
		return false, err
	}
	app := filepath.Join(obsidianDir, "app.json")
	if _, err := os.Stat(app); err != nil {
		data, _ := json.MarshalIndent(map[string]any{"newLinkFormat": "absolute"}, "", "  ")
		if err := os.WriteFile(app, append(data, '\n'), 0o644); err != nil {
			return false, err
		}
	}
	return created, os.MkdirAll(cfg.TreeRoot(), 0o755)
}

// Run executes setup. It returns 1 when the user declines the plan.
func Run(h home.Home, c *console.Console, opts Options) (int, error) {
	fresh := !h.Exists()
	var cfg *home.Config
	if fresh {
		atlas := opts.AtlasVault
		if atlas == "" {
			atlas = c.Ask("Where should the atlas vault live?", home.DefaultAtlas)
		}
		dir := opts.VaultsDir
		if dir == "" {
			dir = c.Ask("Where should new vaults live?", home.DefaultVaults)
		}
		cfg = h.Default(dir, atlas)
	} else {
		loaded, err := h.Load()
		if err != nil {
			return 1, err
		}
		cfg = loaded
		if opts.VaultsDir != "" {
			cfg.VaultsDir = home.Expand(opts.VaultsDir)
		}
		if opts.AtlasVault != "" {
			cfg.AtlasVault = home.Expand(opts.AtlasVault)
		}
	}
	if opts.PluginSource != "" {
		cfg.Plugin.Source = home.Expand(opts.PluginSource)
	}
	if !gitx.Available() {
		return 1, fmt.Errorf("git is required and is not on PATH; install it (on macOS: xcode-select --install) and run setup again")
	}

	installed, _ := claudecode.InstalledPlugin(cfg.Plugin.ID)
	claude := claudecode.CLI()
	atlasReady := false
	if _, err := os.Stat(filepath.Join(cfg.AtlasVault, ".obsidian")); err == nil {
		atlasReady = true
	}
	var registered []*tree.Project
	if atlasReady {
		projects, _, err := tree.Walk(cfg.TreeRoot())
		if err != nil {
			return 1, err
		}
		registered = projects
	}
	firstPath := ""
	if len(registered) == 0 {
		name := opts.FirstVault
		if name == "" {
			name = c.Ask("Name for your first vault", "welcome")
		}
		path, err := vaults.ResolveNewPath(name, cfg.VaultsDir, "")
		if err != nil {
			return 1, err
		}
		if _, err := os.Stat(path); err == nil {
			return 1, fmt.Errorf("%s already exists; choose another name or adopt it with `claude-atlas adopt`", home.Display(path))
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
	plan(c, "atlas vault", ternary(atlasReady, "exists", "create"), home.Display(cfg.AtlasVault))
	plan(c, "vaults dir", "use", home.Display(cfg.VaultsDir))
	if firstPath != "" {
		plan(c, "first vault", "create", home.Display(firstPath))
	} else {
		plan(c, "projects", "keep", fmt.Sprintf("%d registered", len(registered)))
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

	created, err := EnsureAtlasVault(cfg)
	if err != nil {
		return 1, err
	}
	c.Step(ternary(created, console.OK, console.Skip), "atlas vault", ternary(created, home.Display(cfg.AtlasVault), "already present"))

	if firstPath != "" {
		if _, err := vaults.Create(firstPath, vault.Options{Kind: vault.Project}, c, false); err != nil {
			return 1, err
		}
		node, err := vaults.RegisterProject(cfg, firstPath, vaults.RegisterOptions{
			Purpose: "Created by claude-atlas setup to verify the installation.",
		})
		if err != nil {
			return 1, err
		}
		c.Step(console.OK, "first vault", fmt.Sprintf("%s → tree/%s.md", home.Display(firstPath), node.Rel))
	}
	if err := pages.Write(cfg, h.Root, opts.Version); err != nil {
		return 1, err
	}
	c.Step(console.OK, "pages", "About.md, Reference.md")
	page, _, err := refresh.Run(cfg, h.StateDir(), time.Now())
	if err != nil {
		return 1, err
	}
	c.Step(console.OK, "refreshed", home.Display(page))

	c.Say("")
	c.Say("Setup complete.")
	c.Say("")
	c.Say("  Atlas        %s", home.Display(cfg.AtlasVault))
	if firstPath != "" {
		c.Say("  First vault  %s", home.Display(firstPath))
	}
	c.Say("")
	c.Say("Open either one in Obsidian with \"Open folder as vault\", or `claude-atlas open-vault`.")
	c.Say("")
	c.Say("Next:")
	c.Say("  claude-atlas new-vault           create another vault, step by step")
	c.Say("  claude-atlas open-claude NAME    start Claude Code in a vault; try /claude-atlas:wiki")
	c.Say("  claude-atlas refresh             rebuild Overview.md from every vault")
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

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
