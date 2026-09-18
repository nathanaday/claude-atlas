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
	"strconv"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/actions"
	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/claudecode"
	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/describe"
	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/hooks"
	"github.com/nathanaday/claude-atlas/internal/lint"
	"github.com/nathanaday/claude-atlas/internal/mcpserver"
	"github.com/nathanaday/claude-atlas/internal/obsidian"
	"github.com/nathanaday/claude-atlas/internal/place"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/tui"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
	"github.com/nathanaday/claude-atlas/internal/wizard"
)

// Version is set at build time with -ldflags "-X .../cli.Version=v1.2.3".
var Version = "dev"

const usage = `claude-atlas: knowledge bases for Claude Code, and the projects that use them.

Usage:
  claude-atlas                       open the view: every knowledge base and project on one screen
  claude-atlas [--home DIR] [-y] <command> [options]

Getting started:
  setup                     install the plugin and create your first knowledge base
  new-knowledge NAME|PATH   create a knowledge base: an Obsidian vault with an inbox and a wiki
  adopt PATH                make an existing Obsidian or claude-obsidian vault a knowledge base
  init [PATH]               make the current folder (or PATH) a project: an atlas/ folder inside your work
                            --name N, --description TEXT, --knowledge KB or --no-knowledge

Projects (PROJECT is a name, a path, or nothing for the project you are in):
  link KB                   set the knowledge base a project uses; --project P names one
  unlink                    clear it; --project P names one
  describe PROJECT          stage a snapshot of the project into its knowledge base; the describe skill writes the page
  forget PROJECT            drop a project from the atlas; its atlas/ folder stays

Tasks:
  plant PROJECT TEXT...     plant a task: a page with status planted, from your words
                            --title T, --priority P, --phase NAME, --due DATE
  tasks [PROJECT]           list open tasks; outside a project, every project's
  task PROJECT ID           change a task: --status S, --priority P, --phase NAME, --due DATE
  phase PROJECT ACTION ...  create TITLE [--goal TEXT] [--order N], rename TITLE --to NEW,
                            reorder TITLE --order N, remove TITLE

Knowledge bases (KB is a name or a path; default: the one you are in, or your project's):
  open-vault [KB]           open a knowledge base in Obsidian
  ingest KB [PATH...]       stage new files into its inbox, then ingest them
  edit KB                   change its name or scope: --name N, --scope TEXT
  remove KB                 forget a knowledge base outside the vaults directory; the folder stays
  lint [KB]                 run the wiki health check
  stub KB [TITLE...]        create seed pages for the pages your links name but nobody has written
  history [KB]              list operations, newest first
  undo KB OPERATION         revert one operation
  recover [KB]              restore a knowledge base after an interrupted operation
  mode [KB] [MODE]          show or set the filing mode: generic or lyt
  upgrade [KB|--all]        raise a knowledge base made by an older version to the current layout
  apply KB PLAN.json        apply a plan file, for scripts

Across the atlas:
  view                      the interactive screen; the same as no command at all
  list                      every knowledge base and project
  show NAME                 everything the atlas knows about one
  open-claude NAME          start Claude Code in a knowledge base or a project; --task ID continues a task
  refresh                   read everything again and rewrite the registry
  relocate PATH             move the whole vaults directory to another folder and follow it
  config [KEY VALUE]        show the settings, or set one: new-days N
  info                      show every path and version the atlas uses
  doctor                    check the installation and everything the atlas knows

Plugin:
  mcp                       serve the atlas tools over stdio; Claude Code runs this
  hook EVENT                run a plugin hook: session-start, guard, stop
  version                   print the version

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
	if len(rest) > 0 && (rest[0] == "help" || rest[0] == "--help" || rest[0] == "-h") {
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
	if len(rest) == 0 {
		switch {
		case !c.Interactive():
			fmt.Fprint(stdout, usage)
			return 0
		case !e.home.Exists():
			fmt.Fprint(stdout, usage)
			fmt.Fprintf(stdout, "\nNo atlas yet; run `claude-atlas setup`. Afterwards, `claude-atlas` alone opens the view.\n")
			return 0
		}
		rest = []string{"view"}
	}

	var err error
	var code int
	switch rest[0] {
	case "setup":
		code, err = e.setup(rest[1:])
	case "new-knowledge":
		code, err = e.newKnowledge(rest[1:])
	case "adopt":
		code, err = e.adopt(rest[1:])
	case "init":
		code, err = e.initProject(rest[1:])
	case "link":
		code, err = e.link(rest[1:])
	case "unlink":
		code, err = e.unlink(rest[1:])
	case "describe":
		code, err = e.describe(rest[1:])
	case "forget":
		code, err = e.forget(rest[1:])
	case "plant":
		code, err = e.plant(rest[1:])
	case "tasks":
		code, err = e.tasks(rest[1:])
	case "task":
		code, err = e.task(rest[1:])
	case "phase":
		code, err = e.phase(rest[1:])
	case "view":
		code, err = e.view(rest[1:])
	case "open-vault":
		code, err = e.openVault(rest[1:])
	case "open-claude":
		code, err = e.openClaude(rest[1:])
	case "ingest":
		code, err = e.ingest(rest[1:])
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
	case "relocate":
		code, err = e.relocate(rest[1:])
	case "lint":
		code, err = e.lint(rest[1:])
	case "stub":
		code, err = e.stub(rest[1:])
	case "history":
		code, err = e.history(rest[1:])
	case "undo":
		code, err = e.undo(rest[1:])
	case "recover":
		code, err = e.recover(rest[1:])
	case "mode":
		code, err = e.mode(rest[1:])
	case "upgrade":
		code, err = e.upgrade(rest[1:])
	case "config":
		code, err = e.config(rest[1:])
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

// setFlags names the flags the user actually gave, so "" can mean "clear this field".
func setFlags(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

func first(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

// entry resolves one entry of a kind by name, id, or path. Every command scans afresh:
// the registry file is derived state, and a stale one must never decide what a command
// acts on. kind "" takes either kind.
func (e *env) entry(cfg *home.Config, arg string, kind registry.Kind) (registry.Entry, error) {
	ix, err := registry.Scan(cfg)
	if err != nil {
		return registry.Entry{}, err
	}
	return findEntry(ix, arg, kind)
}

// anyEntry resolves one entry, including one the atlas could not read. Only remove and
// forget act on those; every other command wants entry.
func (e *env) anyEntry(cfg *home.Config, arg string) (registry.Entry, error) {
	ix, err := registry.Scan(cfg)
	if err != nil {
		return registry.Entry{}, err
	}
	return findAnyEntry(ix, arg)
}

// findEntry is entry over an index the caller already has. An entry the scan could not
// read carries its own reason, which says more than "no such".
func findEntry(ix *registry.Index, arg string, kind registry.Kind) (registry.Entry, error) {
	found, err := ix.Find(arg, kind)
	if err == nil {
		return *found, nil
	}
	if !errors.Is(err, registry.ErrNotFound) {
		return registry.Entry{}, err
	}
	if bad := badEntry(ix, arg); bad != nil {
		return registry.Entry{}, fmt.Errorf("%s: %s", home.Display(bad.Path), bad.Error)
	}
	what := "knowledge base or project"
	if kind != "" {
		what = kind.Noun()
	}
	return registry.Entry{}, fmt.Errorf("no %s named %q; see `claude-atlas list`", what, arg)
}

// findAnyEntry resolves an entry by name, id, or path, and one the atlas could not read
// by its path or its folder's name.
func findAnyEntry(ix *registry.Index, arg string) (registry.Entry, error) {
	found, err := ix.Find(arg, "")
	if err == nil {
		return *found, nil
	}
	if !errors.Is(err, registry.ErrNotFound) {
		return registry.Entry{}, err
	}
	if bad := badEntry(ix, arg); bad != nil {
		return *bad, nil
	}
	return registry.Entry{}, fmt.Errorf("no knowledge base or project named %q; see `claude-atlas list`", arg)
}

// uncoveredProblems lists the scan's problems that no entry carries, so a command names
// each one once.
func uncoveredProblems(ix *registry.Index) []registry.Problem {
	covered := map[string]bool{}
	for _, en := range ix.Entries {
		covered[en.Path] = true
	}
	var out []registry.Problem
	for _, p := range ix.Problems {
		if !covered[p.Path] {
			out = append(out, p)
		}
	}
	return out
}

// badEntry finds an entry the scan could not read, by its path or its folder's name.
// Such an entry has no name of its own.
func badEntry(ix *registry.Index, arg string) *registry.Entry {
	abs, err := filepath.Abs(home.Expand(arg))
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error == "" {
			continue
		}
		if (err == nil && e.Path == abs) || strings.EqualFold(filepath.Base(e.Path), arg) {
			return e
		}
	}
	return nil
}

// refreshAll rebuilds the registry from a scan.
func (e *env) refreshAll(cfg *home.Config) ([]registry.Entry, *registry.Index, error) {
	return refresh.All(e.home, cfg, time.Now())
}

// registryEntries reads the registry the last refresh wrote, writing one first when no
// refresh has run yet.
func (e *env) registryEntries(cfg *home.Config) ([]registry.Entry, error) {
	return refresh.Entries(e.home, cfg, time.Now())
}

// refreshed is what a command says after rewriting the registry.
func refreshed(entries []registry.Entry) string {
	kbs, projects := 0, 0
	for _, en := range entries {
		switch {
		case en.Error != "":
		case en.Kind == registry.Knowledge:
			kbs++
		default:
			projects++
		}
	}
	return fmt.Sprintf("%d knowledge base%s, %d project%s", kbs, plural(kbs), projects, plural(projects))
}

// entryName is the name to show: an entry the scan could not read has only its folder.
func entryName(en registry.Entry) string {
	if en.Error != "" {
		return filepath.Base(en.Path)
	}
	return en.Name
}

// cwd is the working directory, or "" when it cannot be read.
func cwd() string { return place.Cwd() }

// projectArg resolves a project for the task and project commands: nothing means the
// project at or above the current directory; a path opens that folder; a name goes
// through the registry. It returns the registry entry when the atlas knows it, so a
// command can reach the project's knowledge base.
func (e *env) projectArg(arg string) (*project.Project, *registry.Entry, error) {
	var work string
	switch {
	case arg == "" || arg == ".":
		work = project.FindAbove(cwd())
		if work == "" {
			return nil, nil, fmt.Errorf("%w: the current directory is not inside a project; name one or run `claude-atlas init`", project.ErrNotProject)
		}
	case strings.ContainsAny(arg, `/\`) || strings.HasPrefix(arg, "~"):
		abs, err := filepath.Abs(home.Expand(arg))
		if err != nil {
			return nil, nil, err
		}
		work = project.FindAbove(abs)
		if work == "" {
			return nil, nil, fmt.Errorf("%w: %s", project.ErrNotProject, abs)
		}
	}
	var entry *registry.Entry
	if cfg, err := e.home.Load(); err == nil {
		if ix, err := registry.Scan(cfg); err == nil {
			if work == "" {
				found, err := ix.Find(arg, registry.Project)
				if err != nil {
					if errors.Is(err, registry.ErrNotFound) {
						return nil, nil, fmt.Errorf("no project named %q; see `claude-atlas list`", arg)
					}
					return nil, nil, err
				}
				work = found.Path
			}
			if found := ix.ByPath(work); found != nil && found.Error == "" {
				entry = found
			}
		}
	}
	if work == "" {
		return nil, nil, fmt.Errorf("no atlas config; name the project by its path, or run `claude-atlas setup`")
	}
	p, err := project.Open(work)
	if err != nil {
		return nil, nil, err
	}
	return p, entry, nil
}

// openVaultArg resolves a knowledge base for the in-vault commands: a name, a path, or
// the current directory, which may be inside a knowledge base or inside a project that
// uses one. It does not need the atlas to be set up when a path is given.
func (e *env) openVaultArg(arg string) (*vault.Vault, error) {
	if arg == "" {
		pl, err := place.Resolve(e.home, "", "", cwd(), false)
		if err != nil {
			return nil, fmt.Errorf("%w: the current directory is not inside a knowledge base or a project; name one or give a path", vault.ErrNotVault)
		}
		if pl.Vault == nil {
			if pl.KnowledgeError != "" {
				return nil, fmt.Errorf("%s: %s", pl.Project.Name(), pl.KnowledgeError)
			}
			return nil, fmt.Errorf("%s uses no knowledge base; link one with `claude-atlas link KB`", pl.Project.Name())
		}
		return pl.Vault, nil
	}
	if cfg, err := e.home.Load(); err == nil {
		if ix, err := registry.Scan(cfg); err == nil {
			if found, ferr := ix.Find(arg, ""); ferr == nil {
				if found.Kind == registry.Project {
					if kb := found.KnowledgePath(); kb != "" {
						return vault.Open(kb)
					}
					return nil, fmt.Errorf("%s uses no knowledge base; link one with `claude-atlas link KB`", found.Name)
				}
				return vault.Open(found.Path)
			} else if errors.Is(ferr, registry.ErrAmbiguous) {
				return nil, ferr
			}
			if bad := badEntry(ix, arg); bad != nil {
				return vault.Open(bad.Path)
			}
		}
	}
	return vault.Open(home.Expand(arg))
}

func (e *env) setup(args []string) (int, error) {
	fs := newFlags("setup", e.stderr)
	vaultsDir := fs.String("vaults-dir", "", "where knowledge bases are created (default ~/Vaults)")
	first := fs.String("first-vault", "", "name or path of the first knowledge base (default notes)")
	source := fs.String("plugin-source", "", "install the plugin from this marketplace source, e.g. a local checkout")
	noPlugin := fs.Bool("no-plugin", false, "do not run `claude plugin`")
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	opts := wizard.Options{Version: Version, VaultsDir: *vaultsDir, FirstVault: *first, PluginSource: *source, WithPlugin: !*noPlugin}
	return wizard.Run(e.home, e.console, opts)
}

func (e *env) newKnowledge(args []string) (int, error) {
	fs := newFlags("new-knowledge", e.stderr)
	name := fs.String("name", "", "display name (default: the folder's name)")
	scope := fs.String("scope", "", "one or two sentences: what this knowledge base covers")
	mode := fs.String("mode", "", "filing mode for new pages: generic (default) or lyt")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas new-knowledge NAME|PATH [--name N] [--scope TEXT] [--mode generic|lyt]")
	}
	opts := vault.Options{Name: *name, Scope: *scope}
	if *mode != "" {
		if opts.Mode, err = vault.ParseMode(*mode); err != nil {
			return 2, err
		}
	}
	arg := first(positional)
	if arg == "" && !e.console.Interactive() {
		return 2, errors.New("usage: claude-atlas new-knowledge NAME|PATH")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	if arg == "" {
		arg = e.console.Ask("Name for the knowledge base", "")
		if strings.TrimSpace(arg) == "" {
			return 1, vaults.ErrCancelled
		}
		if opts.Scope == "" {
			opts.Scope = e.console.Ask("Scope: one sentence saying what it holds", "")
		}
	}
	path, err := vaults.ResolvePath(arg, cfg.VaultsDir)
	if err != nil {
		return 1, err
	}
	if _, err := vaults.Create(path, opts, e.console, true); err != nil {
		return 1, err
	}
	return e.finishVault(cfg, path)
}

// finishVault registers a knowledge base that was just created or adopted, refreshes,
// and reports.
func (e *env) finishVault(cfg *home.Config, path string) (int, error) {
	registered, err := vaults.Register(e.home, cfg, path)
	if err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	name := filepath.Base(path)
	for _, en := range entries {
		if en.Path == path && en.Error == "" {
			name = en.Name
		}
	}
	c := e.console
	c.Say("")
	c.Step(console.OK, "created", home.Display(path))
	if registered {
		c.Step(console.OK, "registered", "in the config; it sits outside "+home.Display(cfg.VaultsDir))
	}
	c.Step(console.OK, "refreshed", refreshed(entries))
	c.Say("")
	c.Say("  Open it in Obsidian with `claude-atlas open-vault %s`, or start working:", name)
	c.Say("  claude-atlas open-claude %s    # then /claude-atlas:wiki", name)
	c.Say("  cd <your work> && claude-atlas init --knowledge %s", name)
	c.Say("")
	return 0, nil
}

func (e *env) adopt(args []string) (int, error) {
	fs := newFlags("adopt", e.stderr)
	name := fs.String("name", "", "display name (default: the folder's name)")
	scope := fs.String("scope", "", "what the knowledge base covers")
	mode := fs.String("mode", "", "filing mode when the vault has none: generic (default) or lyt")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) != 1 {
		return 2, errors.New("usage: claude-atlas adopt PATH [--name N] [--scope TEXT] [--mode generic|lyt]")
	}
	opts := vault.Options{Name: *name, Scope: *scope}
	if *mode != "" {
		if opts.Mode, err = vault.ParseMode(*mode); err != nil {
			return 2, err
		}
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	abs, err := filepath.Abs(home.Expand(positional[0]))
	if err != nil {
		return 1, err
	}
	res, err := vault.Adopt(abs, opts, time.Now())
	if err != nil {
		return 1, err
	}
	c := e.console
	switch {
	case res.AlreadyAdopted && res.Commit == "":
		c.Step(console.Skip, "adopt", "already a claude-atlas knowledge base")
	case res.WasLegacy:
		c.Step(console.OK, "adopted", "claude-obsidian vault as a knowledge base; "+setupChanges(res.Added))
	case res.FromV1:
		c.Step(console.OK, "adopted", "v1 vault as a knowledge base; "+setupChanges(res.Added))
	case res.FromV2:
		c.Step(console.OK, "adopted", "v2 vault as a knowledge base; "+setupChanges(res.Added))
	default:
		c.Step(console.OK, "adopted", "as a knowledge base; "+setupChanges(res.Added))
	}
	if res.GitInitialized {
		c.Step(console.OK, "git", "initialized; every operation is now one commit")
	}
	if res.Commit != "" {
		c.Step(console.OK, "committed", res.Commit[:12])
	}
	registered, err := vaults.Register(e.home, cfg, abs)
	if err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	if registered {
		c.Step(console.OK, "registered", "in the config; it sits outside "+home.Display(cfg.VaultsDir))
	}
	c.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

// setupChanges says what an adopt or an upgrade did to a vault's files.
func setupChanges(added []string) string {
	if len(added) == 0 {
		return "committed the files already there"
	}
	return "added " + strings.Join(added, ", ")
}

func (e *env) initProject(args []string) (int, error) {
	fs := newFlags("init", e.stderr)
	name := fs.String("name", "", "the project's name (default: the folder's name)")
	description := fs.String("description", "", "one sentence saying what the project is")
	knowledge := fs.String("knowledge", "", "the knowledge base the project uses, by name or path")
	noKnowledge := fs.Bool("no-knowledge", false, "use no knowledge base, and ask nothing")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 || (*knowledge != "" && *noKnowledge) {
		return 2, errors.New("usage: claude-atlas init [PATH] [--name N] [--description TEXT] [--knowledge KB | --no-knowledge]")
	}
	work := cwd()
	if len(positional) == 1 {
		if work, err = filepath.Abs(home.Expand(positional[0])); err != nil {
			return 1, err
		}
	}
	if err := project.CheckNew(work); err != nil {
		return 1, err
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	c := e.console
	set := setFlags(fs)
	if *name == "" {
		*name = filepath.Base(work)
	}
	if c.Interactive() && !*noKnowledge {
		if !set["name"] {
			*name = c.Ask("Name", *name)
		}
		if !set["description"] {
			*description = c.Ask("Description (one sentence, or leave empty)", "")
		}
		if *knowledge == "" {
			ix, err := registry.Scan(cfg)
			if err != nil {
				return 1, err
			}
			kbs := ix.Knowledge()
			if len(kbs) > 0 {
				c.Say("Knowledge bases on this machine:")
				for i, kb := range kbs {
					c.Say("  %d. %-20s %s", i+1, kb.Name, kb.Scope)
				}
				answer := c.Ask("Use one? (a number, a name, or empty for none)", "")
				if n, err := strconv.Atoi(strings.TrimSpace(answer)); err == nil && n >= 1 && n <= len(kbs) {
					*knowledge = kbs[n-1].Name
				} else if strings.TrimSpace(answer) != "" {
					*knowledge = strings.TrimSpace(answer)
				}
			}
		}
	}
	acts := actions.Bind(e.home, cfg, c)
	p, written, err := acts.InitProject(actions.InitProject{Work: work, Name: *name, Description: *description, Knowledge: *knowledge})
	if err != nil {
		return 1, err
	}
	c.Say("")
	c.Step(console.OK, "project", fmt.Sprintf("%s at %s", p.Name(), home.Display(p.Root)))
	c.Step(console.OK, "wrote", project.Dir+"/: "+strings.Join(written, ", "))
	if p.Config.Knowledge != nil {
		c.Step(console.OK, "knowledge", p.Config.Knowledge.Name)
	} else {
		c.Step(console.Skip, "knowledge", "none; `claude-atlas link KB` sets one")
	}
	c.Step(console.OK, "registered", "in "+home.Display(e.home.ConfigPath()))
	entries, ix, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	c.Step(console.OK, "refreshed", refreshed(entries))
	ref, here := ".", work == cwd()
	if !here {
		ref = entryArg(ix, p.Root, registry.Project)
	}
	var next [][2]string
	if p.Config.Knowledge != nil {
		next = append(next, [2]string{"claude-atlas describe " + entryArg(ix, p.Root, registry.Project), "a page about this project in " + p.Config.Knowledge.Name})
	}
	next = append(next, [2]string{"claude-atlas plant " + ref + ` "..."`, "or a note in " + project.Dir + "/" + project.InboxDir + "/"})
	if here {
		next = append(next, [2]string{"claude", "Claude Code here sees the project and its tasks"})
	} else {
		next = append(next, [2]string{"claude-atlas open-claude " + entryArg(ix, p.Root, ""), "Claude Code in the project sees it and its tasks"})
	}
	c.Say("")
	c.Say("  Next:")
	sayCommands(c, next)
	c.Say("")
	return 0, nil
}

// sayCommands prints commands with their comments in one column.
func sayCommands(c *console.Console, lines [][2]string) {
	width := 0
	for _, l := range lines {
		width = max(width, len(l[0]))
	}
	for _, l := range lines {
		c.Say("  %-*s  # %s", width, l[0], l[1])
	}
}

// entryArg is the shortest command argument that names the entry at path: its name
// when the name finds it, else its path. It comes quoted for a shell when it must be.
func entryArg(ix *registry.Index, path string, kind registry.Kind) string {
	en := ix.ByPath(path)
	if en != nil && en.Error == "" {
		if found, err := ix.Find(en.Name, kind); err == nil && found.Path == path {
			return shellArg(en.Name)
		}
	}
	display := home.Display(path)
	if rest, ok := strings.CutPrefix(display, "~/"); ok {
		return "~/" + shellArg(rest)
	}
	return shellArg(display)
}

// shellArg quotes s for a POSIX shell unless every byte is safe bare.
func shellArg(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-+/=:@,%") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (e *env) link(args []string) (int, error) {
	fs := newFlags("link", e.stderr)
	projectArg := fs.String("project", "", "the project, by name or path (default: the one you are in)")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) != 1 {
		return 2, errors.New("usage: claude-atlas link KB [--project P]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	p, _, err := e.projectArg(*projectArg)
	if err != nil {
		return 1, err
	}
	kb, err := vaults.LinkKnowledge(cfg, p, positional[0])
	if err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "linked", fmt.Sprintf("%s uses %s", p.Name(), kb.Name))
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) unlink(args []string) (int, error) {
	fs := newFlags("unlink", e.stderr)
	projectArg := fs.String("project", "", "the project, by name or path (default: the one you are in)")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) != 0 {
		return 2, errors.New("usage: claude-atlas unlink [--project P]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	p, _, err := e.projectArg(*projectArg)
	if err != nil {
		return 1, err
	}
	was := ""
	if p.Config.Knowledge != nil {
		was = p.Config.Knowledge.Name
	}
	if err := vaults.UnlinkKnowledge(p); err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "unlinked", fmt.Sprintf("%s no longer uses %s", p.Name(), was))
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) forget(args []string) (int, error) {
	if len(args) != 1 {
		return 2, errors.New("usage: claude-atlas forget PROJECT")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.anyEntry(cfg, args[0])
	if err != nil {
		return 1, err
	}
	if entry.Error == "" && entry.Kind != registry.Project {
		return 1, fmt.Errorf("%s is a knowledge base; `claude-atlas remove` forgets one", entry.Name)
	}
	name, where := entryName(entry), home.Display(entry.Path)
	gone := entry.Reason == registry.ReasonMissing
	question := fmt.Sprintf("Forget %s? The folder %s and its %s/ stay.", name, where, project.Dir)
	if gone {
		question = fmt.Sprintf("Forget %s? Its folder %s is already gone.", name, where)
	}
	ok, err := e.console.Confirm(question, false)
	if err != nil {
		return 1, err
	}
	if !ok {
		return 1, vaults.ErrCancelled
	}
	if err := vaults.ForgetProject(e.home, cfg, entry.Path); err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "forgot", name)
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) describe(args []string) (int, error) {
	fs := newFlags("describe", e.stderr)
	noClaude := fs.Bool("no-claude", false, "stage the snapshot but do not start Claude Code")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas describe [PROJECT] [--no-claude]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	p, entry, err := e.projectArg(first(positional))
	if err != nil {
		return 1, err
	}
	if entry == nil {
		return 1, fmt.Errorf("%s is not in the atlas; run `claude-atlas init` there", p.Name())
	}
	snap, err := actions.Bind(e.home, cfg, e.console).StageProject(*entry)
	if err != nil {
		return 1, err
	}
	c := e.console
	if snap.Described != nil {
		c.Say("  %-10s %s", "page", snap.Described.Summary())
	}
	at := snap.Commit
	if len(at) > 7 {
		at = at[:7]
	}
	if at == "" {
		at = "no commit"
	}
	switch {
	case snap.New:
		c.Step(console.OK, "staged", fmt.Sprintf("%s: %s at %s, in %s", strings.TrimPrefix(snap.To, "inbox/"), p.Name(), at, entry.Knowledge.Name))
	default:
		c.Say("  %-10s %s already waits or was captured", "unchanged", strings.TrimPrefix(snap.To, "inbox/"))
	}
	return e.offerClaude(cfg, p.Root, *noClaude, claudecode.DescribePrompt)
}

// offerClaude ends a staging command: it names the skill to run next, and in a terminal
// offers to start Claude Code on it in dir.
func (e *env) offerClaude(cfg *home.Config, dir string, noClaude bool, skill string) (int, error) {
	c := e.console
	if noClaude || !c.Interactive() {
		c.Say("  Next: start Claude Code in %s, then %s", home.Display(dir), skill)
		return 0, nil
	}
	c.Say("  %s", trustNote)
	ok, err := c.Confirm("Start Claude Code now and run "+skill+"?", true)
	if err != nil {
		return 1, err
	}
	if !ok {
		c.Say("  Next: start Claude Code in %s, then %s", home.Display(dir), skill)
		return 0, nil
	}
	cmd, err := claudecode.LaunchCommand(cfg.ClaudeCode, dir, skill)
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

func (e *env) plant(args []string) (int, error) {
	fs := newFlags("plant", e.stderr)
	title := fs.String("title", "", "the task's title; taken from the text when omitted")
	priority := fs.String("priority", "", "high, normal, low, or someday")
	phase := fs.String("phase", "", "the phase the task belongs to")
	due := fs.String("due", "", "YYYY-MM-DD")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) < 2 {
		return 2, errors.New("usage: claude-atlas plant PROJECT TEXT... [--title T] [--priority P] [--phase NAME] [--due DATE]")
	}
	p, _, err := e.projectArg(positional[0])
	if err != nil {
		return 1, err
	}
	t, err := tasks.PlantTask(p, tasks.Plant{Title: *title, Text: strings.Join(positional[1:], " "), Priority: *priority, Phase: *phase, Due: *due}, time.Now())
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "planted", fmt.Sprintf("%s (%s) in %s", t.Title, t.ID, p.Name()))
	e.console.Say("  %s", home.Display(p.Path(t.Path)))
	return 0, nil
}

func (e *env) tasks(args []string) (int, error) {
	fs := newFlags("tasks", e.stderr)
	all := fs.Bool("all", false, "include done and cancelled tasks")
	status := fs.String("status", "", "only tasks with this status")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas tasks [PROJECT] [--all] [--status S]")
	}
	now := time.Now()
	p, _, err := e.projectArg(first(positional))
	if err != nil && len(positional) == 0 && errors.Is(err, project.ErrNotProject) {
		return e.allTasks(now, *all, *status)
	}
	if err != nil {
		return 1, err
	}
	board, err := tasks.Load(p)
	if err != nil {
		return 1, err
	}
	e.printTasks(board, now, *all, *status, "")
	if notes := tasks.Notes(p); len(notes) > 0 {
		e.console.Say("  %d task note%s waiting in %s/%s/: %s", len(notes), plural(len(notes)), project.Dir, project.InboxDir, strings.Join(notes, ", "))
	}
	for _, pr := range board.Problems {
		e.console.Step(console.Fail, pr.Path, pr.Reason)
	}
	return 0, nil
}

// allTasks lists the open tasks of every project the atlas knows.
func (e *env) allTasks(now time.Time, all bool, status string) (int, error) {
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		return 1, err
	}
	shown := 0
	for _, en := range ix.Projects() {
		p, err := project.Open(en.Path)
		if err != nil {
			continue
		}
		board, err := tasks.Load(p)
		if err != nil {
			continue
		}
		list := board.Open()
		if all {
			list = append(list, board.Archived()...)
		}
		if len(list) == 0 {
			continue
		}
		shown += len(list)
		e.printTasks(board, now, all, status, en.Name)
	}
	if shown == 0 {
		e.console.Say("no open tasks in any project; plant one with `claude-atlas plant PROJECT \"...\"`")
	}
	return 0, nil
}

// printTasks lists a board: open tasks grouped by phase in phase order, then the ones
// with no phase, then the archive when asked.
func (e *env) printTasks(board *tasks.Board, now time.Time, all bool, status, name string) {
	if name != "" {
		e.console.Say("%s", name)
	}
	open := board.Open()
	if len(open) == 0 && !all {
		e.console.Say("  no open tasks")
		return
	}
	row := func(t tasks.Task) {
		if status != "" && t.Status != status {
			return
		}
		notes := ""
		if tasks.Stale(t, now) {
			notes = "  stale"
		}
		if t.Due != "" {
			notes += "  due " + t.Due
		}
		e.console.Say("  %-9s %-8s %-40s %s  %s%s", t.Status, t.Priority, t.Title, t.ID, dash(t.Updated), notes)
	}
	for _, ph := range board.Phases {
		in := board.In(ph.Title)
		if len(in) == 0 {
			continue
		}
		e.console.Say("  %s (%d)", ph.Title, len(in))
		for _, t := range in {
			row(t)
		}
	}
	if unphased := board.Unphased(); len(unphased) > 0 {
		if len(board.Phases) > 0 {
			e.console.Say("  no phase")
		}
		for _, t := range unphased {
			row(t)
		}
	}
	if all {
		if archived := board.Archived(); len(archived) > 0 {
			e.console.Say("  archive")
			for _, t := range archived {
				row(t)
			}
		}
	}
}

func (e *env) task(args []string) (int, error) {
	fs := newFlags("task", e.stderr)
	status := fs.String("status", "", strings.Join(tasks.Statuses, ", "))
	priority := fs.String("priority", "", strings.Join(tasks.Priorities, ", "))
	phase := fs.String("phase", "", "the phase the task belongs to; \"\" clears it")
	due := fs.String("due", "", "YYYY-MM-DD; \"\" clears it")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	set := setFlags(fs)
	if len(positional) != 2 || len(set) == 0 {
		return 2, errors.New("usage: claude-atlas task PROJECT ID|TITLE [--status S] [--priority P] [--phase NAME] [--due DATE]")
	}
	p, _, err := e.projectArg(positional[0])
	if err != nil {
		return 1, err
	}
	var ch tasks.Changes
	if set["status"] {
		ch.Status = status
	}
	if set["priority"] {
		ch.Priority = priority
	}
	if set["phase"] {
		ch.Phase = phase
	}
	if set["due"] {
		ch.Due = due
	}
	t, err := tasks.Set(p, positional[1], ch, time.Now())
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, t.Status, fmt.Sprintf("%s (%s) · %s · %s", t.Title, t.ID, t.Priority, dash(t.Phase)))
	e.console.Say("  %s", home.Display(p.Path(t.Path)))
	return 0, nil
}

func (e *env) phase(args []string) (int, error) {
	fs := newFlags("phase", e.stderr)
	goal := fs.String("goal", "", "what the phase delivers")
	order := fs.Int("order", 0, "where the phase sits in the timeline")
	to := fs.String("to", "", "the new title, for rename")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	set := setFlags(fs)
	if len(positional) != 3 {
		return 2, errors.New("usage: claude-atlas phase PROJECT create|rename|reorder|remove TITLE [--goal TEXT] [--order N] [--to NEW]")
	}
	p, _, err := e.projectArg(positional[0])
	if err != nil {
		return 1, err
	}
	title := positional[2]
	now := time.Now()
	switch positional[1] {
	case "create":
		var n *int
		if set["order"] {
			n = order
		}
		ph, err := tasks.CreatePhase(p, title, *goal, n, now)
		if err != nil {
			return 1, err
		}
		e.console.Step(console.OK, "created", fmt.Sprintf("phase %s (order %d) · %s", ph.Title, ph.Order, home.Display(p.Path(ph.Path))))
	case "rename":
		if *to == "" {
			return 2, errors.New("rename needs --to NEW")
		}
		ph, err := tasks.RenamePhase(p, title, *to, now)
		if err != nil {
			return 1, err
		}
		e.console.Step(console.OK, "renamed", fmt.Sprintf("%s is now %s", title, ph.Title))
	case "reorder":
		if !set["order"] {
			return 2, errors.New("reorder needs --order N")
		}
		ph, err := tasks.ReorderPhase(p, title, *order, now)
		if err != nil {
			return 1, err
		}
		e.console.Step(console.OK, "reordered", fmt.Sprintf("%s is now order %d", ph.Title, ph.Order))
	case "remove":
		if err := tasks.RemovePhase(p, title, now); err != nil {
			return 1, err
		}
		e.console.Step(console.OK, "removed", "phase "+title)
	default:
		return 2, fmt.Errorf("unknown action %q; create, rename, reorder, or remove", positional[1])
	}
	return 0, nil
}

func (e *env) view(args []string) (int, error) {
	if !e.console.Interactive() {
		return 2, errors.New("view is an interactive screen and needs a terminal")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	acts := actions.Bind(e.home, cfg, e.console)
	entries, err := acts.Load()
	if err != nil {
		return 1, err
	}
	opener := tui.Opener{
		Obsidian: func(path string) error { return obsidian.RegisterAndOpen(path) },
		Claude: func(path string) error {
			cmd, err := claudecode.LaunchCommand(cfg.ClaudeCode, path, "")
			if err != nil {
				return err
			}
			return cmd.Run()
		},
	}
	changed, err := tui.RunView(tui.Items(entries), opener, acts)
	if err != nil {
		return 1, err
	}
	if !changed {
		return 0, nil
	}
	entries, _, err = e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) openVault(args []string) (int, error) {
	fs := newFlags("open-vault", e.stderr)
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas open-vault [KB | PATH]")
	}
	v, err := e.openVaultArg(first(positional))
	if err != nil {
		return 1, err
	}
	root, label := v.Root, v.Name()
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
func skillHint(kind registry.Kind) string {
	if kind == registry.Knowledge {
		return "skills: " + hooks.KnowledgeSkills
	}
	return "skills: " + hooks.ProjectSkills
}

// trustNote explains Claude Code's own first-run dialog, whose default answer quits.
const trustNote = "The first time in a folder, Claude Code asks whether you trust it; choose Yes."

func (e *env) openClaude(args []string) (int, error) {
	fs := newFlags("open-claude", e.stderr)
	taskID := fs.String("task", "", "continue this task: start with /claude-atlas:task-run as the first message")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) != 1 {
		return 2, errors.New("usage: claude-atlas open-claude NAME [--task ID]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.entry(cfg, positional[0], "")
	if err != nil {
		return 1, err
	}
	if !e.console.Interactive() {
		return 2, errors.New("open-claude starts an interactive Claude Code session and needs a terminal")
	}
	prompt := ""
	if *taskID != "" {
		if entry.Kind != registry.Project {
			return 1, fmt.Errorf("%s is a knowledge base; tasks live in a project", entry.Name)
		}
		p, err := project.Open(entry.Path)
		if err != nil {
			return 1, err
		}
		t, err := findTask(p, *taskID)
		if err != nil {
			return 1, err
		}
		prompt = claudecode.TaskPrompt(t.ID)
		e.console.Say("  task: %s (%s)", t.Title, t.Status)
	}
	cmd, err := claudecode.LaunchCommand(cfg.ClaudeCode, entry.Path, prompt)
	if err != nil {
		return 1, err
	}
	e.console.Say("  %s", home.Display(cmd.Dir))
	e.console.Say("  %s", skillHint(entry.Kind))
	e.console.Say("  %s", trustNote)
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

// findTask finds a task in a project by id, or by the start of its title.
func findTask(p *project.Project, key string) (*tasks.Task, error) {
	board, err := tasks.Load(p)
	if err != nil {
		return nil, err
	}
	if t := board.Find(key); t != nil {
		return t, nil
	}
	var matches []*tasks.Task
	for i := range board.Tasks {
		if strings.HasPrefix(strings.ToLower(board.Tasks[i].Title), strings.ToLower(key)) {
			matches = append(matches, &board.Tasks[i])
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, fmt.Errorf("no task %q in %s; see `claude-atlas tasks %s`", key, p.Name(), p.Name())
	}
	return nil, fmt.Errorf("%q matches several tasks in %s; use the id", key, p.Name())
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
		return 2, errors.New("usage: claude-atlas ingest KB [PATH ...] [--dry-run] [--no-claude]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	v, err := e.openVaultArg(positional[0])
	if err != nil {
		return 1, err
	}
	entry, err := e.entry(cfg, v.Root, registry.Knowledge)
	if err != nil {
		return 1, err
	}
	c := e.console
	sources, err := capture.SourcesFor(v, positional[1:])
	if err != nil {
		return 1, err
	}
	plan, err := capture.PlanStage(v, sources, time.Now())
	if err != nil {
		return 1, err
	}
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
	if plan.Waiting > 0 {
		c.Say("  %-10s %d file%s waiting to be ingested", "inbox", plan.Waiting, plural(plan.Waiting))
	}
	if *dryRun {
		return 0, nil
	}
	if len(plan.New) == 0 && plan.Waiting == 0 {
		c.Say("  nothing to ingest")
		return 0, nil
	}
	if len(plan.New) > 0 {
		question := fmt.Sprintf("Stage %d file%s into inbox/", len(plan.New), plural(len(plan.New)))
		ok, err := c.Confirm(question+"?", true)
		if err != nil {
			return 1, err
		}
		if !ok {
			return 1, vaults.ErrCancelled
		}
		res, remembered, err := actions.Bind(e.home, cfg, e.console).Stage(entry, plan)
		if err != nil {
			return 1, err
		}
		c.Step(console.OK, "staged", fmt.Sprintf("%d file%s in %s", len(res.Staged), plural(len(res.Staged)), home.Display(v.Path("inbox"))))
		for _, dir := range remembered {
			c.Step(console.OK, "remembered", home.Display(dir)+"; `claude-atlas ingest "+entry.Name+"` stages what is new there next time")
		}
	} else {
		c.Say("  nothing new to stage; %d file%s already waiting", plan.Waiting, plural(plan.Waiting))
	}
	return e.offerClaude(cfg, v.Root, *noClaude, claudecode.IngestPrompt)
}

func (e *env) list(args []string) (int, error) {
	if len(args) != 0 {
		return 2, errors.New("usage: claude-atlas list")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entries, err := e.registryEntries(cfg)
	if err != nil {
		return 1, err
	}
	if len(entries) == 0 {
		e.console.Say("nothing yet; create a knowledge base with `claude-atlas new-knowledge NAME`, then `claude-atlas init` in your work")
		return 0, nil
	}
	for _, en := range entries {
		e.console.Say("  %-9s %-7s %-24s %s", listKind(en), listHeat(en), entryName(en), home.Display(en.Path))
	}
	for _, en := range entries {
		if en.Error != "" {
			e.console.Step(console.Fail, entryName(en), en.Error)
		}
	}
	return 0, nil
}

// listKind is the kind column: an entry the scan could not read has none.
func listKind(en registry.Entry) string {
	if en.Error != "" {
		return "?"
	}
	return string(en.Kind)
}

// unreadable is the one word for an entry the atlas knows but could not read.
func unreadable(en registry.Entry) string {
	switch en.Reason {
	case registry.ReasonV1:
		return "v1"
	case registry.ReasonV2Project:
		return "v2"
	case registry.ReasonMissing:
		return "missing"
	default:
		return "bad"
	}
}

// listHeat is the heat column: what the last refresh found, or why there is nothing.
func listHeat(en registry.Entry) string {
	if en.Error != "" {
		return unreadable(en)
	}
	if en.State == nil {
		return "?"
	}
	if en.State.Heat == "" {
		return "off"
	}
	return en.State.Heat
}

func (e *env) show(args []string) (int, error) {
	if len(args) != 1 {
		return 2, errors.New("usage: claude-atlas show NAME")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		return 1, err
	}
	entry, err := findEntry(ix, args[0], "")
	if err != nil {
		return 1, err
	}
	if stored, _, err := registry.Read(e.home.StateDir()); err == nil {
		for _, s := range stored {
			if s.ID == entry.ID {
				entry.State = s.State
			}
		}
	}
	c := e.console
	row := func(k, val string) {
		if val == "" {
			val = "—"
		}
		c.Say("  %-16s %s", k, val)
	}
	row("Name", entry.Name)
	row("Kind", entry.Kind.Noun())
	row("Id", entry.ID)
	row("Path", home.Display(entry.Path))
	row("Created", entry.Created)
	if entry.Kind == registry.Knowledge {
		row("Mode", string(entry.Mode))
		row("Scope", entry.Scope)
		for _, p := range entry.Projects {
			row("Project", fmt.Sprintf("%-20s %s", p.Name, home.Display(p.Path)))
		}
	} else {
		row("Description", entry.Description)
		switch {
		case entry.Knowledge == nil:
			row("Knowledge", "none")
		case entry.Knowledge.Error != "":
			row("Knowledge", entry.Knowledge.Name+"  "+entry.Knowledge.Error)
		default:
			row("Knowledge", fmt.Sprintf("%-20s %s", entry.Knowledge.Name, home.Display(entry.Knowledge.Path)))
			if d := describe.Page(entry); d != nil {
				row("Page", d.Summary())
			} else {
				row("Page", registry.NotDescribed)
			}
		}
	}
	state := entry.State
	if state == nil {
		row("Refreshed", "never; run `claude-atlas refresh`")
		return 0, nil
	}
	if state.OK {
		row("Check", "ok")
	} else {
		row("Check", state.Error)
	}
	heat := state.Heat
	if heat == "" {
		heat = "unknown"
	}
	row("Heat", heat)
	row("Last touched", state.LastTouched)
	if state.DaysIdle != nil {
		row("Idle", fmt.Sprintf("%d day%s", *state.DaysIdle, plural(*state.DaysIdle)))
	}
	if entry.Kind == registry.Knowledge {
		row("Last operation", state.LastOperation)
		if state.Pages != nil {
			row("Pages", fmt.Sprint(*state.Pages))
		}
		if state.Inbox != nil {
			row("Inbox", fmt.Sprintf("%d waiting", *state.Inbox))
		}
		row("Unfinished", state.Unfinished.Text())
		for i, t := range state.OpenThreads {
			label := "Open threads"
			if i > 0 {
				label = ""
			}
			c.Say("  %-16s - %s", label, refresh.PlainText(t))
		}
	} else {
		if state.Git != nil {
			row("Git", refresh.LinkSummary(*state.Git))
		}
		// The task pages are cheap to read and change without a refresh, so they are
		// read now rather than from the registry.
		if p, err := project.Open(entry.Path); err == nil {
			if board, err := tasks.Load(p); err == nil {
				counts := board.Counts(time.Now())
				counts.Notes = len(tasks.Notes(p))
				row("Tasks", taskCounts(counts))
				var phases []string
				for _, ph := range board.Phases {
					name := ph.Title
					if board.Finished(ph.Title) {
						name += " (finished)"
					}
					phases = append(phases, name)
				}
				if len(phases) > 0 {
					row("Phases", strings.Join(phases, ", "))
				}
			}
		}
	}
	row("Refreshed", state.GeneratedAt)
	for _, signal := range refresh.Signals(entry, time.Now()) {
		row("Signal", signal)
	}
	return 0, nil
}

// taskCounts is one line of a project's tasks.
func taskCounts(c tasks.Counts) string {
	line := fmt.Sprintf("%d open (active %d, blocked %d, planned %d, planted %d)", c.Open, c.Active, c.Blocked, c.Planned, c.Planted)
	if c.Notes > 0 {
		line += fmt.Sprintf(" · %d note%s waiting", c.Notes, plural(c.Notes))
	}
	return line
}

func (e *env) edit(args []string) (int, error) {
	fs := newFlags("edit", e.stderr)
	name := fs.String("name", "", "display name")
	scope := fs.String("scope", "", "what a knowledge base covers; \"\" clears it")
	description := fs.String("description", "", "what a project is; \"\" clears it")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	set := setFlags(fs)
	if len(positional) != 1 || len(set) == 0 {
		return 2, errors.New("usage: claude-atlas edit NAME [--name N] [--scope TEXT] [--description TEXT]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.entry(cfg, positional[0], "")
	if err != nil {
		return 1, err
	}
	acts := actions.Bind(e.home, cfg, e.console)
	path := entry.Path
	if entry.Kind == registry.Project {
		if set["scope"] {
			return 2, errors.New("a project has a description, not a scope")
		}
		change := vaults.ProjectEdit{Name: *name}
		if set["description"] {
			change.Description = description
		}
		if err := acts.EditProject(entry, change); err != nil {
			return 1, err
		}
	} else {
		if set["description"] {
			return 2, errors.New("a knowledge base has a scope, not a description")
		}
		change := vaults.Edit{Name: *name}
		if set["scope"] {
			change.Scope = scope
		}
		if path, err = acts.EditKnowledge(entry, change); err != nil {
			return 1, err
		}
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	shown := entry.Name
	if *name != "" {
		shown = *name
	}
	e.console.Step(console.OK, "edited", shown)
	if path != entry.Path {
		e.console.Step(console.OK, "moved", home.Display(path))
	}
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) remove(args []string) (int, error) {
	if len(args) != 1 {
		return 2, errors.New("usage: claude-atlas remove KB")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.anyEntry(cfg, args[0])
	if err != nil {
		return 1, err
	}
	if entry.Error == "" && entry.Kind == registry.Project {
		return 1, fmt.Errorf("%s is a project; `claude-atlas forget` drops one", entry.Name)
	}
	// Ask nothing when the answer cannot be acted on.
	if err := vaults.CheckForget(cfg, entry.Path); err != nil {
		return 1, err
	}
	name, where := entryName(entry), home.Display(entry.Path)
	gone := entry.Reason == registry.ReasonMissing
	question := fmt.Sprintf("Forget %s? The vault at %s stays on disk.", name, where)
	if gone {
		question = fmt.Sprintf("Forget %s? Its folder %s is already gone.", name, where)
	}
	ok, err := e.console.Confirm(question, false)
	if err != nil {
		return 1, err
	}
	if !ok {
		return 1, vaults.ErrCancelled
	}
	if err := vaults.Unregister(e.home, cfg, entry.Path); err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "removed", fmt.Sprintf("%s; %s", name, ternary(gone, "its folder was already gone", "the vault is still at "+where)))
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

// warnExamples is how many paths a preview shows per kind before it counts the rest.
const warnExamples = 3

// relTo shortens a path under the vaults directory to what follows it, so a preview
// reads as a list of places inside the tree rather than a column of identical prefixes.
func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return home.Display(path)
}

// relocate moves the whole vaults directory. It shows what moves, what the config will
// say afterwards, and what holds the old path that the atlas will not change; then it
// asks, moves, and refreshes so the registry follows.
func (e *env) relocate(args []string) (int, error) {
	if len(args) != 1 {
		return 2, errors.New("usage: claude-atlas relocate PATH")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	p, err := vaults.PlanRelocate(e.home, cfg, args[0])
	if err != nil {
		return 1, err
	}
	c := e.console
	how := "a rename on the same volume"
	if !p.SameVolume {
		how = "a copy to another volume, verified before the old folder goes"
	}
	c.Say("claude-atlas will move %d vault%s from %s to %s:", p.Vaults, plural(p.Vaults), home.Display(p.From), home.Display(p.To))
	c.Say("    %d files, %s, %s", p.Files, console.Size(p.Bytes), how)
	c.Say("")
	c.Say("  The config will say:")
	for _, r := range p.Rewrites {
		c.Say("    %-24s %s", r.What, home.Display(r.To))
	}
	if groups := p.GroupWarnings(); len(groups) > 0 {
		c.Say("")
		c.Say("  These hold the old path and claude-atlas does not change them:")
		for _, g := range groups {
			c.Say("    %d %s", len(g.Paths), g.Reason)
			for i, path := range g.Paths {
				if i == warnExamples {
					c.Say("        and %d more", len(g.Paths)-warnExamples)
					break
				}
				c.Say("        %s", relTo(p.From, path))
			}
		}
	}
	c.Say("")
	ok, err := c.Confirm("Move the vaults?", false)
	if err != nil {
		return 1, err
	}
	if !ok {
		return 1, vaults.ErrCancelled
	}
	if err := vaults.ApplyRelocate(e.home, cfg, p); err != nil {
		return 1, err
	}
	c.Step(console.OK, "moved", fmt.Sprintf("%d vault%s to %s", p.Vaults, plural(p.Vaults), home.Display(p.To)))
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	c.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) refresh(args []string) (int, error) {
	if len(args) != 0 {
		return 2, errors.New("usage: claude-atlas refresh")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entries, ix, err := refresh.Registry(e.home, cfg, e.home.StateDir(), time.Now())
	if err != nil {
		return 1, err
	}
	for _, en := range entries {
		switch {
		case en.Error != "":
			e.console.Step(console.Fail, entryName(en), en.Error)
		case en.State == nil:
			e.console.Step(console.Fail, en.Name, "not read")
		case !en.State.OK:
			e.console.Step(console.Fail, en.Name, en.State.Error)
		default:
			heat := en.State.Heat
			if heat == "" {
				heat = "-"
			}
			days := "?"
			if en.State.DaysIdle != nil {
				days = fmt.Sprint(*en.State.DaysIdle)
			}
			e.console.Step(console.OK, en.Name, fmt.Sprintf("%s · %s, idle %sd", en.Kind.Noun(), heat, days))
		}
	}
	for _, problem := range uncoveredProblems(ix) {
		e.console.Step(console.Fail, home.Display(problem.Path), problem.Reason)
	}
	e.console.Say("  wrote %s", home.Display(registry.File(e.home.StateDir())))
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
		return 2, errors.New("usage: claude-atlas lint [KB] [--json] [--strict]")
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

func (e *env) history(args []string) (int, error) {
	fs := newFlags("history", e.stderr)
	limit := fs.Int("n", 20, "how many operations to show")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas history [KB] [-n N]")
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

func (e *env) stub(args []string) (int, error) {
	fs := newFlags("stub", e.stderr)
	pageType := fs.String("type", "", "the type of every stub: concept or entity; in lyt mode note or moc")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) < 1 {
		return 2, errors.New("usage: claude-atlas stub KB [TITLE...] [--type T]")
	}
	v, err := e.openVaultArg(positional[0])
	if err != nil {
		return 1, err
	}
	var titles []txn.StubTitle
	for _, title := range positional[1:] {
		titles = append(titles, txn.StubTitle{Title: title})
	}
	res, err := txn.StubPages(v, titles, *pageType, time.Now())
	if err != nil {
		return 1, err
	}
	for _, s := range res.Skipped {
		e.console.Step(console.Skip, "skipped", fmt.Sprintf("%s: %s", s.Title, s.Reason))
	}
	if len(res.Stubs) == 0 {
		if len(res.Skipped) == 0 {
			e.console.Step(console.Skip, "nothing to stub", "every page the wiki links to exists")
		}
		return 0, nil
	}
	for _, s := range res.Stubs {
		e.console.Step(console.OK, "stubbed", fmt.Sprintf("%s (%s)", s.Path, s.Type))
	}
	e.console.Step(console.OK, "committed", res.OperationID)
	return 0, nil
}

func (e *env) upgrade(args []string) (int, error) {
	fs := newFlags("upgrade", e.stderr)
	all := fs.Bool("all", false, "every knowledge base the atlas knows")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 || (*all && len(positional) > 0) {
		return 2, errors.New("usage: claude-atlas upgrade [KB] or claude-atlas upgrade --all")
	}
	var roots []string
	if *all {
		cfg, err := e.home.Load()
		if err != nil {
			return 1, err
		}
		ix, err := registry.Scan(cfg)
		if err != nil {
			return 1, err
		}
		for _, en := range ix.Entries {
			if en.Error == "" && en.Kind != registry.Knowledge {
				continue
			}
			roots = append(roots, en.Path)
		}
	} else {
		v, err := e.openVaultArg(first(positional))
		if err != nil {
			return 1, err
		}
		roots = []string{v.Root}
	}
	restyled := false
	for _, root := range roots {
		res, err := vault.Upgrade(root, time.Now())
		if err != nil {
			if *all && (errors.Is(err, vault.ErrV1) || errors.Is(err, vault.ErrProjectVault) || errors.Is(err, vault.ErrNotVault)) {
				e.console.Step(console.Skip, home.Display(root), err.Error())
				continue
			}
			return 1, err
		}
		if len(res.Added) == 0 && !res.Schema {
			e.console.Step(console.Skip, home.Display(root), "current")
			continue
		}
		what := setupChanges(res.Added)
		if res.Schema {
			what = "raised to " + vault.Schema + "; " + what
		}
		e.console.Step(console.OK, home.Display(root), what)
		for _, rel := range res.Added {
			if strings.HasPrefix(rel, ".obsidian/snippets/") {
				restyled = true
			}
		}
	}
	if restyled {
		e.console.Say("  The folder colors come from .obsidian/snippets/claude-atlas.css, enabled in each vault's appearance settings; reload Obsidian (Cmd+R) to see them.")
	}
	return 0, nil
}

func (e *env) undo(args []string) (int, error) {
	if len(args) != 2 {
		return 2, errors.New("usage: claude-atlas undo KB OPERATION")
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
		return 2, errors.New("usage: claude-atlas recover [KB]")
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
		return 2, errors.New("usage: claude-atlas mode [KB] [generic|lyt]")
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
		return 2, errors.New("usage: claude-atlas apply KB PLAN.json")
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
	dir := os.Getenv("CLAUDE_PROJECT_DIR")
	if dir == "" {
		dir = cwd()
	}
	err := mcpserver.Run(context.Background(), mcpserver.Options{
		Version: Version, PluginRoot: os.Getenv("CLAUDE_PLUGIN_ROOT"), ProjectDir: dir,
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
		return 0, hooks.SessionStart(e.stdin, e.stdout, os.Getenv, enabled, time.Now())
	case "guard":
		return 0, hooks.Guard(e.stdin, e.stdout)
	case "stop":
		return 0, hooks.Stop(e.stdin, e.stdout, os.Getenv)
	}
	return 2, fmt.Errorf("unknown hook %q", args[0])
}

// config shows the settings, or sets one and refreshes so the registry follows.
func (e *env) config(args []string) (int, error) {
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	c := e.console
	if len(args) == 0 {
		row := func(label, value string) { c.Say("  %-18s %s", label, value) }
		row("new-days", fmt.Sprintf("%d  (an entry is new for this many days after its creation; 0 turns it off)", cfg.NewDays()))
		row("vaults dir", home.Display(cfg.VaultsDir)+"  (where knowledge bases live; `claude-atlas relocate PATH` moves them)")
		row("projects", fmt.Sprintf("%d registered", len(cfg.Projects)))
		row("claude command", cfg.ClaudeCode.Command)
		row("plugin source", cfg.Plugin.Source)
		row("file", home.Display(e.home.ConfigPath()))
		return 0, nil
	}
	if len(args) != 2 {
		return 2, errors.New("usage: claude-atlas config [KEY VALUE]; keys: new-days")
	}
	switch args[0] {
	case "new-days":
		days, err := strconv.Atoi(args[1])
		if err != nil {
			return 2, fmt.Errorf("new-days takes a number of days, got %q", args[1])
		}
		if err := cfg.SetNewDays(days); err != nil {
			return 2, err
		}
	default:
		return 2, fmt.Errorf("unknown setting %q; keys: new-days", args[0])
	}
	if err := e.home.Save(cfg); err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	c.Step(console.OK, args[0], args[1])
	c.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
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
	row("registry", home.Display(registry.File(e.home.StateDir())))
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
	ix, err := registry.Scan(cfg)
	if err != nil {
		return 1, err
	}
	row("entries", fmt.Sprintf("%d found", len(ix.Entries)))
	for _, en := range ix.Entries {
		row("  "+entryName(en), home.Display(en.Path))
	}
	for _, p := range uncoveredProblems(ix) {
		row("  "+filepath.Base(p.Path), home.Display(p.Path)+": "+p.Reason)
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
		line("git", "missing; knowledge base operations need it")
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
	line("vaults dir", home.Display(cfg.VaultsDir))
	ix, err := registry.Scan(cfg)
	if err != nil {
		return 1, err
	}
	line("entries", fmt.Sprintf("%d found", len(ix.Entries)))
	for _, en := range ix.Entries {
		status := entryStatus(en)
		if status != "ok" {
			ok = false
		}
		c.Say("    %-7s %-10s %-24s %s", status, listKind(en), entryName(en), home.Display(en.Path))
	}
	for _, en := range ix.Projects() {
		if en.Knowledge != nil && en.Knowledge.Error != "" {
			ok = false
			c.Step(console.Fail, en.Name+" · knowledge", en.Knowledge.Error+"; run `claude-atlas link KB --project "+en.Name+"`")
		}
	}
	for _, p := range uncoveredProblems(ix) {
		ok = false
		c.Step(console.Fail, filepath.Base(p.Path), home.Display(p.Path)+": "+p.Reason)
	}
	if !ok {
		return 1, nil
	}
	return 0, nil
}

// entryStatus is doctor's one word for an entry: what stands between it and working.
func entryStatus(en registry.Entry) string {
	if en.Error != "" {
		return unreadable(en)
	}
	if en.Kind == registry.Project {
		if _, err := project.Open(en.Path); err != nil {
			return "bad"
		}
		return "ok"
	}
	v, err := vault.Open(en.Path)
	if err != nil {
		return "bad"
	}
	if pending, _ := txn.Pending(v); pending != nil {
		return "recover"
	}
	if !v.Repo().IsRepo() {
		return "no git"
	}
	return "ok"
}
