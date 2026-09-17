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
	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/hooks"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/lint"
	"github.com/nathanaday/claude-atlas/internal/mcpserver"
	"github.com/nathanaday/claude-atlas/internal/obsidian"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/repomap"
	"github.com/nathanaday/claude-atlas/internal/tasks"
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
  claude-atlas                       open the view: every vault on one screen
  claude-atlas [--home DIR] [-y] <command> [options]

Vaults:
  setup                     install the plugin and create your first project
  new-project NAME|PATH     create a project: tasks, questions, notes, repositories
                            --in REPO puts it inside a code repository, at REPO/atlas
  new-knowledge NAME|PATH   create a knowledge base: sources, entities, concepts
  new-cluster NAME|PATH     create a knowledge base meant to gather others; add members with cluster add
  adopt [PATH]              make an existing Obsidian or claude-obsidian vault one of these
  open-vault [NAME|PATH]    open a vault in Obsidian; with no name, the one you are in
  open-claude NAME          start Claude Code inside a vault; --task ID continues a task in its workdir
  ingest NAME [PATH...]     stage new files from outside a project into its inbox, then ingest them

Across the vaults:
  view                      the interactive screen; the same as no command at all
  list                      every vault: kind, heat, name, path
  show NAME                 everything the atlas knows about one vault
  edit NAME [flags]         change a vault's name, tags, scope, or access
  remove NAME               forget a vault outside the vaults directory; the folder stays
  refresh                   read every vault again and rewrite the registry

Knowledge bases a project reaches (a mount is a folder kb/NAME inside the project):
  mount PROJECT KB          mount a knowledge base in a project; --read mounts it read-only, --as NAME renames it
  unmount PROJECT KB|NAME   drop that mount; the knowledge base stays
  grant KB PROJECT          let one project into a guarded knowledge base: --read or --write
  revoke KB PROJECT|ID      take that grant back, by project or by a stale grant's id

Clusters (a knowledge base that gathers others; mounting it reaches every member):
  cluster NAME              the cluster's members, one per line
  cluster add NAME KB       add a knowledge base to the cluster
  cluster remove NAME KB|ID drop that member; the knowledge base stays

Repositories (a project's deliverables; memory stays in the vault):
  link NAME PATH|URL        link a repository to a project, or clone one from a URL; --init makes a plain folder one first
  new-repo NAME REPO        create a repository for a project and link it; --at DIR places it
  unlink NAME REPO          drop that repository from the project; the folder stays
  repos [NAME]              one project's repositories, or every project's
  edit-repo NAME REPO       set a repository's remote, folder, or how changes land: --changes pr|commit

Tasks (VAULT is a vault name or a path; default: the current directory):
  plant VAULT TEXT...       plant a task: a page with status planted, from your words
  tasks [VAULT]             list open tasks; with no vault and outside one, every project's

Inside a vault (VAULT is a vault name or a path; default: the current directory):
  lint [VAULT]              run the wiki health check
  stub VAULT [TITLE...]     create seed pages for the pages your links name but nobody has written
  history [VAULT]           list operations, newest first
  undo VAULT OPERATION      revert one operation
  recover [VAULT]           restore a vault after an interrupted operation
  mode [VAULT] [MODE]       show or set the filing mode: generic or lyt
  upgrade [VAULT|--all]     add the files a vault made by an older version lacks
  apply VAULT PLAN.json     apply a plan file, for scripts

Plugin:
  mcp                       serve the atlas tools over stdio; Claude Code runs this
  hook EVENT                run a plugin hook: session-start, guard, stop

  config [KEY VALUE]        show the settings, or set one: new-days N, repo-changes pr|commit
  info                      show every path and version the atlas uses
  doctor                    check the installation and every vault
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
		// Bare, in a terminal, with an atlas: the view. Otherwise the usage, with the
		// one step that is missing.
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
	case "new-project":
		code, err = e.newProject(rest[1:])
	case "new-knowledge":
		code, err = e.newKnowledge(rest[1:])
	case "new-cluster":
		code, err = e.newCluster(rest[1:])
	case "cluster":
		code, err = e.cluster(rest[1:])
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
	case "mount":
		code, err = e.mount(rest[1:])
	case "unmount":
		code, err = e.unmount(rest[1:])
	case "grant":
		code, err = e.grant(rest[1:])
	case "revoke":
		code, err = e.revoke(rest[1:])
	case "link":
		code, err = e.link(rest[1:])
	case "new-repo":
		code, err = e.newRepo(rest[1:])
	case "unlink":
		code, err = e.unlink(rest[1:])
	case "repos":
		code, err = e.repos(rest[1:])
	case "edit-repo":
		code, err = e.editRepo(rest[1:])
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
	case "plant":
		code, err = e.plant(rest[1:])
	case "tasks":
		code, err = e.tasks(rest[1:])
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

// entry resolves one vault by name, id, or path. Every command scans afresh: the registry
// file is derived state, and a stale one must never decide what a command acts on.
func (e *env) entry(cfg *home.Config, arg string) (registry.Entry, error) {
	ix, err := registry.Scan(cfg)
	if err != nil {
		return registry.Entry{}, err
	}
	return findEntry(ix, arg)
}

// anyEntry resolves one vault, including one the atlas could not read. Only remove acts
// on those; every other command wants entry.
func (e *env) anyEntry(cfg *home.Config, arg string) (registry.Entry, error) {
	ix, err := registry.Scan(cfg)
	if err != nil {
		return registry.Entry{}, err
	}
	return findAnyEntry(ix, arg)
}

// findEntry is entry over an index the caller already has. A vault the scan could not
// read carries its own reason, which says more than "no vault named".
func findEntry(ix *registry.Index, arg string) (registry.Entry, error) {
	en, err := findAnyEntry(ix, arg)
	if err != nil {
		return registry.Entry{}, err
	}
	if en.Error != "" {
		return registry.Entry{}, fmt.Errorf("%s: %s", home.Display(en.Path), en.Error)
	}
	return en, nil
}

// findAnyEntry resolves a vault by name, id, or path, and a vault the atlas could not read
// by its path or its folder's name.
func findAnyEntry(ix *registry.Index, arg string) (registry.Entry, error) {
	found, err := ix.Find(arg)
	if err == nil {
		return *found, nil
	}
	if !errors.Is(err, registry.ErrNotFound) {
		return registry.Entry{}, err
	}
	if bad := badEntry(ix, arg); bad != nil {
		return *bad, nil
	}
	return registry.Entry{}, fmt.Errorf("no vault named %q; see `claude-atlas list`", arg)
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

// badEntry finds a vault the scan could not read, by its path or its folder's name. Such
// an entry has no name of its own.
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

// refreshAll rebuilds the registry from a scan and recreates every project's mount
// symlinks. Callers report the count themselves; only `refresh` reports the symlinks.
func (e *env) refreshAll(cfg *home.Config) ([]registry.Entry, *registry.Index, error) {
	entries, ix, _, err := refresh.All(e.home, cfg, time.Now())
	return entries, ix, err
}

// registryEntries reads the registry the last refresh wrote, writing one first when no
// refresh has run yet.
func (e *env) registryEntries(cfg *home.Config) ([]registry.Entry, error) {
	return refresh.Entries(e.home, cfg, time.Now())
}

// refreshed is what a command says after rewriting the registry.
func refreshed(entries []registry.Entry) string {
	return fmt.Sprintf("%d vault%s", len(entries), plural(len(entries)))
}

// entryName is the name to show: a vault the scan could not read has only its folder.
func entryName(en registry.Entry) string {
	if en.Error != "" {
		return filepath.Base(en.Path)
	}
	return en.Name
}

func (e *env) setup(args []string) (int, error) {
	fs := newFlags("setup", e.stderr)
	vaultsDir := fs.String("vaults-dir", "", "where new vaults are created (default ~/Documents/Vaults)")
	first := fs.String("first-vault", "", "name or path of the first project (default welcome)")
	source := fs.String("plugin-source", "", "install the plugin from this marketplace source, e.g. a local checkout")
	noPlugin := fs.Bool("no-plugin", false, "do not run `claude plugin`")
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}
	opts := wizard.Options{Version: Version, VaultsDir: *vaultsDir, FirstVault: *first, PluginSource: *source, WithPlugin: !*noPlugin}
	return wizard.Run(e.home, e.console, opts)
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

// parseKind reads a --as value; empty means the caller's default.
func parseKind(s string, fallback vault.Kind) (vault.Kind, error) {
	if s == "" {
		return fallback, nil
	}
	return vault.ParseKind(s)
}

// splitTags reads a comma-separated tag list.
func splitTags(s string) []string {
	var out []string
	for _, tag := range strings.Split(s, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

// checkAccess validates a knowledge base's access value.
func checkAccess(s string) error {
	if s != vault.AccessOpen && s != vault.AccessGuarded {
		return fmt.Errorf("--access must be %s or %s, not %q", vault.AccessOpen, vault.AccessGuarded, s)
	}
	return nil
}

func (e *env) newProject(args []string) (int, error) {
	fs := newFlags("new-project", e.stderr)
	name := fs.String("name", "", "display name (default: the folder's name)")
	tags := fs.String("tags", "", "comma-separated tags, e.g. usc,fall")
	in := fs.String("in", "", "create the project inside this git repository, at REPO/atlas/")
	mode := modeFlag(fs)
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas new-project NAME|PATH [--name N] [--tags a,b] [--mode generic|lyt] [--in REPO]")
	}
	opts := vault.Options{Kind: vault.Project, Name: *name}
	if *mode != "" {
		if opts.Mode, err = vault.ParseMode(*mode); err != nil {
			return 2, err
		}
	}
	var edit vaults.Edit
	if list := splitTags(*tags); len(list) > 0 {
		edit.Tags = &list
	}
	if *in != "" {
		// With --in the folder is REPO/atlas, so the positional is the project's name.
		// With no positional and no --name, the project takes the repository's name.
		if len(positional) == 1 {
			if strings.ContainsAny(positional[0], `/\`) {
				return 2, errors.New("with --in, NAME is the project's name; the folder is REPO/atlas")
			}
			if *name != "" && *name != positional[0] {
				return 2, errors.New("give the name once: as NAME or as --name, not both")
			}
			opts.Name = positional[0]
		}
		return e.createVaultIn(*in, opts, edit)
	}
	if len(positional) == 0 {
		if !e.console.Interactive() {
			return 2, errors.New("usage: claude-atlas new-project NAME|PATH (the interactive screen needs a terminal)")
		}
		return e.newVaultInteractive(opts, edit)
	}
	return e.createVault(positional[0], opts, edit)
}

func (e *env) newKnowledge(args []string) (int, error) {
	return e.createKnowledge("new-knowledge", args)
}

// newCluster creates an ordinary knowledge base. It becomes a cluster when
// `claude-atlas cluster add` gives it its first member.
func (e *env) newCluster(args []string) (int, error) {
	return e.createKnowledge("new-cluster", args)
}

// createKnowledge is the body new-knowledge and new-cluster share: the same flags, the
// same vault, and the command's own name in the usage line.
func (e *env) createKnowledge(command string, args []string) (int, error) {
	fs := newFlags(command, e.stderr)
	name := fs.String("name", "", "display name (default: the folder's name)")
	scope := fs.String("scope", "", "one or two sentences: what this knowledge base covers")
	access := fs.String("access", "", "open (every project may write) or guarded (only the projects it grants); default open")
	mode := modeFlag(fs)
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, fmt.Errorf("usage: claude-atlas %s NAME|PATH [--name N] [--scope TEXT] [--access open|guarded] [--mode generic|lyt]", command)
	}
	opts := vault.Options{Kind: vault.Knowledge, Name: *name}
	if *mode != "" {
		if opts.Mode, err = vault.ParseMode(*mode); err != nil {
			return 2, err
		}
	}
	set := setFlags(fs)
	if set["access"] {
		if err := checkAccess(*access); err != nil {
			return 2, err
		}
	}
	var edit vaults.Edit
	if set["scope"] {
		edit.Scope = scope
	}
	if set["access"] {
		edit.Access = access
	}
	if len(positional) == 0 {
		if !e.console.Interactive() {
			return 2, fmt.Errorf("usage: claude-atlas %s NAME|PATH (the interactive screen needs a terminal)", command)
		}
		return e.newVaultInteractive(opts, edit)
	}
	return e.createVault(positional[0], opts, edit)
}

// createVault makes a vault at arg, records the identity fields the template does not
// carry, registers it when it lies outside the vaults directory, and refreshes.
func (e *env) createVault(arg string, opts vault.Options, edit vaults.Edit) (int, error) {
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	path, err := vaults.ResolvePath(arg, cfg.VaultsDir, opts.Kind)
	if err != nil {
		return 1, err
	}
	if _, err := vaults.Create(path, opts, e.console, true); err != nil {
		return 1, err
	}
	if _, err := vaults.EditIdentity(e.home, cfg, registry.Entry{Path: path, Kind: opts.Kind}, edit, time.Now()); err != nil {
		return 1, err
	}
	return e.finishVault(cfg, path)
}

// createVaultIn makes a project inside the git repository at repoRoot, at REPO/atlas, and
// finishes it the way createVault does.
func (e *env) createVaultIn(repoRoot string, opts vault.Options, edit vaults.Edit) (int, error) {
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	path, err := vaults.CreateIn(repoRoot, opts, e.console)
	if err != nil {
		return 1, err
	}
	if _, err := vaults.EditIdentity(e.home, cfg, registry.Entry{Path: path, Kind: opts.Kind}, edit, time.Now()); err != nil {
		return 1, err
	}
	return e.finishVault(cfg, path)
}

// finishVault registers a vault that was just created or adopted, refreshes, and reports.
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
	c.Say("")
	return 0, nil
}

// newVaultInteractive asks the add screen for a vault of opts.Kind, then creates it. A
// flag the user gave wins over the screen's answer, as it does on adopt.
func (e *env) newVaultInteractive(opts vault.Options, edit vaults.Edit) (int, error) {
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	choice, err := tui.RunAddVault(cfg.VaultsDir, opts.Kind)
	if err != nil {
		return 1, err
	}
	if choice == nil {
		return 1, vaults.ErrCancelled
	}
	opts.Kind = choice.Kind
	if opts.Name == "" {
		opts.Name = choice.Name
	}
	if opts.Mode == "" {
		if opts.Mode, err = parseMode(choice.Mode); err != nil {
			return 1, err
		}
	}
	if edit.Tags == nil && len(choice.Tags) > 0 {
		edit.Tags = &choice.Tags
	}
	if edit.Scope == nil && choice.Scope != "" {
		edit.Scope = &choice.Scope
	}
	if _, err := vaults.Create(choice.Path, opts, e.console, false); err != nil {
		return 1, err
	}
	if _, err := vaults.EditIdentity(e.home, cfg, registry.Entry{Path: choice.Path, Kind: opts.Kind}, edit, time.Now()); err != nil {
		return 1, err
	}
	return e.finishVault(cfg, choice.Path)
}

func (e *env) adopt(args []string) (int, error) {
	fs := newFlags("adopt", e.stderr)
	name := fs.String("name", "", "display name (default: the folder's name)")
	mode := fs.String("mode", "", "filing mode when the vault has none: generic (default) or lyt")
	as := fs.String("as", "", "adopt as a project or a knowledge base; a vault that already has a kind keeps it; default project")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas adopt [PATH] [--as project|knowledge] [--name N] [--mode generic|lyt]")
	}
	var m vault.Mode
	if *mode != "" {
		if m, err = vault.ParseMode(*mode); err != nil {
			return 2, err
		}
	}
	k, err := parseKind(*as, "")
	if err != nil {
		return 2, err
	}
	path := first(positional)
	if path == "" {
		if !e.console.Interactive() {
			return 2, errors.New("usage: claude-atlas adopt PATH (the interactive screen needs a terminal)")
		}
		choice, cerr := tui.RunAdopt()
		if cerr != nil {
			return 1, cerr
		}
		if choice == nil {
			return 1, vaults.ErrCancelled
		}
		// A flag the user gave wins over the screen's answer.
		if k == "" {
			k = choice.Kind
		}
		if m == "" {
			if m, err = vault.ParseMode(choice.Mode); err != nil {
				return 1, err
			}
		}
		if *name == "" {
			*name = choice.Name
		}
		path = choice.Path
	}
	return e.adoptPath(path, vault.Options{Kind: k, Mode: m, Name: *name})
}

// adoptPath makes a directory a claude-atlas vault, registers it, and refreshes.
func (e *env) adoptPath(path string, vopts vault.Options) (int, error) {
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	abs, err := filepath.Abs(home.Expand(path))
	if err != nil {
		return 1, err
	}
	res, err := vault.Adopt(abs, vopts, time.Now())
	if err != nil {
		return 1, err
	}
	c := e.console
	switch {
	case res.AlreadyAdopted && res.Commit == "":
		c.Step(console.Skip, "adopt", "already a claude-atlas "+res.Kind.Noun())
	case res.WasLegacy:
		c.Step(console.OK, "adopted", fmt.Sprintf("claude-obsidian vault as %s; %s", res.Kind.Noun(), setupChanges(res.Added, res.Moved)))
	case res.FromV1:
		c.Step(console.OK, "adopted", fmt.Sprintf("v1 vault as %s; %s", res.Kind.Noun(), setupChanges(res.Added, res.Moved)))
	default:
		c.Step(console.OK, "adopted", fmt.Sprintf("as %s; %s", res.Kind.Noun(), setupChanges(res.Added, res.Moved)))
	}
	if host := vault.HostRepo(res.Root); host != "" {
		c.Step(console.OK, "inside", fmt.Sprintf("the repository %s; its git holds the vault's history", home.Display(host)))
	}
	if len(res.Removed) > 0 {
		c.Step(console.OK, "removed", strings.Join(res.Removed, ", ")+" (a knowledge base has none)")
	}
	if res.GitInitialized {
		c.Step(console.OK, "git", "initialized; every operation is now one commit")
	}
	if res.Commit != "" {
		c.Step(console.OK, "committed", res.Commit[:12]+" baseline")
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

func (e *env) view(args []string) (int, error) {
	if !e.console.Interactive() {
		return 2, errors.New("view is an interactive screen and needs a terminal")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	hooks := actions.Bind(e.home, cfg, e.console)
	entries, err := hooks.Load()
	if err != nil {
		return 1, err
	}
	opener := tui.Opener{
		Status:          obsidian.Status,
		Open:            obsidian.Open,
		RegisterAndOpen: obsidian.RegisterAndOpen,
		ClaudeIn: func(vault, dir, prompt string) (*exec.Cmd, error) {
			return claudecode.LaunchIn(cfg.ClaudeCode, vault, dir, prompt)
		},
		OpenPath: obsidian.OpenPath,
		Claude: func(vault, prompt string) (*exec.Cmd, error) {
			return claudecode.LaunchCommand(cfg.ClaudeCode, vault, prompt)
		},
	}
	changed, err := tui.RunView(tui.Items(entries), opener, hooks)
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

// resolveVault turns an open-vault argument into a directory and a label: nothing means
// the vault at or above the current directory, a name or an id means its vault, and
// anything else is taken as a path.
func resolveVault(ix *registry.Index, arg string) (string, string, error) {
	if arg == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", "", err
		}
		root := vault.FindAbove(cwd)
		if root == "" {
			return "", "", fmt.Errorf("%w: the current directory is not inside a vault; name one or give a path", vault.ErrNotVault)
		}
		return root, filepath.Base(root), nil
	}
	if found, err := ix.Find(arg); err == nil {
		return found.Path, found.Name, nil
	} else if errors.Is(err, registry.ErrAmbiguous) {
		return "", "", err
	}
	path, err := filepath.Abs(home.Expand(arg))
	if err != nil {
		return "", "", err
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path, filepath.Base(path), nil
	}
	return "", "", fmt.Errorf("%q is neither a vault nor a directory", arg)
}

// openVaultArg resolves a vault for the in-vault commands: a vault name, a path, or the
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
		return nil, fmt.Errorf("%w: the current directory is not inside a vault; name a vault or a path", vault.ErrNotVault)
	}
	if cfg, err := e.home.Load(); err == nil {
		if ix, err := registry.Scan(cfg); err == nil {
			if found, ferr := ix.Find(arg); ferr == nil {
				return vault.Open(found.Path)
			} else if errors.Is(ferr, registry.ErrAmbiguous) {
				return nil, ferr
			}
			// A vault the scan could not read still has a path; Open says why.
			if bad := badEntry(ix, arg); bad != nil {
				return vault.Open(bad.Path)
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
	ix, err := registry.Scan(cfg)
	if err != nil {
		return 1, err
	}
	root, label, err := resolveVault(ix, first(positional))
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

// skillHint is printed before handing the terminal to Claude Code. A knowledge base takes
// no sources and has no tasks, so it lists fewer skills.
func skillHint(kind vault.Kind) string {
	if kind == vault.Knowledge {
		return "skills: " + hooks.KnowledgeSkills
	}
	return "skills: " + hooks.Skills
}

// trustNote explains Claude Code's own first-run dialog, whose default answer quits.
const trustNote = "The first time in a vault, Claude Code asks whether you trust the folder; choose Yes."

func (e *env) openClaude(args []string) (int, error) {
	fs := newFlags("open-claude", e.stderr)
	taskID := fs.String("task", "", "continue this task: start in its workdir with /claude-atlas:task-run as the first message")
	in := fs.String("in", "", "start the session in this folder instead of the vault")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) != 1 {
		return 2, errors.New("usage: claude-atlas open-claude NAME [--task ID] [--in DIR]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.entry(cfg, positional[0])
	if err != nil {
		return 1, err
	}
	if !e.console.Interactive() {
		return 2, errors.New("open-claude starts an interactive Claude Code session and needs a terminal")
	}
	dir, prompt := home.Expand(*in), ""
	if *taskID != "" {
		rec, err := e.findTask(entry, *taskID)
		if err != nil {
			return 1, err
		}
		prompt = claudecode.TaskPrompt(rec.ID)
		if dir == "" {
			dir = rec.Workdir
		}
		e.console.Say("  task: %s (%s)", rec.Title, rec.Status)
	}
	cmd, err := claudecode.LaunchIn(cfg.ClaudeCode, entry.Path, dir, prompt)
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

func (e *env) ingest(args []string) (int, error) {
	fs := newFlags("ingest", e.stderr)
	dryRun := fs.Bool("dry-run", false, "show what would be staged and stop")
	noClaude := fs.Bool("no-claude", false, "stage the files but do not start Claude Code")
	repo := fs.String("repo", "", "stage a snapshot of one of the project's repositories instead of files")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) == 0 || (*repo != "" && (len(positional) > 1 || *dryRun)) {
		return 2, errors.New("usage: claude-atlas ingest NAME [PATH ...] [--dry-run] [--no-claude], or claude-atlas ingest NAME --repo REPO [--no-claude]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.entry(cfg, positional[0])
	if err != nil {
		return 1, err
	}
	if entry.Kind != vault.Project {
		return 1, fmt.Errorf("%s is a knowledge base; knowledge enters through a project that mounts it", entry.Name)
	}
	v, err := vault.Open(entry.Path)
	if err != nil {
		return 1, err
	}
	c := e.console
	if *repo != "" {
		snap, err := actions.Bind(e.home, cfg, e.console).StageRepo(entry, *repo)
		if err != nil {
			return 1, err
		}
		if snap.Described != nil {
			c.Say("  %-10s %s", "page", snap.Described.Summary())
		}
		switch {
		case snap.New:
			c.Step(console.OK, "staged", fmt.Sprintf("%s: %s at %s", strings.TrimPrefix(snap.To, "inbox/"), snap.Repo.Name, snap.Commit[:7]))
		default:
			c.Say("  %-10s %s already waits or was captured", "unchanged", strings.TrimPrefix(snap.To, "inbox/"))
		}
		return e.offerIngest(cfg, entry, *noClaude, "/claude-atlas:repo-map", claudecode.RepoMapPrompt)
	}
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
	return e.offerIngest(cfg, entry, *noClaude, "/claude-atlas:wiki-ingest", claudecode.IngestPrompt)
}

// offerIngest ends ingest: it names the skill to run next, and in a terminal offers to
// start Claude Code on it.
func (e *env) offerIngest(cfg *home.Config, entry registry.Entry, noClaude bool, skill, prompt string) (int, error) {
	c := e.console
	if noClaude || !c.Interactive() {
		c.Say("  Next: claude-atlas open-claude %s, then %s", entry.Name, skill)
		return 0, nil
	}
	c.Say("  %s", trustNote)
	ok, err := c.Confirm("Start Claude Code now and run "+skill+"?", true)
	if err != nil {
		return 1, err
	}
	if !ok {
		c.Say("  Next: claude-atlas open-claude %s, then %s", entry.Name, skill)
		return 0, nil
	}
	cmd, err := claudecode.LaunchCommand(cfg.ClaudeCode, entry.Path, prompt)
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
		e.console.Say("no vaults yet; create one with `claude-atlas new-project NAME`")
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

// listKind is the kind column: a vault the scan could not read has no kind, and a
// knowledge base that gathers others reads as a cluster.
func listKind(en registry.Entry) string {
	if en.Error != "" {
		return "?"
	}
	if vaults.IsCluster(en) {
		return "cluster"
	}
	return string(en.Kind)
}

// unreadable is the one word for a vault the atlas knows but could not read.
func unreadable(en registry.Entry) string {
	switch en.Reason {
	case registry.ReasonV1:
		return "v1"
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
	entry, err := findEntry(ix, args[0])
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
	row("Kind", string(entry.Kind))
	row("Id", entry.ID)
	row("Path", home.Display(entry.Path))
	row("Mode", string(entry.Mode))
	row("Created", entry.Created)
	if entry.Kind == vault.Knowledge {
		row("Scope", entry.Scope)
		row("Access", entry.Access)
		for _, g := range entry.Grants {
			val := g.Name + "  " + g.Access
			if g.Error != "" {
				val += "  " + g.Error
			}
			row("Grant", val)
		}
		for _, m := range entry.Members {
			row("Member", memberLine(ix, m))
		}
		for _, cl := range entry.Clusters {
			row("Cluster", cl.Name)
		}
		for _, m := range entry.MountedBy {
			row("Mounted by", m.Name+"  "+m.Access)
		}
	} else {
		row("Tags", strings.Join(entry.Tags, ", "))
	}
	for _, m := range entry.Mounts {
		row("Mount", mountLine(entry, m))
	}
	for _, r := range entry.Repos {
		row("Repo", repoLine(entry, r))
	}
	state := entry.State
	if state == nil {
		row("Refreshed", "never; run `claude-atlas refresh`")
		return 0, nil
	}
	if state.VaultOK {
		row("Vault check", "ok")
	} else {
		row("Vault check", state.VaultError)
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
	row("Last operation", state.LastOperation)
	if state.Pages != nil {
		row("Pages", fmt.Sprint(*state.Pages))
	}
	row("Unfinished", state.Unfinished.Text())
	for i, t := range state.OpenThreads {
		label := "Open threads"
		if i > 0 {
			label = ""
		}
		c.Say("  %-16s - %s", label, refresh.PlainText(t))
	}
	if state.Tasks != nil {
		row("Tasks", taskCounts(state.Tasks.Counts))
	}
	row("Refreshed", state.GeneratedAt)
	for _, signal := range refresh.Signals(entry, time.Now()) {
		row("Signal", signal)
	}
	return 0, nil
}

// taskCounts is one line of a project's task ledger.
func taskCounts(c tasks.Counts) string {
	line := fmt.Sprintf("%d open (active %d, blocked %d, planned %d, planted %d)", c.Open, c.Active, c.Blocked, c.Planned, c.Planted)
	if c.Notes > 0 {
		line += fmt.Sprintf(" · %d note%s waiting", c.Notes, plural(c.Notes))
	}
	return line
}

// mountLine renders one mount: its name, the folder the project reaches it through, the
// access both vaults agree on, and what the symlink is.
func mountLine(project registry.Entry, m registry.Mount) string {
	if m.Error != "" {
		return fmt.Sprintf("%-20s %s", m.Name, m.Error)
	}
	line := fmt.Sprintf("%-20s %-20s %-6s %s", m.Name, kbPath(m.Name), m.Effective, vaults.MountState(project, m))
	if m.Through != "" {
		line += "  through " + m.Through
	}
	return line
}

// kbPath is the folder a project reaches a mount through, as the user sees it.
func kbPath(name string) string { return vault.KbDir + "/" + name }

// repoLine renders one repository: its name, its folder, how changes land, its remote,
// and, for the repository the project lives in, that it is the host.
func repoLine(e registry.Entry, r registry.Repo) string {
	where := home.Display(r.Path)
	if r.Path == "" {
		where = r.Error
	}
	line := fmt.Sprintf("%-20s %s  changes: %s", r.Name, where, links.Policy(r.Changes, r.Remote))
	if r.Remote != "" {
		line += " · remote " + r.Remote
	}
	if e.Host != "" && r.Path == e.Host {
		line += "  (this project lives in it)"
	}
	if r.Path != "" {
		if d := repomap.Describe(e, r); d != nil {
			line += " · " + d.Summary()
		} else {
			line += " · " + registry.NotDescribed
		}
	}
	return line
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
	tags := fs.String("tags", "", "a project's tags, comma-separated; \"\" clears them")
	scope := fs.String("scope", "", "what a knowledge base covers; \"\" clears it")
	access := fs.String("access", "", "a knowledge base's access: open or guarded")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	set := setFlags(fs)
	if len(positional) != 1 || len(set) == 0 {
		return 2, errors.New("usage: claude-atlas edit NAME [--name N] [--tags a,b] [--scope TEXT] [--access open|guarded]")
	}
	if set["access"] {
		if err := checkAccess(*access); err != nil {
			return 2, err
		}
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.entry(cfg, positional[0])
	if err != nil {
		return 1, err
	}
	change := vaults.Edit{Name: *name}
	if set["tags"] {
		list := splitTags(*tags)
		change.Tags = &list
	}
	if set["scope"] {
		change.Scope = scope
	}
	if set["access"] {
		change.Access = access
	}
	path, err := vaults.EditIdentity(e.home, cfg, entry, change, time.Now())
	if err != nil {
		return 1, err
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
		return 2, errors.New("usage: claude-atlas remove NAME")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.anyEntry(cfg, args[0])
	if err != nil {
		return 1, err
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

func (e *env) mount(args []string) (int, error) {
	fs := newFlags("mount", e.stderr)
	read := fs.Bool("read", false, "the project may read the knowledge base but not write to it")
	as := fs.String("as", "", "the name the project reaches it under (default: the knowledge base's name)")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) != 2 {
		return 2, errors.New("usage: claude-atlas mount PROJECT KB [--read] [--as NAME]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	project, err := e.entry(cfg, positional[0])
	if err != nil {
		return 1, err
	}
	kb, err := e.entry(cfg, positional[1])
	if err != nil {
		return 1, err
	}
	access := vault.AccessWrite
	if *read {
		access = vault.AccessRead
	}
	m, err := vaults.Mount(project, kb, access, *as, time.Now())
	if err != nil {
		if m.ID != "" {
			return 1, fmt.Errorf("%w; the mount is recorded, so `claude-atlas refresh` can make kb/%s", err, m.Name)
		}
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	effective := registry.Effective(m.Access, registry.GrantedAccess(kb, project.ID))
	e.console.Step(console.OK, "mounted", fmt.Sprintf("%s as kb/%s (effective %s)", kb.Name, m.Name, effective))
	if effective != m.Access {
		e.console.Step(console.Skip, "access", fmt.Sprintf("%s is guarded; run `claude-atlas grant %s %s --%s` to open it", kb.Name, kb.Name, project.Name, m.Access))
	}
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) unmount(args []string) (int, error) {
	if len(args) != 2 {
		return 2, errors.New("usage: claude-atlas unmount PROJECT KB|NAME")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	project, err := e.entry(cfg, args[0])
	if err != nil {
		return 1, err
	}
	name := args[1]
	if found := vaults.FindMount(project, name); found != nil {
		name = found.Name
	}
	if err := vaults.Unmount(project, args[1], time.Now()); err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "unmounted", fmt.Sprintf("%s from %s; the knowledge base is untouched", name, project.Name))
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) grant(args []string) (int, error) {
	fs := newFlags("grant", e.stderr)
	read := fs.Bool("read", false, "the project may read the knowledge base")
	write := fs.Bool("write", false, "the project may write pages there")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) != 2 || *read == *write {
		return 2, errors.New("usage: claude-atlas grant KB PROJECT --read|--write")
	}
	access := vault.AccessRead
	if *write {
		access = vault.AccessWrite
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	kb, err := e.entry(cfg, positional[0])
	if err != nil {
		return 1, err
	}
	project, err := e.entry(cfg, positional[1])
	if err != nil {
		return 1, err
	}
	if err := vaults.Grant(kb, project, access, time.Now()); err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "granted", fmt.Sprintf("%s %s on %s", project.Name, access, kb.Name))
	if kb.Access == vault.AccessOpen {
		e.console.Step(console.Skip, "access", fmt.Sprintf("%s is open, so every project already writes there", kb.Name))
	}
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) revoke(args []string) (int, error) {
	if len(args) != 2 {
		return 2, errors.New("usage: claude-atlas revoke KB PROJECT|ID")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	kb, err := e.entry(cfg, args[0])
	if err != nil {
		return 1, err
	}
	label := args[1]
	byID := false
	for _, g := range kb.Grants {
		if g.ID == args[1] {
			byID = true
			break
		}
	}
	if byID {
		if err := vaults.RevokeID(kb, args[1], time.Now()); err != nil {
			return 1, err
		}
	} else {
		project, err := e.entry(cfg, args[1])
		if err != nil {
			return 1, err
		}
		if err := vaults.Revoke(kb, project, time.Now()); err != nil {
			return 1, err
		}
		label = project.Name
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "revoked", fmt.Sprintf("%s on %s", label, kb.Name))
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

const clusterUsage = `usage: claude-atlas cluster NAME
       claude-atlas cluster add NAME KB
       claude-atlas cluster remove NAME KB|ID`

// cluster lists a knowledge base's members, adds one, or drops one.
func (e *env) cluster(args []string) (int, error) {
	if len(args) == 0 {
		return 2, errors.New(clusterUsage)
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	switch args[0] {
	case "add":
		return e.clusterAdd(cfg, args[1:])
	case "remove":
		return e.clusterRemove(cfg, args[1:])
	}
	if len(args) != 1 {
		return 2, errors.New(clusterUsage)
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		return 1, err
	}
	entry, err := findEntry(ix, args[0])
	if err != nil {
		return 1, err
	}
	if entry.Kind != vault.Knowledge {
		return 1, fmt.Errorf("%s is a project; a cluster is a knowledge base that gathers others", entry.Name)
	}
	if len(entry.Members) == 0 {
		e.console.Say("%s holds no members; `claude-atlas cluster add %s KB` makes it a cluster", entry.Name, entry.Name)
		return 0, nil
	}
	for _, m := range entry.Members {
		e.console.Say("  %s", memberLine(ix, m))
	}
	return 0, nil
}

func (e *env) clusterAdd(cfg *home.Config, args []string) (int, error) {
	if len(args) != 2 {
		return 2, errors.New(clusterUsage)
	}
	cluster, err := e.entry(cfg, args[0])
	if err != nil {
		return 1, err
	}
	kb, err := e.entry(cfg, args[1])
	if err != nil {
		return 1, err
	}
	if err := vaults.AddMember(cluster, kb, time.Now()); err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "added", fmt.Sprintf("%s to %s; every project that mounts %s reaches it", kb.Name, cluster.Name, cluster.Name))
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) clusterRemove(cfg *home.Config, args []string) (int, error) {
	if len(args) != 2 {
		return 2, errors.New(clusterUsage)
	}
	cluster, err := e.entry(cfg, args[0])
	if err != nil {
		return 1, err
	}
	// The member comes first, by name or id, so one the scan lost still drops. Only when
	// the cluster holds no such member does the argument name a vault.
	var target, label string
	if member := findMember(cluster, args[1]); member != nil {
		target, label = member.ID, member.Name
	} else {
		kb, err := e.entry(cfg, args[1])
		if err != nil {
			return 1, err
		}
		if findMember(cluster, kb.ID) == nil {
			return 1, fmt.Errorf("%s holds no member named %q", cluster.Name, kb.Name)
		}
		target, label = kb.ID, kb.Name
	}
	if err := vaults.RemoveMember(cluster, target, time.Now()); err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "dropped", fmt.Sprintf("%s from %s; the knowledge base is untouched", label, cluster.Name))
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

// findMember finds a cluster's member by id or by name; nil when there is none.
func findMember(cluster registry.Entry, target string) *registry.Ref {
	for i := range cluster.Members {
		m := cluster.Members[i]
		if m.ID == target || strings.EqualFold(m.Name, target) {
			return &m
		}
	}
	return nil
}

// memberLine renders one member: its name and what it covers, or why the scan lost it.
func memberLine(ix *registry.Index, m registry.Ref) string {
	if m.Error != "" {
		return fmt.Sprintf("%-20s %s", m.Name, m.Error)
	}
	scope := ""
	if kb := ix.ByID(m.ID); kb != nil {
		scope = kb.Scope
	}
	return strings.TrimRight(fmt.Sprintf("%-20s %s", m.Name, scope), " ")
}

// checkPolicy validates a --changes value.
func checkPolicy(s string) error {
	if s != links.ChangesPR && s != links.ChangesCommit {
		return errors.New("--changes must be pr or commit")
	}
	return nil
}

func (e *env) link(args []string) (int, error) {
	fs := newFlags("link", e.stderr)
	initGit := fs.Bool("init", false, "make a plain folder a git repository first, with one commit of what it holds")
	at := fs.String("at", "", "where the repository goes (default: repos/ inside the project)")
	changes := fs.String("changes", "", "how claude-atlas lands changes there: pr or commit (asked when the repository has a remote)")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) != 2 {
		return 2, errors.New("usage: claude-atlas link NAME PATH|URL [--init] [--at DIR] [--changes pr|commit]")
	}
	if *changes != "" {
		if err := checkPolicy(*changes); err != nil {
			return 2, err
		}
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.entry(cfg, positional[0])
	if err != nil {
		return 1, err
	}
	now := time.Now()
	target := positional[1]
	var repo vault.Repo
	var path string
	if links.IsRemoteURL(target) {
		e.console.Say("  cloning %s …", target)
		repo, path, err = vaults.CloneRepo(e.home, cfg, entry, target, *at, now)
	} else {
		repo, path, err = vaults.AddRepo(e.home, cfg, entry, target, *initGit, now)
		var notRepo *vaults.NotRepoError
		if errors.As(err, &notRepo) && e.console.Interactive() {
			ok, cerr := e.console.Confirm(fmt.Sprintf("%s is not a git repository. Initialize one there, with one commit of what it holds?", home.Display(notRepo.Path)), true)
			if cerr != nil {
				return 1, cerr
			}
			if !ok {
				return 1, vaults.ErrCancelled
			}
			repo, path, err = vaults.AddRepo(e.home, cfg, entry, target, true, now)
		}
	}
	if err != nil {
		return 1, err
	}
	// The entry was read before the repository was added; settleChanges edits it by name.
	entry.Repos = append(entry.Repos, registry.Repo{Name: repo.Name, Path: path, Remote: repo.Remote, Changes: repo.Changes})
	if repo, err = e.settleChanges(cfg, entry, repo, *changes); err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	policy := links.Policy(repo.Changes, repo.Remote)
	e.console.Step(console.OK, "linked", fmt.Sprintf("%s (%s) → %s", repo.Name, home.Display(path), entry.Name))
	if repo.Remote != "" {
		e.console.Step(console.OK, "remote", repo.Remote)
	}
	e.console.Step(console.OK, "changes", policy+"  "+links.PolicyText(policy))
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

// settleChanges records how changes land in a repository: the flag, else the user's
// answer when the repository has a remote and nothing is recorded yet, else the default.
// A repository with no remote lands commits; there is nothing to ask.
func (e *env) settleChanges(cfg *home.Config, entry registry.Entry, repo vault.Repo, flag string) (vault.Repo, error) {
	policy := flag
	switch {
	case flag != "":
	case repo.Changes != "" || repo.Remote == "" || !e.console.Interactive():
		return repo, nil
	default:
		e.console.Say("  %s has a remote, %s. How should claude-atlas land its changes there?", repo.Name, repo.Remote)
		e.console.Say("    pr      work on a branch and open a pull request; never push to the default branch")
		e.console.Say("    commit  commit on the current branch")
		pr, err := e.console.Confirm("Pull requests?", true)
		if err != nil {
			return repo, err
		}
		policy = links.ChangesCommit
		if pr {
			policy = links.ChangesPR
		}
	}
	return vaults.EditRepo(e.home, cfg, entry, repo.Name, vaults.RepoEdit{Changes: &policy}, time.Now())
}

func (e *env) newRepo(args []string) (int, error) {
	fs := newFlags("new-repo", e.stderr)
	at := fs.String("at", "", "where to create it (default: repos/ inside the project)")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) != 2 {
		return 2, errors.New("usage: claude-atlas new-repo NAME REPO [--at DIR]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.entry(cfg, positional[0])
	if err != nil {
		return 1, err
	}
	repo, path, err := vaults.CreateRepo(e.home, cfg, entry, positional[1], *at, time.Now())
	if err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "created", home.Display(path)+" with its own git history")
	e.console.Step(console.OK, "linked", fmt.Sprintf("%s → %s", repo.Name, entry.Name))
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) unlink(args []string) (int, error) {
	if len(args) != 2 {
		return 2, errors.New("usage: claude-atlas unlink NAME REPO")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.entry(cfg, args[0])
	if err != nil {
		return 1, err
	}
	if err := vaults.RemoveRepo(e.home, cfg, entry, args[1], time.Now()); err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "unlinked", fmt.Sprintf("%s from %s; the folder is untouched", args[1], entry.Name))
	e.console.Step(console.OK, "refreshed", refreshed(entries))
	return 0, nil
}

func (e *env) repos(args []string) (int, error) {
	if len(args) > 1 {
		return 2, errors.New("usage: claude-atlas repos [NAME]")
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		return 1, err
	}
	c := e.console
	if len(args) == 1 {
		entry, err := findEntry(ix, args[0])
		if err != nil {
			return 1, err
		}
		if entry.Kind == vault.Knowledge {
			c.Say("a knowledge base has no repositories; mount it in a project instead")
			return 0, nil
		}
		if len(entry.Repos) == 0 {
			c.Say("%s has no repositories; link one with `claude-atlas link %s PATH` or create one with `claude-atlas new-repo %s NAME`", entry.Name, entry.Name, entry.Name)
			return 0, nil
		}
		for _, r := range entry.Repos {
			c.Say("  %s", repoLine(entry, r))
		}
		return 0, nil
	}
	shown := 0
	for _, p := range ix.Projects() {
		if len(p.Repos) == 0 {
			continue
		}
		c.Say("%s", p.Name)
		for _, r := range p.Repos {
			c.Say("  %s", repoLine(p, r))
			shown++
		}
	}
	if shown == 0 {
		c.Say("no repositories yet; link one with `claude-atlas link NAME PATH` or create one with `claude-atlas new-repo NAME REPO`")
	}
	return 0, nil
}

func (e *env) editRepo(args []string) (int, error) {
	fs := newFlags("edit-repo", e.stderr)
	remote := fs.String("remote", "", "the repository's remote URL; \"\" clears it")
	changes := fs.String("changes", "", "how changes land: pr or commit; \"\" returns to the default")
	path := fs.String("path", "", "the folder the project reaches under that name")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	set := setFlags(fs)
	if len(positional) != 2 || len(set) == 0 {
		return 2, errors.New("usage: claude-atlas edit-repo NAME REPO [--remote URL] [--changes pr|commit] [--path DIR]")
	}
	if set["changes"] && *changes != "" {
		if err := checkPolicy(*changes); err != nil {
			return 2, err
		}
	}
	cfg, err := e.home.Load()
	if err != nil {
		return 1, err
	}
	entry, err := e.entry(cfg, positional[0])
	if err != nil {
		return 1, err
	}
	edit := vaults.RepoEdit{Path: *path}
	if set["remote"] {
		edit.Remote = remote
	}
	if set["changes"] {
		edit.Changes = changes
	}
	updated, err := vaults.EditRepo(e.home, cfg, entry, positional[1], edit, time.Now())
	if err != nil {
		return 1, err
	}
	entries, _, err := e.refreshAll(cfg)
	if err != nil {
		return 1, err
	}
	policy := links.Policy(updated.Changes, updated.Remote)
	e.console.Step(console.OK, "edited", fmt.Sprintf("%s on %s", updated.Name, entry.Name))
	e.console.Step(console.OK, "changes", policy+"  "+links.PolicyText(policy))
	e.console.Step(console.OK, "refreshed", refreshed(entries))
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
	entries, ix, changes, err := refresh.Registry(e.home, cfg, e.home.StateDir(), time.Now(), true)
	if err != nil {
		return 1, err
	}
	for _, ch := range changes {
		for _, name := range ch.Adopted {
			e.console.Step(console.OK, "linked", fmt.Sprintf("%s in %s: a repository waiting under repos/", name, ch.Project))
		}
		for _, name := range ch.Created {
			e.console.Step(console.OK, "created", fmt.Sprintf("%s in %s", kbPath(name), ch.Project))
		}
		for _, name := range ch.Repaired {
			e.console.Step(console.OK, "repaired", fmt.Sprintf("%s in %s: it pointed elsewhere", kbPath(name), ch.Project))
		}
		for _, name := range ch.Removed {
			e.console.Step(console.OK, "removed", fmt.Sprintf("%s from %s", kbPath(name), ch.Project))
		}
		for _, name := range ch.Missing {
			e.console.Step(console.Fail, "missing", fmt.Sprintf("%s in %s: no knowledge base with that id", kbPath(name), ch.Project))
		}
		if ch.Error != "" {
			e.console.Step(console.Fail, "refresh", ch.Project+": "+ch.Error)
		}
	}
	for _, en := range entries {
		switch {
		case en.Error != "":
			e.console.Step(console.Fail, entryName(en), en.Error)
		case en.State == nil:
			e.console.Step(console.Fail, en.Name, "not read")
		case !en.State.VaultOK:
			e.console.Step(console.Fail, en.Name, en.State.VaultError)
		default:
			heat := en.State.Heat
			if heat == "" {
				heat = "-"
			}
			days := "?"
			if en.State.DaysIdle != nil {
				days = fmt.Sprint(*en.State.DaysIdle)
			}
			e.console.Step(console.OK, en.Name, fmt.Sprintf("%s, idle %sd", heat, days))
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

// findTask finds a task in a vault by id, or by the start of its title.
func (e *env) findTask(entry registry.Entry, key string) (*tasks.Record, error) {
	v, err := vault.Open(entry.Path)
	if err != nil {
		return nil, err
	}
	led, err := tasks.Current(v, time.Now())
	if err != nil {
		return nil, err
	}
	if rec := led.Find(key); rec != nil {
		return rec, nil
	}
	var matches []*tasks.Record
	for i := range led.Tasks {
		if strings.HasPrefix(strings.ToLower(led.Tasks[i].Title), strings.ToLower(key)) {
			matches = append(matches, &led.Tasks[i])
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, fmt.Errorf("no task %q in %s; see `claude-atlas tasks %s`", key, entry.Name, entry.Name)
	}
	return nil, fmt.Errorf("%q matches several tasks in %s; use the id", key, entry.Name)
}

func (e *env) plant(args []string) (int, error) {
	fs := newFlags("plant", e.stderr)
	title := fs.String("title", "", "the task's title; taken from the text when omitted")
	priority := fs.String("priority", "", "high, normal, low, or someday")
	workdir := fs.String("workdir", "", "the folder the work happens in")
	due := fs.String("due", "", "YYYY-MM-DD")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) < 2 {
		return 2, errors.New("usage: claude-atlas plant VAULT TEXT... [--title T] [--priority P] [--workdir DIR] [--due DATE]")
	}
	v, err := e.openVaultArg(positional[0])
	if err != nil {
		return 1, err
	}
	now := time.Now()
	req, planted, err := txn.PlantRequest(v, tasks.Plant{Title: *title, Text: strings.Join(positional[1:], " "), Priority: *priority, Workdir: home.Expand(*workdir), Due: *due}, "", now)
	if err != nil {
		return 1, err
	}
	plan, err := txn.Prepare(v, req, now)
	if err != nil {
		return 1, err
	}
	res, err := txn.Apply(v, plan, now)
	if err != nil {
		return 1, err
	}
	e.console.Step(console.OK, "planted", fmt.Sprintf("%s (%s)", planted.Path, planted.ID))
	e.console.Step(console.OK, "committed", res.OperationID)
	return 0, nil
}

func (e *env) stub(args []string) (int, error) {
	fs := newFlags("stub", e.stderr)
	pageType := fs.String("type", "", "the type of every stub: concept or entity; in a project also question or session; in lyt mode note or moc")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) < 1 {
		return 2, errors.New("usage: claude-atlas stub VAULT [TITLE...] [--type T]")
	}
	v, err := e.openVaultArg(positional[0])
	if err != nil {
		return 1, err
	}
	var titles []txn.StubTitle
	for _, title := range positional[1:] {
		titles = append(titles, txn.StubTitle{Title: title})
	}
	// nil mounts: the CLI reads the symlinks under kb/, which refresh keeps current.
	res, err := txn.StubPages(v, titles, *pageType, nil, time.Now())
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

func (e *env) tasks(args []string) (int, error) {
	fs := newFlags("tasks", e.stderr)
	all := fs.Bool("all", false, "include done and cancelled tasks")
	status := fs.String("status", "", "only tasks with this status")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 {
		return 2, errors.New("usage: claude-atlas tasks [VAULT] [--all] [--status S]")
	}
	now := time.Now()
	v, err := e.openVaultArg(first(positional))
	if err != nil && len(positional) == 0 && errors.Is(err, vault.ErrNotVault) {
		return e.allTasks(now, *all, *status)
	}
	if err != nil {
		return 1, err
	}
	led, err := tasks.Current(v, now)
	if err != nil {
		return 1, err
	}
	e.printTasks(led, now, *all, *status, "")
	if notes := tasks.Notes(v); len(notes) > 0 {
		e.console.Say("  %d task note%s waiting in %s/: %s", len(notes), plural(len(notes)), vault.InboxTasksDir, strings.Join(notes, ", "))
	}
	for _, p := range led.Problems {
		e.console.Step(console.Fail, p.Path, p.Reason)
	}
	return 0, nil
}

// allTasks lists the open tasks of every project the scan finds.
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
	for _, p := range ix.Projects() {
		v, err := vault.Open(p.Path)
		if err != nil {
			continue
		}
		led, err := tasks.Current(v, now)
		if err != nil {
			continue
		}
		list := led.Open()
		if all {
			list = append(list, led.Archived()...)
		}
		if len(list) == 0 {
			continue
		}
		shown += len(list)
		e.printTasks(led, now, all, status, p.Name)
	}
	if shown == 0 {
		e.console.Say("no open tasks in any project; plant one with `claude-atlas plant NAME \"...\"`")
	}
	return 0, nil
}

func (e *env) printTasks(led tasks.Ledger, now time.Time, all bool, status, project string) {
	list := led.Open()
	if all {
		list = append(list, led.Archived()...)
	}
	if project != "" {
		e.console.Say("%s", project)
	}
	if len(list) == 0 {
		e.console.Say("  no open tasks")
		return
	}
	for _, r := range list {
		if status != "" && r.Status != status {
			continue
		}
		flags := ""
		if tasks.Stale(r, now) {
			flags = "  stale"
		}
		if r.Due != "" {
			flags += "  due " + r.Due
		}
		e.console.Say("  %-9s %-8s %-40s %s  %s%s", r.Status, r.Priority, r.Title, r.ID, dash(r.LastTouched), flags)
		if r.Workdir != "" {
			e.console.Say("  %-9s %-8s %s", "", "", "workdir "+home.Display(r.Workdir))
		}
	}
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func (e *env) upgrade(args []string) (int, error) {
	fs := newFlags("upgrade", e.stderr)
	all := fs.Bool("all", false, "every vault the atlas knows")
	positional, err := parse(fs, args)
	if err != nil {
		return 2, nil
	}
	if len(positional) > 1 || (*all && len(positional) > 0) {
		return 2, errors.New("usage: claude-atlas upgrade [VAULT] or claude-atlas upgrade --all")
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
			if *all && errors.Is(err, vault.ErrV1) {
				e.console.Step(console.Skip, home.Display(root), "v1 vault; adopt it")
				continue
			}
			return 1, err
		}
		if len(res.Added)+len(res.Moved) == 0 {
			e.console.Step(console.Skip, home.Display(root), "current")
			continue
		}
		e.console.Step(console.OK, home.Display(root), setupChanges(res.Added, res.Moved))
		for _, rel := range res.Added {
			if strings.HasPrefix(rel, ".obsidian/snippets/") {
				restyled = true
			}
		}
	}
	if restyled {
		e.console.Say("  The folder colors come from .obsidian/snippets/claude-atlas.css, enabled in each vault's appearance settings; reload Obsidian (Cmd+R) to see them. An older vault-colors.css may stay; the new snippet takes precedence.")
	}
	return 0, nil
}

// setupChanges says what an adopt or an upgrade did to a vault's files.
func setupChanges(added []string, moved []vault.Move) string {
	var parts []string
	if len(added) > 0 {
		parts = append(parts, "added "+strings.Join(added, ", "))
	}
	for _, m := range moved {
		parts = append(parts, fmt.Sprintf("moved %s to %s", m.From, m.To))
	}
	if len(parts) == 0 {
		return "committed the files already there"
	}
	return strings.Join(parts, "; ")
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
		row("new-days", fmt.Sprintf("%d  (a vault is new for this many days after its creation; 0 turns it off)", cfg.NewDays()))
		row("repo-changes", cfg.DefaultChanges()+"  (how a newly linked repository lands its changes: commit or pr)")
		row("vaults dir", home.Display(cfg.VaultsDir))
		row("claude command", cfg.ClaudeCode.Command)
		row("plugin source", cfg.Plugin.Source)
		row("file", home.Display(e.home.ConfigPath()))
		return 0, nil
	}
	if len(args) != 2 {
		return 2, errors.New("usage: claude-atlas config [KEY VALUE]; keys: new-days, repo-changes")
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
	case "repo-changes":
		if err := cfg.SetDefaultChanges(args[1]); err != nil {
			return 2, err
		}
	default:
		return 2, fmt.Errorf("unknown setting %q; keys: new-days, repo-changes", args[0])
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
	row("vaults", fmt.Sprintf("%d found", len(ix.Entries)))
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
	line("vaults dir", home.Display(cfg.VaultsDir))
	ix, err := registry.Scan(cfg)
	if err != nil {
		return 1, err
	}
	line("vaults", fmt.Sprintf("%d found", len(ix.Entries)))
	for _, en := range ix.Entries {
		status := vaultStatus(en)
		if status != "ok" {
			ok = false
		}
		c.Say("    %-7s %-24s %s", status, entryName(en), home.Display(en.Path))
	}
	for _, en := range ix.Entries {
		for _, m := range en.Mounts {
			if m.Error != "" {
				ok = false
				c.Step(console.Fail, en.Name+" · "+m.Name, m.Error)
				continue
			}
			// Doctor reports the symlink; refresh is what makes it.
			if state := vaults.MountState(en, m); state != vaults.MountOK {
				ok = false
				c.Step(console.Fail, en.Name+" · "+kbPath(m.Name), state+"; run `claude-atlas refresh` to recreate it")
			}
		}
		for _, r := range en.Repos {
			if r.Error != "" {
				ok = false
				c.Step(console.Fail, en.Name+" · "+r.Name, r.Error)
			}
		}
		if en.Kind != vault.Knowledge {
			continue
		}
		// `cluster add` refuses a member that is not a knowledge base, a member that is a
		// cluster, and a cluster that is already a member. Only a hand-edited identity file
		// produces those states, so doctor reports them.
		drop := "; run `claude-atlas cluster remove " + en.Name + " "
		for _, m := range en.Members {
			member := ix.ByID(m.ID)
			switch {
			case member != nil && member.Kind != vault.Knowledge:
				ok = false
				c.Step(console.Fail, en.Name+" · member "+m.Name, fmt.Sprintf("%s is a %s, not a knowledge base%s%s`", member.Name, member.Kind, drop, m.ID))
			case m.Error != "":
				ok = false
				c.Step(console.Fail, en.Name+" · member "+m.Name, m.Error+drop+m.ID+"`")
			case member != nil && len(member.Members) > 0:
				ok = false
				c.Step(console.Fail, en.Name+" · member "+m.Name, m.Name+" is a cluster too, and a cluster holds no cluster; edit "+home.Display(filepath.Join(en.Path, vault.Marker)))
			}
		}
		for _, g := range en.Grants {
			if g.Error != "" {
				ok = false
				c.Step(console.Fail, en.Name+" · grant "+g.Name, g.Error+"; run `claude-atlas revoke "+en.Name+" "+g.ID+"`")
				continue
			}
			if en.Access == vault.AccessOpen {
				c.Step(console.Skip, en.Name+" · grant "+g.Name, en.Name+" is open; the grant applies when it is guarded")
			}
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

// vaultStatus is doctor's one word for a vault: what stands between it and working.
func vaultStatus(en registry.Entry) string {
	if en.Error != "" {
		return unreadable(en)
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

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
