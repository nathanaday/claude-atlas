// Package wizard is the guided `claude-atlas setup` flow.
package wizard

import (
	"fmt"
	"os"
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
	VaultsDir    string
	FirstVault   string
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
		dir := opts.VaultsDir
		if dir == "" {
			dir = c.Ask("Where should your vaults live?", home.DefaultVaults)
		}
		cfg = h.Default(dir)
	} else {
		loaded, err := h.Load()
		if err != nil {
			return 1, err
		}
		cfg = loaded
		if opts.VaultsDir != "" {
			cfg.VaultsDir = home.Expand(opts.VaultsDir)
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
	ix, err := registry.Scan(cfg)
	if err != nil {
		return 1, err
	}
	firstPath := ""
	if len(ix.Entries) == 0 {
		name := opts.FirstVault
		if name == "" {
			name = c.Ask("Name for your first project", "welcome")
		}
		path, err := vaults.ResolvePath(name, cfg.VaultsDir, vault.Project)
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
	plan(c, "vaults dir", "use", home.Display(cfg.VaultsDir))
	if firstPath != "" {
		plan(c, "first project", "create", home.Display(firstPath))
	} else {
		found, needAdopting := 0, 0
		for _, e := range ix.Entries {
			switch {
			case e.Reason == registry.ReasonV1:
				needAdopting++
			case e.Error == "":
				found++
			}
		}
		note := fmt.Sprintf("%d found", found)
		if needAdopting > 0 {
			note += fmt.Sprintf(", %d need adopting", needAdopting)
		}
		plan(c, "vaults", "keep", note)
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
		if _, err := vaults.Create(firstPath, vault.Options{Kind: vault.Project, Name: filepath.Base(firstPath)}, c, false); err != nil {
			return 1, err
		}
		registered, err := vaults.Register(h, cfg, firstPath)
		if err != nil {
			return 1, err
		}
		note := home.Display(firstPath)
		if registered {
			note += "; recorded in the config, since it is outside " + home.Display(cfg.VaultsDir)
		}
		c.Step(console.OK, "first project", note)
	}
	entries, _, err := refresh.Registry(cfg, h.StateDir(), time.Now())
	if err != nil {
		return 1, err
	}
	read := 0
	for _, e := range entries {
		if e.Error != "" {
			c.Step(console.Fail, "vault", home.Display(e.Path)+": "+e.Error)
			continue
		}
		read++
	}
	c.Step(console.OK, "refreshed", fmt.Sprintf("%d vault%s", read, plural(read)))

	c.Say("")
	c.Say("Setup complete.")
	c.Say("")
	c.Say("  Vaults         %s", home.Display(cfg.VaultsDir))
	if firstPath != "" {
		c.Say("  First project  %s", home.Display(firstPath))
	}
	c.Say("")
	c.Say("Open a vault in Obsidian with `claude-atlas open-vault NAME`.")
	c.Say("")
	c.Say("Next:")
	c.Say("  claude-atlas new-project NAME    create another project")
	c.Say("  claude-atlas new-knowledge NAME  create a knowledge base")
	c.Say("  claude-atlas open-claude NAME    start Claude Code in a vault; try /claude-atlas:wiki")
	c.Say("  claude-atlas refresh             read every vault again")
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
