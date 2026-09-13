// Package cli parses arguments and dispatches subcommands.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/claudecode"
	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/hooks"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/lint"
	"github.com/nathanaday/claude-atlas/internal/mcpserver"
	"github.com/nathanaday/claude-atlas/internal/obsidian"
	"github.com/nathanaday/claude-atlas/internal/pages"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/tree"
	"github.com/nathanaday/claude-atlas/internal/tui"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
	"github.com/nathanaday/claude-atlas/internal/wizard"
)

// Version is set at build time with -ldflags "-X .../cli.Version=v1.2.3".
var Version = "dev"

const usage = `claude-atlas: knowledge vaults for Claude Code, and one view across them.

Usage:
  claude-atlas [--home DIR] [-y] <command> [options]

Vaults:
  setup                  install the plugin, create the atlas, and your first vault
  new-vault              create a vault and its project page, step by step
  new-vault NAME         create a vault without prompts
  adopt [PATH]           make an existing Obsidian or claude-obsidian vault a claude-atlas vault
  open-vault [NAME]      open the atlas, or a project's vault, in Obsidian
  open-claude NAME       start Claude Code inside a project's vault
  ingest NAME [PATH...]  stage new files from outside the vault into its inbox, then ingest them

The atlas:
  view                   the whole atlas as one interactive tree
  list                   list every project
  show NAME              everything the atlas knows about a project
  edit NAME [flags]      change a project's name, purpose, category, priority, state, or vault
  remove NAME            remove a project from the atlas; the vault stays on disk
  link NAME PATH         link a git repo or a folder of material to a project
  unlink NAME PATH       remove that link; the folder is untouched
  links NAME             show a project's links and what refresh found in them
  refresh                read every vault and rewrite Overview.md

Inside a vault (VAULT is a project name or a path; default: the current directory):
  lint [VAULT]           run the wiki health check
  history [VAULT]        list operations, newest first
  undo VAULT OPERATION   revert one operation
  recover [VAULT]        restore a vault after an interrupted operation
  mode [VAULT] [MODE]    show or set the filing mode: generic or lyt
  apply VAULT PLAN.json  apply a plan file, for scripts

Plugin:
  mcp                    serve the atlas tools over stdio; Claude Code runs this
  hook EVENT             run a plugin hook: session-start, guard, stop

  info                   show every path and version the atlas uses
  doctor                 check the installation and every registered vault
  version                print the version

Global options:
  --home DIR       atlas home (default ~/.claude-atlas or $CLAUDE_ATLAS_HOME)
  -y, --yes        answer yes to every prompt
`

type env struct {
	home    home.Home
	console *console.Console
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
}

// Main runs the CLI and returns the exit code.
func Main(args []string) int {
	return run(args, os.Stdin, os.Stdout, os.Stderr, nil)
}

// run is Main with injectable streams; console may be nil to build one from stdin.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, c *console.Console) int {
	global := flag.NewFlagSet("claude-atlas", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	homeFlag := global.String("home", "", "")
	yes := global.Bool("yes", false, "")
	global.BoolVar(yes, "y", false, "")
	if err := global.Parse(args); err != nil {
		fmt.Fprint(stderr, usage)
		return 2
	}
	rest := global.Args()
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "--help" || rest[0] == "-h" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if c == nil {
		c = console.New(*yes)
		c.Out = stdout
	} else {
		c.AssumeYes = c.AssumeYes || *yes
	}
	e := &env{home: home.Resolve(*homeFlag), console: c, stdin: stdin, stdout: stdout, stderr: stderr}

	var err error
	var code int
	switch rest[0] {
	case "setup":
		code, err = e.setup(rest[1:])
	case "new-vault":
		code, err = e.newVault(rest[1:])
	case "adopt":
		code, err = e.adopt(rest[1:])
	case "view":
		code, err = e.view(rest[1:])
	case "open-vault":
		code, err = e.openVault(rest[1:])
	case "open-claude":
		code, err = e.openClaude(rest[1:])
	case "ingest":
		code, err = e.ingest(rest[1:])
	case "link":
		code, err = e.link(rest[1:])
	case "unlink":
		code, err = e.unlink(rest[1:])
	case "links":
		code, err = e.links(rest[1:])
	case "list":
		code, err = e.list(rest[1:])
	case "show":
		code, err = e.show(rest[1:])
	case "edit":
		code, err = e.edit(rest[1:])
	case "remove":
		code, err = e.remove(rest[1:])
	case "refresh":
		code, err = e.refresh(rest[1:])
	case "lint":
		code, err = e.lint(rest[1:])
	case "history":
		code, err = e.history(rest[1:])
	case "undo":
		code, err = e.undo(rest[1:])
	case "recover":
		code, err = e.recover(rest[1:])
	case "mode":
		code, err = e.mode(rest[1:])
	case "apply":
		code, err = e.apply(rest[1:])
	case "mcp":
		code, err = e.mcp(rest[1:])
	case "hook":
		code, err = e.hook(rest[1:])
	case "info":
		code, err = e.info(rest[1:])
	case "doctor":
		code, err = e.doctor(rest[1:])
	case "version":
		fmt.Fprintln(stdout, Version)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", rest[0], usage)
		return 2
	}
	if err != nil {
		if errors.Is(err, vaults.ErrCancelled) {
			fmt.Fprintln(stderr, "cancelled")
			return 1
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parse accepts flags before and after positional arguments, unlike flag.Parse.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// refreshAll rewrites every derived page in the atlas vault.
func (e *env) refreshAll(cfg *home.Config) (string, *refresh.Result, error) {
	if err := pages.Write(cfg, e.home.Root, Version); err != nil {
		return "", nil, err
	}
	return refresh.Run(cfg, e.home.StateDir(), time.Now())
}

func (e *env) setup(args []string) (int, error) {
	fs := newFlags("setup", e.stderr)
	vaultsDir := fs.String("vaults-dir", "", "where new vaults are created (default ~/Documents/Vaults)")
	atlasVault := fs.String("atlas-vault", "", "where the atlas vault lives (default ~/Documents/Atlas)")
	first := fs.String("first-vault", "", "name or path of the first vault (default welcome)")
	source := fs.String("plugin-source", "", "install the plugin from this marketplace source, e.g. a local checkout")
	noPlugin := fs.Bool("no-plugin", false, "do not run `claude plugin`")
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	opts := wizard.Options{Version: Version, VaultsDir: *vaultsDir, AtlasVault: *atlasVault, FirstVault: *first, PluginSource: *source, WithPlugin: !*noPlugin}
	return wizard.Run(e.home, e.console, opts)
}

func nodeFlags(fs *flag.FlagSet) *vaults.RegisterOptions {
	opts := &vaults.RegisterOptions{}
	fs.StringVar(&opts.Category, "category", "", "directory under tree/ to file the project in, e.g. university/cs566")
	fs.StringVar(&opts.Purpose, "purpose", "", "one paragraph: why this vault exists")
	fs.StringVar(&opts.Priority, "priority", "normal", "high, normal, low, or someday")
	return opts
}

func modeFlag(fs *flag.FlagSet) *string {
	return fs.String("mode", "", "filing mode for new pages: generic (default) or lyt")
}

func parseMode(s string) (vault.Mode, error) {
	if s == "" {
		return vault.Generic, nil
	}
	return vault.ParseMode(s)
}

func (e *env) newVault(args []string) (int, error) {
	fs := newFlags("new-vault", e.stderr)
	opts := nodeFlags(fs)
	fs.StringVar(&opts.Name, "name", "", "display name (default: the vault's directory name)")
	mode := modeFlag(fs)
	from := fs.String("from", "", "register a vault that already exists at this path (same as adopt)")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	m, err := parseMode(*mode)
	if err != nil {
		return 2, err
	}
	switch {
	case *from != "" && len(positional) == 0:
		return e.adoptPath(*from, *opts, m)
	case *from == "" && len(positional) == 0:
		return e.newVaultInteractive()
	case *from == "" && len(positional) == 1:
		return e.createVault(positional[0], *opts, m)
	}
	return 2, errors.New("usage: claude-atlas new-vault [NAME | --from PATH] [--name N] [--category DIR] [--purpose TEXT] [--priority P] [--mode generic|lyt]")
}

func (e *env) adopt(args []string) (int, error) {
	fs := newFlags("adopt", e.stderr)
	opts := nodeFlags(fs)
	fs.StringVar(&opts.Name, "name", "", "display name (default: the vault's directory name)")
	mode := fs.String("mode", "", "filing mode when the vault has none: generic (default) or lyt")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas adopt [PATH] [--name N] [--category DIR] [--purpose TEXT] [--priority P] [--mode generic|lyt]")
	}
	var m vault.Mode
	if *mode != "" {
		if m, err = vault.ParseMode(*mode); err != nil {
			return 2, err
		}
	}
	if len(positional) == 0 {
		if !e.console.Interactive() {
			return 2, errors.New("usage: claude-atlas adopt PATH (the interactive screen needs a terminal)")
		}
		cfg, err := e.home.Load()
		if err != nil {
			return 1, err
		}
		choice, err := tui.RunAdopt(tui.Categories(cfg.TreeRoot()))
		if err != nil {
			return 1, err
		}
		if choice == nil {
			return 1, vaults.ErrCancelled
		}
		return e.adoptPath(choice.Path, vaults.RegisterOptions{Name: choice.Name, Category: choice.Category, Purpose: choice.Purpose, Priority: opts.Priority}, vault.Mode(choice.Mode))
	}
	return e.adoptPath(positional[0], *opts, m)
}

// createOrAdopt is what the interactive screens call: it makes or adopts the vault and
// registers it, returning the project's rel. The CLI commands share every step.
func (e *env) createOrAdopt(cfg *home.Config, choice tui.AddVault) (string, error) {
	mode, err := parseMode(choice.Mode)
	if err != nil {
		return "", err
	}
	opts := vaults.RegisterOptions{Name: choice.Name, Category: choice.Category, Purpose: choice.Purpose}
	if choice.Adopt {
		if _, err := vault.Adopt(choice.Path, mode, time.Now()); err != nil {
			return "", err
		}
		projects, _, err := tree.Walk(cfg.TreeRoot())
		if err != nil {
			return "", err
		}
		if existing := tree.FindByVault(projects, choice.Path); existing != nil {
			return existing.Rel, nil
		}
	} else if _, err := vaults.Create(choice.Path, mode, e.console, false); err != nil {
		return "", err
	}
	project, err := vaults.Register(cfg, choice.Path, opts)
	if err != nil {
		return "", err
	}
	return project.Rel, nil
}

// hooks wires the interactive screens to the same backend calls the CLI commands use.
func (e *env) hooks(cfg *home.Config) tui.Hooks {
	return tui.Hooks{
		Load: func() ([]*tree.Project, error) {
			projects, _, err := tree.Walk(cfg.TreeRoot())
			return projects, err
		},
		Categories: func() []string { return tui.Categories(cfg.TreeRoot()) },
		State: func(rel string) *tree.State {
			state, err := tree.ReadState(e.home.StateDir(), rel)
			if err != nil {
				return nil
			}
			return state
		},
		Update:    func(p *tree.Project, edit vaults.Edit) error { return vaults.Update(cfg, p, edit) },
		Unlink:    vaults.Unlink,
		Create:    func(choice tui.AddVault) (string, error) { return e.createOrAdopt(cfg, choice) },
		Refresh:   func() error { _, _, err := e.refreshAll(cfg); return err },
		StagePlan: func(p *tree.Project, source string) (*capture.StagePlan, error) { return e.stagePlan(p, source) },
		Stage: func(p *tree.Project, plan *capture.StagePlan) (*capture.StageResult, []string, error) {
			return e.stage(cfg, p, plan)
		},
		VaultsDir: cfg.VaultsDir,
	}
}

// ingestSources are the paths an ingest reads: the given ones, or the project's linked
// material folders when none is given.
func ingestSources(p *tree.Project, given []string) ([]string, error) {
	if len(given) > 0 {
		out := make([]string, 0, len(given))
		for _, g := range given {
			out = append(out, home.Expand(g))
		}
		return out, nil
	}
	if len(p.Materials) == 0 {
		return nil, fmt.Errorf("name a file or folder to ingest; %s has no linked material folders yet", p.Name)
	}
	out := make([]string, 0, len(p.Materials))
	for _, m := range p.Materials {
		out = append(out, home.Expand(m))
	}
	return out, nil
}

// stagePlan opens the project's vault and plans a staging from one source, or from the
// linked material folders when source is empty.
func (e *env) stagePlan(p *tree.Project, source string) (*capture.StagePlan, error) {
	var given []string
	if strings.TrimSpace(source) != "" {
		given = []string{strings.TrimSpace(source)}
	}
	sources, err := ingestSources(p, given)
	if err != nil {
		return nil, err
	}
	v, err := vault.Open(p.VaultPath())
	if err != nil {
		return nil, err
	}
	return capture.PlanStage(v, sources, time.Now())
}

// stage copies a plan into the inbox and links every source folder that is not yet
// material of the project, so a later ingest with no path picks up what is new.
func (e *env) stage(cfg *home.Config, p *tree.Project, plan *capture.StagePlan) (*capture.StageResult, []string, error) {
	v, err := vault.Open(p.VaultPath())
	if err != nil {
		return nil, nil, err
	}
	res, err := capture.ApplyStage(v, plan, time.Now())
	if err != nil {
		return res, nil, err
	}
	var linked []string
	for _, dir := range plan.Dirs {
		if err := vaults.AddLink(p, links.Materials, dir); err == nil {
			linked = append(linked, dir)
		}
	}
	if len(linked) > 0 {
		if _, _, err := e.refreshAll(cfg); err != nil {
			return res, linked, err
		}
	}
	return res, linked, nil
}

// adoptPath makes a directory a claude-atlas vault, registers it, and refreshes.
func (e *env) adoptPath(path string, opts vaults.RegisterOptions, mode vault.Mode) (int, error) {
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	abs, err := filepath.Abs(home.Expand(path))
	if err != nil {
		return 1, err
	}
	res, err := vault.Adopt(abs, mode, time.Now())
	if err != nil {
		return 1, err
	}
	c := e.console
	switch {
	case res.AlreadyAdopted && res.Commit == "":
		c.Step(console.Skip, "adopt", "already a claude-atlas vault")
	case res.WasLegacy:
		c.Step(console.OK, "adopted", fmt.Sprintf("claude-obsidian vault; added %s", strings.Join(res.Added, ", ")))
	default:
		c.Step(console.OK, "adopted", fmt.Sprintf("added %s", strings.Join(res.Added, ", ")))
	}
	if res.GitInitialized {
		c.Step(console.OK, "git", "initialized; every operation is now one commit")
	}
	if res.Commit != "" {
		c.Step(console.OK, "committed", res.Commit[:12]+" baseline")
	}
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return 1, err
	}
	if existing := tree.FindByVault(projects, abs); existing != nil {
		c.Step(console.Skip, "registered", "already tree/"+existing.Rel+".md")
	} else {
		project, err := vaults.Register(cfg, abs, opts)
		if err != nil {
			return 1, err
		}
		c.Step(console.OK, "registered", fmt.Sprintf("%s → tree/%s.md", home.Display(abs), project.Rel))
	}
	page, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	c.Step(console.OK, "refreshed", home.Display(page))
	return 0, nil
}

func (e *env) createVault(arg string, opts vaults.RegisterOptions, mode vault.Mode) (int, error) {
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	path, err := vaults.ResolveNewPath(arg, cfg.VaultsDir)
	if err != nil {
		return 1, err
	}
	if _, err := vaults.Create(path, mode, e.console, true); err != nil {
		return 1, err
	}
	return e.finishVault(cfg, path, opts)
}

// finishVault registers a vault that was just created, refreshes, and reports.
func (e *env) finishVault(cfg *home.Config, path string, opts vaults.RegisterOptions) (int, error) {
	project, err := vaults.Register(cfg, path, opts)
	if err != nil {
		return 1, err
	}
	page, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	c := e.console
	c.Say("")
	c.Step(console.OK, "created", home.Display(path))
	c.Step(console.OK, "registered", "tree/"+project.Rel+".md")
	c.Step(console.OK, "refreshed", home.Display(page))
	c.Say("")
	c.Say("  Open it in Obsidian with `claude-atlas open-vault %s`, or start working:", project.Rel)
	c.Say("  claude-atlas open-claude %s    # then /claude-atlas:wiki", project.Rel)
	c.Say("")
	return 0, nil
}

// newVaultInteractive walks the user through name, category, and purpose, then creates the vault.
func (e *env) newVaultInteractive() (int, error) {
	if !e.console.Interactive() {
		return 2, errors.New("usage: claude-atlas new-vault NAME (the interactive screen needs a terminal)")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	choice, err := tui.RunAddVault(cfg.VaultsDir, tui.Categories(cfg.TreeRoot()))
	if err != nil {
		return 1, err
	}
	if choice == nil {
		return 1, vaults.ErrCancelled
	}
	mode, err := parseMode(choice.Mode)
	if err != nil {
		return 1, err
	}
	if _, err := vaults.Create(choice.Path, mode, e.console, false); err != nil {
		return 1, err
	}
	return e.finishVault(cfg, choice.Path, vaults.RegisterOptions{
		Name: choice.Name, Category: choice.Category, Purpose: choice.Purpose,
	})
}

func (e *env) view(args []string) (int, error) {
	if !e.console.Interactive() {
		return 2, errors.New("view is an interactive screen and needs a terminal")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return 1, err
	}
	items := make([]tui.Item, 0, len(projects))
	for _, p := range projects {
		state, _ := tree.ReadState(e.home.StateDir(), p.Rel)
		items = append(items, tui.Item{Project: p, State: state})
	}
	opener := tui.Opener{
		Status:          obsidian.Status,
		Open:            obsidian.Open,
		RegisterAndOpen: obsidian.RegisterAndOpen,
		Claude: func(vault, prompt string) (*exec.Cmd, error) {
			return claudecode.LaunchCommand(cfg.ClaudeCode, vault, prompt)
		},
	}
	changed, err := tui.RunView(items, opener, e.hooks(cfg))
	if err != nil {
		return 1, err
	}
	if !changed {
		return 0, nil
	}
	page, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "refreshed", home.Display(page))
	return 0, nil
}

// resolveVault turns an open-vault argument into a directory: nothing means the atlas,
// a project name or tree path means its vault, and anything else is taken as a path.
func resolveVault(cfg *home.Config, projects []*tree.Project, arg string) (string, string, error) {
	if arg == "" {
		return cfg.AtlasVault, "the atlas", nil
	}
	if p := tree.FindByRel(projects, arg); p != nil {
		return p.VaultPath(), p.Name, nil
	}
	path, err := filepath.Abs(home.Expand(arg))
	if err != nil {
		return "", "", err
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path, filepath.Base(path), nil
	}
	return "", "", fmt.Errorf("%q is neither a project nor a directory", arg)
}

// openVaultArg resolves a vault for the in-vault commands: a project name, a path, or the
// current directory. It does not need the atlas to be set up when a path is given.
func (e *env) openVaultArg(arg string) (*vault.Vault, error) {
	if arg == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		if root := vault.FindAbove(cwd); root != "" {
			return vault.Open(root)
		}
		return nil, fmt.Errorf("%w: the current directory is not inside a vault; name a project or a path", vault.ErrNotVault)
	}
	if cfg, err := e.home.Load(); err == nil {
		if projects, _, err := tree.Walk(cfg.TreeRoot()); err == nil {
			if p := tree.FindByRel(projects, arg); p != nil {
				return vault.Open(p.VaultPath())
			}
		}
	}
	return vault.Open(home.Expand(arg))
}

func (e *env) openVault(args []string) (int, error) {
	fs := newFlags("open-vault", e.stderr)
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas open-vault [NAME | PATH]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return 1, err
	}
	arg := ""
	if len(positional) == 1 {
		arg = positional[0]
	}
	root, label, err := resolveVault(cfg, projects, arg)
	if err != nil {
		return 1, err
	}
	c := e.console
	registered, running, err := obsidian.Status(root)
	if err != nil {
		c.Say("%v", err)
		c.Say("Open it by hand: in Obsidian choose \"Open folder as vault\" and pick %s.", home.Display(root))
		obsidian.Reveal(root)
		return 1, nil
	}
	if registered {
		if err := obsidian.Open(root); err != nil {
			return 1, err
		}
		c.Step(console.OK, "opened", fmt.Sprintf("%s in Obsidian", label))
		return 0, nil
	}
	c.Say("Obsidian does not know %s yet (%s).", label, home.Display(root))
	question := "Register it as a vault and open it?"
	if running {
		question = "Register it as a vault? Obsidian will quit and relaunch so it sees the new entry."
	}
	ok, err := c.Confirm(question, true)
	if err != nil {
		return 1, err
	}
	if !ok {
		c.Say("Open it by hand: in Obsidian choose \"Open folder as vault\" and pick %s.", home.Display(root))
		obsidian.Reveal(root)
		return 1, vaults.ErrCancelled
	}
	if err := obsidian.RegisterAndOpen(root); err != nil {
		if errors.Is(err, obsidian.ErrManualRestart) {
			c.Say("%v", err)
			return 1, nil
		}
		return 1, err
	}
	c.Step(console.OK, "registered", home.Display(root))
	if running {
		c.Step(console.OK, "restarted", "Obsidian")
	}
	c.Step(console.OK, "opened", fmt.Sprintf("%s in Obsidian", label))
	return 0, nil
}

// skillHint is printed before handing the terminal to Claude Code.
const skillHint = "skills: " + hooks.Skills

func (e *env) openClaude(args []string) (int, error) {
	if len(args) != 1 {
		return 2, errors.New("usage: claude-atlas open-claude NAME")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return 1, err
	}
	project := tree.FindByRel(projects, args[0])
	if project == nil {
		return 1, fmt.Errorf("no project named %q; see `claude-atlas list`", args[0])
	}
	if !e.console.Interactive() {
		return 2, errors.New("open-claude starts an interactive Claude Code session and needs a terminal")
	}
	if vault.IsLegacy(project.VaultPath()) {
		e.console.Say("  %s is a claude-obsidian vault; adopt it first: claude-atlas adopt %s", project.Name, home.Display(project.VaultPath()))
	}
	cmd, err := claudecode.LaunchCommand(cfg.ClaudeCode, project.VaultPath(), "")
	if err != nil {
		return 1, err
	}
	e.console.Say("  %s", home.Display(project.VaultPath()))
	e.console.Say("  %s", skillHint)
	e.console.Say("")
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func (e *env) ingest(args []string) (int, error) {
	fs := newFlags("ingest", e.stderr)
	dryRun := fs.Bool("dry-run", false, "show what would be staged and stop")
	noClaude := fs.Bool("no-claude", false, "stage the files but do not start Claude Code")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) == 0 {
		return 2, errors.New("usage: claude-atlas ingest NAME [PATH ...] [--dry-run] [--no-claude]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	p, err := e.project(cfg, positional[0])
	if err != nil {
		return 1, err
	}
	sources, err := ingestSources(p, positional[1:])
	if err != nil {
		return 1, err
	}
	v, err := vault.Open(p.VaultPath())
	if err != nil {
		return 1, err
	}
	plan, err := capture.PlanStage(v, sources, time.Now())
	if err != nil {
		return 1, err
	}
	c := e.console
	for _, src := range plan.Sources {
		c.Say("  %-10s %s", "source", home.Display(src))
	}
	for _, f := range plan.New {
		c.Say("  %-10s %s", "new", strings.TrimPrefix(f.To, "inbox/"))
	}
	if n := len(plan.Unchanged); n > 0 {
		c.Say("  %-10s %d file%s already ingested or waiting", "unchanged", n, plural(n))
	}
	for _, sk := range plan.Skipped {
		c.Say("  %-10s %s (%s)", "skipped", home.Display(sk.From), sk.Reason)
	}
	if len(plan.New) == 0 {
		c.Say("  nothing new to ingest")
		return 0, nil
	}
	if *dryRun {
		return 0, nil
	}
	question := fmt.Sprintf("Stage %d file%s into inbox/", len(plan.New), plural(len(plan.New)))
	var toLink []string
	for _, dir := range plan.Dirs {
		if !linkedMaterial(p, dir) {
			toLink = append(toLink, home.Display(dir))
		}
	}
	if len(toLink) > 0 {
		question += " and link " + strings.Join(toLink, ", ") + " as material of " + p.Name
	}
	ok, err := c.Confirm(question+"?", true)
	if err != nil {
		return 1, err
	}
	if !ok {
		return 1, vaults.ErrCancelled
	}
	res, linked, err := e.stage(cfg, p, plan)
	if err != nil {
		return 1, err
	}
	c.Step(console.OK, "staged", fmt.Sprintf("%d file%s in %s", len(res.Staged), plural(len(res.Staged)), home.Display(v.Path("inbox"))))
	for _, dir := range linked {
		c.Step(console.OK, "linked", home.Display(dir)+" as material; `claude-atlas ingest "+p.Rel+"` stages what is new next time")
	}
	if *noClaude || !c.Interactive() {
		c.Say("  Next: claude-atlas open-claude %s, then /claude-atlas:wiki-ingest", p.Rel)
		return 0, nil
	}
	ok, err = c.Confirm("Start Claude Code now and run /claude-atlas:wiki-ingest?", true)
	if err != nil {
		return 1, err
	}
	if !ok {
		c.Say("  Next: claude-atlas open-claude %s, then /claude-atlas:wiki-ingest", p.Rel)
		return 0, nil
	}
	cmd, err := claudecode.LaunchCommand(cfg.ClaudeCode, p.VaultPath(), claudecode.IngestPrompt)
	if err != nil {
		return 1, err
	}
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func linkedMaterial(p *tree.Project, dir string) bool {
	for _, m := range p.Materials {
		if filepath.Clean(home.Expand(m)) == dir {
			return true
		}
	}
	return false
}

func (e *env) project(cfg *home.Config, name string) (*tree.Project, error) {
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return nil, err
	}
	p := tree.FindByRel(projects, name)
	if p == nil {
		return nil, fmt.Errorf("no project named %q; see `claude-atlas list`", name)
	}
	return p, nil
}

func (e *env) show(args []string) (int, error) {
	if len(args) != 1 {
		return 2, errors.New("usage: claude-atlas show NAME")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	p, err := e.project(cfg, args[0])
	if err != nil {
		return 1, err
	}
	state, _ := tree.ReadState(e.home.StateDir(), p.Rel)
	c := e.console
	row := func(k, val string) {
		if val == "" {
			val = "—"
		}
		c.Say("  %-16s %s", k, val)
	}
	row("Name", p.Name)
	row("Page", "tree/"+p.Rel+".md")
	row("Vault", home.Display(p.VaultPath()))
	row("Category", p.Category())
	row("Priority", p.Priority)
	row("State", p.State)
	row("Blocked on", p.BlockedOn)
	row("Review after", p.ReviewAfter)
	row("Purpose", p.Purpose)
	row("Done when", p.DefinitionOfDone)
	for _, r := range p.Repos {
		row("Repo", home.Display(home.Expand(r))+"  "+linkFacts(state, r))
	}
	for _, m := range p.Materials {
		row("Materials", home.Display(home.Expand(m))+"  "+linkFacts(state, m))
	}
	if state == nil {
		row("Refreshed", "never; run `claude-atlas refresh`")
		return 0, nil
	}
	heat := state.Heat
	if heat == "" {
		heat = "unreachable"
	}
	if !state.VaultOK {
		row("Vault check", state.VaultError)
	} else {
		row("Vault check", "ok")
	}
	row("Heat", heat)
	row("Created", state.Created)
	row("Last touched", state.LastTouched)
	if state.DaysIdle != nil {
		row("Idle", fmt.Sprintf("%d day%s", *state.DaysIdle, plural(*state.DaysIdle)))
	}
	row("Last operation", state.LastOperation)
	if state.Pages != nil {
		row("Pages", fmt.Sprint(*state.Pages))
	}
	u := state.Unfinished
	var bits []string
	for _, kv := range []struct {
		k string
		v *int
	}{{"empty sections", u.EmptySections}, {"seed pages", u.SeedPages}, {"dead links", u.DeadLinks}} {
		if kv.v != nil {
			bits = append(bits, fmt.Sprintf("%d %s", *kv.v, kv.k))
		}
	}
	row("Unfinished", strings.Join(bits, " · "))
	for i, t := range state.OpenThreads {
		label := "Open threads"
		if i > 0 {
			label = ""
		}
		c.Say("  %-16s - %s", label, refresh.PlainText(t))
	}
	row("Refreshed", state.GeneratedAt)
	return 0, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (e *env) edit(args []string) (int, error) {
	fs := newFlags("edit", e.stderr)
	name := fs.String("name", "", "display name")
	purpose := fs.String("purpose", "", "why the project exists; \"\" clears it")
	category := fs.String("category", "", "directory under tree/ to move the page to; \"\" for the top level")
	priority := fs.String("priority", "", "high, normal, low, or someday")
	state := fs.String("state", "", "active, paused, blocked, or archived")
	blocked := fs.String("blocked-on", "", "what the project waits for; \"\" clears it")
	review := fs.String("review-after", "", "date (YYYY-MM-DD) to revisit these fields; \"\" clears it")
	done := fs.String("done", "", "what finished looks like; \"\" clears it")
	vaultPath := fs.String("vault", "", "point the project at this vault")
	move := fs.Bool("move", false, "with --vault: move the vault directory there")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) != 1 {
		return 2, errors.New("usage: claude-atlas edit NAME [--name N] [--purpose TEXT] [--category DIR] [--priority P] [--state S] [--blocked-on TEXT] [--review-after DATE] [--done TEXT] [--vault PATH [--move]]")
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if len(set) == 0 || (len(set) == 1 && set["move"]) {
		return 2, errors.New("edit needs at least one field flag; see `claude-atlas help`")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	p, err := e.project(cfg, positional[0])
	if err != nil {
		return 1, err
	}
	change := vaults.Edit{Name: *name, Priority: *priority, State: *state, Vault: *vaultPath, MoveVault: *move}
	if set["purpose"] {
		change.Purpose = *purpose
		change.ClearPurpose = *purpose == ""
	}
	if set["category"] {
		cat := strings.Trim(*category, "/")
		change.Category = &cat
	}
	if set["blocked-on"] {
		change.BlockedOn = blocked
	}
	if set["review-after"] {
		change.ReviewAfter = review
	}
	if set["done"] {
		change.DefinitionOfDone = done
	}
	if err := vaults.Update(cfg, p, change); err != nil {
		return 1, err
	}
	page, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	rel := p.Rel
	if change.Category != nil {
		rel = p.ID()
		if *change.Category != "" {
			rel = *change.Category + "/" + rel
		}
	}
	e.console.Step(console.OK, "edited", "tree/"+rel+".md")
	e.console.Step(console.OK, "refreshed", home.Display(page))
	return 0, nil
}

func (e *env) remove(args []string) (int, error) {
	if len(args) != 1 {
		return 2, errors.New("usage: claude-atlas remove NAME")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	p, err := e.project(cfg, args[0])
	if err != nil {
		return 1, err
	}
	ok, err := e.console.Confirm(fmt.Sprintf("Remove %s from the atlas? The vault at %s stays on disk.", p.Name, home.Display(p.VaultPath())), false)
	if err != nil {
		return 1, err
	}
	if !ok {
		return 1, vaults.ErrCancelled
	}
	if err := vaults.Unlink(p); err != nil {
		return 1, err
	}
	page, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "removed", fmt.Sprintf("%s; the vault is still at %s", p.Name, home.Display(p.VaultPath())))
	e.console.Step(console.OK, "refreshed", home.Display(page))
	return 0, nil
}

func (e *env) link(args []string) (int, error) {
	fs := newFlags("link", e.stderr)
	kind := fs.String("kind", "", "repo or materials (default: repo when the folder holds .git)")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) != 2 {
		return 2, errors.New("usage: claude-atlas link NAME PATH [--kind repo|materials]")
	}
	if *kind != "" && *kind != links.Repo && *kind != links.Materials {
		return 2, errors.New("--kind must be repo or materials")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	p, err := e.project(cfg, positional[0])
	if err != nil {
		return 1, err
	}
	k := *kind
	if k == "" {
		k = links.DetectKind(positional[1])
	}
	if err := vaults.AddLink(p, k, positional[1]); err != nil {
		return 1, err
	}
	page, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "linked", fmt.Sprintf("%s → %s (%s)", home.Display(home.Expand(positional[1])), p.Name, k))
	e.console.Step(console.OK, "refreshed", home.Display(page))
	return 0, nil
}

func (e *env) unlink(args []string) (int, error) {
	if len(args) != 2 {
		return 2, errors.New("usage: claude-atlas unlink NAME PATH")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	p, err := e.project(cfg, args[0])
	if err != nil {
		return 1, err
	}
	if err := vaults.RemoveLink(p, args[1]); err != nil {
		return 1, err
	}
	page, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "unlinked", fmt.Sprintf("%s from %s", home.Display(home.Expand(args[1])), p.Name))
	e.console.Step(console.OK, "refreshed", home.Display(page))
	return 0, nil
}

func (e *env) links(args []string) (int, error) {
	if len(args) != 1 {
		return 2, errors.New("usage: claude-atlas links NAME")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	p, err := e.project(cfg, args[0])
	if err != nil {
		return 1, err
	}
	state, _ := tree.ReadState(e.home.StateDir(), p.Rel)
	if len(p.Repos)+len(p.Materials) == 0 {
		e.console.Say("%s has no links; add one with `claude-atlas link %s PATH`", p.Name, p.Rel)
		return 0, nil
	}
	for _, path := range p.Repos {
		e.console.Say("  %-10s %s  %s", "repo", home.Display(home.Expand(path)), linkFacts(state, path))
	}
	for _, path := range p.Materials {
		e.console.Say("  %-10s %s  %s", "materials", home.Display(home.Expand(path)), linkFacts(state, path))
	}
	return 0, nil
}

func linkFacts(state *tree.State, path string) string {
	if state == nil {
		return "(not refreshed)"
	}
	for _, l := range state.Links {
		if l.Path == path {
			return refresh.LinkSummary(l)
		}
	}
	return "(not refreshed)"
}

func (e *env) list(args []string) (int, error) {
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	projects, problems, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return 1, err
	}
	if len(projects) == 0 && len(problems) == 0 {
		e.console.Say("no projects yet; run `claude-atlas new-vault`")
		return 0, nil
	}
	for _, p := range projects {
		heat := "?"
		if state, err := tree.ReadState(e.home.StateDir(), p.Rel); err == nil {
			heat = state.Heat
			if heat == "" {
				heat = "off"
			}
		}
		e.console.Say("  %-5s %-7s %-8s %-32s %s", heat, p.Priority, p.State, p.Rel, home.Display(p.VaultPath()))
	}
	for _, problem := range problems {
		e.console.Step(console.Fail, "tree/"+problem.Rel+".md", problem.Reason)
	}
	return 0, nil
}

func (e *env) refresh(args []string) (int, error) {
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	page, res, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	for _, problem := range res.Problems {
		e.console.Step(console.Fail, "tree/"+problem.Rel+".md", problem.Reason)
	}
	for _, r := range res.Rows {
		if !r.State.VaultOK {
			e.console.Step(console.Fail, r.Project.Rel, r.State.VaultError)
			continue
		}
		heat := r.State.Heat
		if heat == "" {
			heat = "-"
		}
		days := "?"
		if r.State.DaysIdle != nil {
			days = fmt.Sprint(*r.State.DaysIdle)
		}
		note := ""
		if r.State.Legacy {
			note = " (claude-obsidian vault; adopt it)"
		}
		e.console.Step(console.OK, r.Project.Rel, fmt.Sprintf("%s, idle %sd%s", heat, days, note))
	}
	e.console.Say("  wrote %s", home.Display(page))
	return 0, nil
}

func (e *env) lint(args []string) (int, error) {
	fs := newFlags("lint", e.stderr)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	strict := fs.Bool("strict", false, "exit 1 when there are findings")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas lint [VAULT] [--json] [--strict]")
	}
	v, err := e.openVaultArg(first(positional))
	if err != nil {
		return 1, err
	}
	report, err := lint.Run(v.Root, lint.Options{AsOf: time.Now()})
	if err != nil {
		return 1, err
	}
	if *asJSON {
		e.stdout.Write(report.JSON())
	} else {
		io.WriteString(e.stdout, report.Markdown())
	}
	if *strict && report.Summary.IssuesFound > 0 {
		return 1, nil
	}
	return 0, nil
}

func first(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

func (e *env) history(args []string) (int, error) {
	fs := newFlags("history", e.stderr)
	limit := fs.Int("n", 20, "how many operations to show")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas history [VAULT] [-n N]")
	}
	v, err := e.openVaultArg(first(positional))
	if err != nil {
		return 1, err
	}
	ops, err := txn.History(v, *limit, false)
	if err != nil {
		return 1, err
	}
	if len(ops) == 0 {
		e.console.Say("no operations yet")
		return 0, nil
	}
	for _, op := range ops {
		e.console.Say("  %s  %-34s %-9s %s", op.Date.Local().Format("2006-01-02 15:04"), op.ID, op.Kind, op.Summary)
	}
	return 0, nil
}

func (e *env) undo(args []string) (int, error) {
	if len(args) != 2 {
		return 2, errors.New("usage: claude-atlas undo VAULT OPERATION")
	}
	v, err := e.openVaultArg(args[0])
	if err != nil {
		return 1, err
	}
	op, err := txn.Find(v, args[1])
	if err != nil {
		return 1, err
	}
	ok, err := e.console.Confirm(fmt.Sprintf("Undo %s (%s: %s)?", op.ID, op.Kind, op.Summary), false)
	if err != nil {
		return 1, err
	}
	if !ok {
		return 1, vaults.ErrCancelled
	}
	res, err := txn.UndoOperation(v, args[1], time.Now())
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "undone", fmt.Sprintf("%s in commit %s", op.ID, res.Commit[:12]))
	for _, p := range res.ChangedPaths {
		e.console.Say("    %s", p)
	}
	return 0, nil
}

func (e *env) recover(args []string) (int, error) {
	if len(args) > 1 {
		return 2, errors.New("usage: claude-atlas recover [VAULT]")
	}
	v, err := e.openVaultArg(first(args))
	if err != nil {
		return 1, err
	}
	res, err := txn.Recover(v)
	if err != nil {
		return 1, err
	}
	if res == nil {
		e.console.Step(console.Skip, "recover", "nothing was interrupted")
		return 0, nil
	}
	e.console.Step(console.OK, "recovered", fmt.Sprintf("%s; restored %s", res.OperationID, strings.Join(res.Restored, ", ")))
	return 0, nil
}

func (e *env) mode(args []string) (int, error) {
	if len(args) > 2 {
		return 2, errors.New("usage: claude-atlas mode [VAULT] [generic|lyt]")
	}
	var vaultArg, modeArg string
	switch len(args) {
	case 1:
		if _, err := vault.ParseMode(args[0]); err == nil {
			modeArg = args[0]
		} else {
			vaultArg = args[0]
		}
	case 2:
		vaultArg, modeArg = args[0], args[1]
	}
	v, err := e.openVaultArg(vaultArg)
	if err != nil {
		return 1, err
	}
	if modeArg == "" {
		e.console.Say("%s", v.Config.Mode)
		return 0, nil
	}
	m, err := vault.ParseMode(modeArg)
	if err != nil {
		return 2, err
	}
	if m == v.Config.Mode {
		e.console.Step(console.Skip, "mode", "already "+string(m))
		return 0, nil
	}
	plan, err := txn.Prepare(v, txn.ConfigRequest(v, m), time.Now())
	if err != nil {
		return 1, err
	}
	res, err := txn.Apply(v, plan, time.Now())
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "mode", fmt.Sprintf("%s → %s (%s)", v.Config.Mode, m, res.OperationID))
	return 0, nil
}

// planFile is the JSON shape `apply` reads: the plan tool's arguments, with content_file
// allowed in place of content.
type planFile struct {
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
	Writes  []struct {
		Path        string `json:"path"`
		Mode        string `json:"mode"`
		Content     string `json:"content"`
		ContentFile string `json:"content_file"`
		BaseSHA256  string `json:"base_sha256"`
	} `json:"writes"`
	Sources []struct {
		ID        string   `json:"id"`
		Ingested  bool     `json:"ingested"`
		Pages     []string `json:"pages"`
		Authority string   `json:"authority"`
		Title     string   `json:"title"`
		Notes     string   `json:"notes"`
	} `json:"sources"`
}

func (e *env) apply(args []string) (int, error) {
	if len(args) != 2 {
		return 2, errors.New("usage: claude-atlas apply VAULT PLAN.json")
	}
	v, err := e.openVaultArg(args[0])
	if err != nil {
		return 1, err
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		return 1, err
	}
	var pf planFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return 1, fmt.Errorf("%s: %w", args[1], err)
	}
	req := txn.Request{Kind: txn.Kind(pf.Kind), Summary: pf.Summary}
	for _, w := range pf.Writes {
		content := []byte(w.Content)
		if w.ContentFile != "" {
			content, err = os.ReadFile(filepath.Join(filepath.Dir(args[1]), w.ContentFile))
			if err != nil {
				return 1, err
			}
		}
		req.Writes = append(req.Writes, txn.Write{Path: w.Path, Mode: txn.WriteMode(w.Mode), Content: content, BaseSHA256: w.BaseSHA256})
	}
	for _, s := range pf.Sources {
		req.Sources = append(req.Sources, ledgerUpdate(s.ID, s.Ingested, s.Pages, s.Authority, s.Title, s.Notes))
	}
	plan, err := txn.Prepare(v, req, time.Now())
	if err != nil {
		return 1, err
	}
	c := e.console
	for _, ch := range plan.Preview.Creates {
		c.Say("  create   %s", ch.Path)
	}
	for _, ch := range plan.Preview.Replaces {
		c.Say("  replace  %s", ch.Path)
	}
	for _, ch := range plan.Preview.Deletes {
		c.Say("  delete   %s", ch.Path)
	}
	for _, w := range plan.Warnings {
		c.Step(console.Fail, "warning", w)
	}
	ok, err := c.Confirm("Apply "+plan.Summary+"?", true)
	if err != nil {
		return 1, err
	}
	if !ok {
		return 1, vaults.ErrCancelled
	}
	res, err := txn.Apply(v, plan, time.Now())
	if err != nil {
		return 1, err
	}
	c.Step(console.OK, "applied", fmt.Sprintf("%s in commit %s", res.OperationID, res.Commit[:12]))
	return 0, nil
}

func (e *env) mcp(args []string) (int, error) {
	if len(args) != 0 {
		return 2, errors.New("usage: claude-atlas mcp")
	}
	project := os.Getenv("CLAUDE_PROJECT_DIR")
	if project == "" {
		project, _ = os.Getwd()
	}
	err := mcpserver.Run(context.Background(), mcpserver.Options{
		Version: Version, PluginRoot: os.Getenv("CLAUDE_PLUGIN_ROOT"), ProjectDir: project,
	})
	if err != nil {
		return 1, err
	}
	return 0, nil
}

func (e *env) hook(args []string) (int, error) {
	if len(args) != 1 {
		return 2, errors.New("usage: claude-atlas hook session-start|guard|stop")
	}
	switch args[0] {
	case "session-start":
		enabled := true
		if cfg, err := e.home.Load(); err == nil {
			enabled = cfg.ClaudeCode.SessionContext
		}
		return 0, hooks.SessionStart(e.stdin, e.stdout, os.Getenv, enabled)
	case "guard":
		return 0, hooks.Guard(e.stdin, e.stdout)
	case "stop":
		return 0, hooks.Stop(e.stdin, e.stdout, os.Getenv)
	}
	return 2, fmt.Errorf("unknown hook %q", args[0])
}

func (e *env) info(args []string) (int, error) {
	c := e.console
	row := func(label, value string) { c.Say("  %-18s %s", label, value) }
	row("claude-atlas", Version)
	if exe, err := os.Executable(); err == nil {
		row("binary", home.Display(exe))
	}
	row("home", home.Display(e.home.Root))
	row("config", home.Display(e.home.ConfigPath()))
	if !e.home.Exists() {
		row("status", "not set up; run `claude-atlas setup`")
		return 0, nil
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	row("atlas vault", home.Display(cfg.AtlasVault))
	row("overview", home.Display(filepath.Join(cfg.AtlasVault, "Overview.md")))
	row("tree", home.Display(cfg.TreeRoot()))
	row("state", home.Display(e.home.StateDir()))
	row("vaults dir", home.Display(cfg.VaultsDir))
	if inst, _ := claudecode.InstalledPlugin(cfg.Plugin.ID); inst != nil {
		row("plugin", fmt.Sprintf("%s v%s", cfg.Plugin.ID, inst.Version))
		row("  path", home.Display(inst.InstallPath))
	} else {
		row("plugin", cfg.Plugin.ID+" (not installed; run `claude-atlas setup`)")
	}
	row("  source", cfg.Plugin.Source)
	row("claude config", home.Display(claudecode.ConfigDir()))
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return 1, err
	}
	row("projects", fmt.Sprintf("%d registered", len(projects)))
	for _, p := range projects {
		row("  "+p.Rel, home.Display(p.VaultPath()))
	}
	return 0, nil
}

func (e *env) doctor(args []string) (int, error) {
	c := e.console
	line := func(label, value string) { c.Say("  %-16s %s", label, value) }
	line("home", home.Display(e.home.Root)+"  "+ternary(e.home.Exists(), "ok", "missing"))
	if !e.home.Exists() {
		return 1, nil
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	ok := true
	line("claude-atlas", Version)
	if gitx.Available() {
		line("git", "on PATH")
	} else {
		ok = false
		line("git", "missing; vault operations need it")
	}
	if claudecode.CLI() == "" {
		line("Claude Code", "`claude` is not on PATH")
	} else {
		line("Claude Code", "on PATH")
	}
	if inst, _ := claudecode.InstalledPlugin(cfg.Plugin.ID); inst != nil {
		note := ""
		if inst.Version != "" && Version != "dev" && inst.Version != Version {
			note = fmt.Sprintf("  (binary is %s; keep them in step)", Version)
		}
		line("plugin", fmt.Sprintf("v%s at %s%s", inst.Version, home.Display(inst.InstallPath), note))
	} else {
		ok = false
		line("plugin", cfg.Plugin.ID+" is not installed; run `claude-atlas setup`")
	}
	_, statErr := os.Stat(filepath.Join(cfg.AtlasVault, ".obsidian"))
	line("atlas vault", home.Display(cfg.AtlasVault)+"  "+ternary(statErr == nil, "ok", "missing"))
	line("vaults dir", home.Display(cfg.VaultsDir))
	projects, problems, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return 1, err
	}
	line("projects", fmt.Sprintf("%d registered", len(projects)))
	for _, p := range projects {
		root := p.VaultPath()
		status := "off"
		switch {
		case vault.IsVault(root):
			status = "ok"
			if v, err := vault.Open(root); err == nil {
				if pending, _ := txn.Pending(v); pending != nil {
					status = "recover"
					ok = false
				} else if !v.Repo().IsRepo() {
					status = "no git"
					ok = false
				}
			}
		case vault.IsLegacy(root):
			status = "adopt"
		default:
			ok = false
		}
		c.Say("    %-7s %-24s %s", status, p.Rel, home.Display(root))
	}
	for _, problem := range problems {
		ok = false
		c.Say("    %-7s %-24s %s", "bad", "tree/"+problem.Rel+".md", problem.Reason)
	}
	if !ok {
		return 1, nil
	}
	return 0, nil
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
