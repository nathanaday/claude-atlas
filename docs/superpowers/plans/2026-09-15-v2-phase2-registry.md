# v2 Phase 2: The Registry Replaces the Atlas Vault — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The atlas learns its vaults by scanning for identity files instead of reading project pages in an Obsidian vault; config v2 holds the only stored paths; `refresh` derives `~/.claude-atlas/state/registry.json`; the CLI and the TUI run over that registry; `tree`, `pages`, the atlas-vault rendering, the link pages, and `related` are deleted.

**Architecture:** A new package `internal/registry` scans `vaults_dir` and the config's extra paths for identity files and builds an `Entry` per vault (identity, path, resolved mounts and repositories); `refresh` derives per-entry state and writes the registry file; `discover` finds a session's project through a repository path in the registry. A vault's own facts (name, tags, scope, access, repos) change through one function, `vault.UpdateConfig`, which rewrites the identity file and commits it as a `setup` operation. New code goes in bottom-up (registry, config fields, identity edits, derive, discovery), then the CLI and wizard switch, then the TUI, then the old packages are deleted.

**Tech Stack:** Go 1.24.2, the MCP go-sdk v1.4.0, Bubble Tea, git.

**Spec:** `docs/v2-design.md`: "The identity file", "The atlas", "Rules", and phase 2 in "Phases". Phase 1 (`docs/superpowers/plans/2026-09-15-v2-phase1-engine.md`) is on `main`; this plan builds on its `vault.Config`, `vault.Options`, `vault.Kind`, and `vault.ReadConfig`.

## Global Constraints

- No new dependencies. Allowed: `gopkg.in/yaml.v3`, MCP `go-sdk` v1.4.0, Bubble Tea, Bubbles, Lip Gloss.
- The identity file never holds a path. The atlas config holds every path the atlas cannot compute: vault roots outside `vaults_dir` (`vaults`) and repository paths outside the project folder (`repos`, keyed `<project id>/<repo name>`). Everything else about a vault is computed by a scan or derived by `refresh`.
- `state/registry.json` is derived: `refresh` deletes and rewrites it. The server, the hooks, and the CLI find vaults by scanning, never by reading the registry file; the registry file feeds `view`, `list`, `show`, and `doctor`.
- The scan walks `vaults_dir` at most five directory levels deep, skips dot-directories and `node_modules`, and never descends into a vault (a vault never contains a vault).
- A project's repositories live at `<project>/repos/<name>/` unless the config maps them elsewhere; the project's git ignores `/repos/`.
- `vault.Init`, `vault.Adopt`, `vault.Upgrade`, and the new `vault.UpdateConfig` are the only code that writes vault files directly. `UpdateConfig` commits the identity file as one `setup` operation.
- The atlas never writes into a vault except through those functions. Lint and refresh stay read-only toward every vault, offline, and idempotent.
- The TUI is a subset of the CLI: every key maps to a command; `Update` holds the logic; tests drive with `tea.KeyMsg`.
- Tests never touch a real `~/.claude-atlas`, never install a plugin, and skip when `git` is missing.
- Every task ends with `go build ./... && go vet ./... && go test ./...` passing.
- Commits: author `nathanaday <nraday1221@gmail.com>` (`-c user.name=nathanaday -c user.email=nraday1221@gmail.com`); no `Co-Authored-By`; subject `area: what changed`. Stage files by name; never stage `.superpowers/`.
- Comments are one-line doc comments in the style of the surrounding code. Prose follows the user's writing guide: short sentences, plain words, no metaphors.

## Default locations

`<vaults dir>/knowledge/<name>` for a knowledge base and `<vaults dir>/projects/<name>` for a project, unless the user gives a path. A vault outside `vaults_dir` is listed in the config's `vaults`.

## File map

| File | Change |
|---|---|
| `internal/registry/registry.go`, `registry_test.go` | new: `Entry`, `Scan`, `Index`, name lookup, repo and mount resolution, the state file |
| `internal/home/home.go`, `home_test.go` | config v2: `Vaults`, `Repos`; `AtlasVault` optional until Task 9 |
| `internal/vault/vault.go`, `vault_test.go` | `UpdateConfig`; project `.gitignore` gains `/repos/` |
| `internal/links/repos.go`, `links.go`, `links_test.go` | repository helpers in one file; `RemoteURL`; materials dropped |
| `internal/vaults/identity.go`, `identity_test.go` | `Register`, `Unregister`, `EditIdentity`, `AddRepo`, `CreateRepo`, `CloneRepo`, `RemoveRepo`, `EditRepo`, `PathFor` |
| `internal/refresh/derive.go`, `refresh.go`, `refresh_test.go` | `Derive`, `Registry`, `Signals` over entries; old rendering deleted in Task 9 |
| `internal/discover/discover.go`, `discover_test.go` | over the registry |
| `internal/hooks/hooks.go`, `hooks_test.go`, `internal/mcpserver/server.go`, `server_test.go` | the new `discover.Match`; `repos` tool over entries |
| `internal/cli/cli.go`, `cli_test.go` | every command over the registry; new and removed commands |
| `internal/wizard/wizard.go` | no atlas vault; the first vault is a project |
| `internal/tui/*.go`, `*_test.go` | screens over `registry.Entry` |
| deleted in Task 9 | `internal/tree/`, `internal/pages/`, `internal/links/pages.go`, `internal/refresh/graph.go`, the old `refresh` rendering, the old `vaults` functions, `home.Config.AtlasVault` |
| `CLAUDE.md`, `docs/usage.md`, `docs/v2-design.md` | the new layout and commands |

---

### Task 1: The registry package

**Files:**
- Create: `internal/registry/registry.go`
- Create: `internal/registry/registry_test.go`

**Interfaces:**
- Consumes: `vault.ReadConfig`, `vault.Config`, `vault.Kind`, `vault.Schema`, `vault.SchemaV1`, `vault.AccessOpen`, `vault.AccessGuarded`, `vault.AccessRead`, `vault.AccessWrite`, `home.Config` (Task 2 adds `Vaults` and `Repos`; this task reads them, so Task 2 must land first — **do Task 2 before Task 1**; the numbering keeps the registry first in the file map only).
- Produces:

```go
package registry

// Ref names a vault another entry refers to.
type Ref struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Access string `json:"access,omitempty"` // the effective access, on a mount or a mounted-by
}

// Mount is a project's mount resolved against the scan.
type Mount struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Access    string `json:"access"`              // what the project asked for
	Effective string `json:"effective,omitempty"` // the lesser of the request and the grant; "" when unresolved
	Path      string `json:"path,omitempty"`      // the knowledge base's wiki/, when found
	Error     string `json:"error,omitempty"`     // "no knowledge base with id …"
}

// Repo is a project's repository resolved to a path.
type Repo struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"` // "" when the config names no path and <project>/repos/<name> is absent
	Remote  string `json:"remote,omitempty"`
	Changes string `json:"changes,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Entry is one vault the atlas knows: its identity file, where it is, and how its
// mounts and repositories resolve. Refresh adds the derived State.
type Entry struct {
	ID      string     `json:"id"`
	Kind    vault.Kind `json:"kind"`
	Name    string     `json:"name"`
	Path    string     `json:"path"`
	Mode    vault.Mode `json:"mode"`
	Created string     `json:"created"`
	Tags    []string   `json:"tags,omitempty"`
	Scope   string     `json:"scope,omitempty"`
	Access  string     `json:"access,omitempty"`
	Grants  []vault.Grant `json:"grants,omitempty"`
	Mounts  []Mount    `json:"mounts,omitempty"`
	Repos   []Repo     `json:"repos,omitempty"`
	// MountedBy lists the projects that mount a knowledge base, with their effective access.
	MountedBy []Ref `json:"mounted_by,omitempty"`
	// Error is set for a vault the scan found but could not read: a v1 identity file, or
	// one that is not JSON. Such an entry has Path and Error and nothing else.
	Error string `json:"error,omitempty"`
	State *State `json:"state,omitempty"`
}

// State is what refresh derived for one vault; the refresh package fills it.
type State struct {
	GeneratedAt     string   `json:"generated_at"`
	VaultOK         bool     `json:"vault_ok"`
	VaultError      string   `json:"vault_error,omitempty"`
	PendingRecovery bool     `json:"pending_recovery,omitempty"`
	LastOperation   string   `json:"last_operation,omitempty"`
	LastTouched     string   `json:"last_touched,omitempty"`
	DaysIdle        *int     `json:"days_idle"`
	Heat            string   `json:"heat"`
	Pages           *int     `json:"pages"`
	OpenThreads     []string `json:"open_threads"`
	Unfinished      Unfinished `json:"unfinished"`
	Tasks           *TaskSummary `json:"tasks,omitempty"`
	// RepoFacts pairs each repository name with what git says about it.
	RepoFacts map[string]links.Link `json:"repo_facts,omitempty"`
}

// Unfinished, TaskLine, TaskSummary: moved here from internal/tree verbatim (with their
// Text and Total methods), tasks.Counts included by import.

// Index is one scan.
type Index struct {
	Entries  []Entry  // sorted: projects first, then knowledge bases, each by name then path
	Problems []Problem
}

// Problem is a path the scan could not use.
type Problem struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func Scan(cfg *home.Config) (*Index, error)
func (ix *Index) ByID(id string) *Entry
func (ix *Index) ByPath(path string) *Entry
// Find matches a name without regard to case, an id or an id prefix of at least 8
// characters, or a path. Two vaults with one name make it return ErrAmbiguous with both.
func (ix *Index) Find(arg string) (*Entry, error)
func (ix *Index) Projects() []Entry
func (ix *Index) Knowledge() []Entry
var ErrAmbiguous = errors.New("ambiguous")
var ErrNotFound = errors.New("no such vault")

// Rel is the entry's place in the view: projects/<first tag>/<name> or projects/<name>
// for a project, knowledge/<name> for a knowledge base.
func (e Entry) Rel() string
// Wiki is the entry's wiki folder.
func (e Entry) Wiki() string
// RepoDir is where a repository of that name sits by default.
func (e Entry) RepoDir(name string) string

const StateSchema = "claude-atlas.registry.v1"
func File(stateDir string) string          // <stateDir>/registry.json
func Write(stateDir string, entries []Entry, generatedAt string) error
func Read(stateDir string) ([]Entry, string, error)  // entries, generatedAt; os.ErrNotExist when never refreshed
```

- [ ] **Step 1: Write the failing tests**

`internal/registry/registry_test.go`:

```go
package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// fixture makes a vaults directory with two knowledge bases and two projects, one vault
// outside it, one v1 vault, and one unreadable identity file.
func fixture(t *testing.T) (*home.Config, map[string]*vault.Vault) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{VaultsDir: filepath.Join(root, "Vaults")}
	vs := map[string]*vault.Vault{}
	mk := func(rel string, opts vault.Options) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := vault.Init(path, opts, now); err != nil {
			t.Fatal(err)
		}
		v, _ := vault.Open(path)
		vs[opts.Name] = v
	}
	mk("Vaults/knowledge/ai-ml", vault.Options{Kind: vault.Knowledge, Name: "ai-ml"})
	mk("Vaults/knowledge/deep/nested/robotics", vault.Options{Kind: vault.Knowledge, Name: "robotics"})
	mk("Vaults/projects/cs566", vault.Options{Kind: vault.Project, Name: "cs566"})
	mk("Vaults/projects/self-study", vault.Options{Kind: vault.Project, Name: "self-study"})
	mk("Elsewhere/side", vault.Options{Kind: vault.Project, Name: "side"})
	cfg.Vaults = []string{filepath.Join(root, "Elsewhere", "side")}
	old := filepath.Join(root, "Vaults", "old")
	os.MkdirAll(filepath.Join(old, "wiki"), 0o755)
	os.WriteFile(filepath.Join(old, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	bad := filepath.Join(root, "Vaults", "bad")
	os.MkdirAll(bad, 0o755)
	os.WriteFile(filepath.Join(bad, vault.Marker), []byte(`{not json`), 0o644)
	os.MkdirAll(filepath.Join(root, "Vaults", ".hidden", "v"), 0o755)
	os.WriteFile(filepath.Join(root, "Vaults", ".hidden", "v", vault.Marker), []byte(`{}`), 0o644)
	return cfg, vs
}

func TestScanFindsEveryVaultAndSortsThem(t *testing.T) {
	cfg, vs := fixture(t)
	ix, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ix.Entries {
		names = append(names, string(e.Kind)+":"+e.Name)
	}
	if got := strings.Join(names, ","); got != "project:cs566,project:self-study,project:side,knowledge:ai-ml,knowledge:robotics" {
		t.Fatalf("entries %s", got)
	}
	if len(ix.Problems) != 2 {
		t.Fatalf("problems %+v", ix.Problems)
	}
	for _, p := range ix.Problems {
		switch filepath.Base(p.Path) {
		case "old":
			if !strings.Contains(p.Reason, "v1") {
				t.Errorf("old: %s", p.Reason)
			}
		case "bad":
			if !strings.Contains(p.Reason, "not JSON") && !strings.Contains(p.Reason, "unreadable") {
				t.Errorf("bad: %s", p.Reason)
			}
		default:
			t.Errorf("unexpected problem %+v", p)
		}
	}
	if e := ix.ByID(vs["ai-ml"].Config.ID); e == nil || e.Path != vs["ai-ml"].Root || e.Access != vault.AccessOpen {
		t.Fatalf("by id %+v", e)
	}
	if e := ix.ByPath(vs["side"].Root); e == nil || e.Name != "side" {
		t.Fatalf("by path %+v", e)
	}
	if e, err := ix.Find("CS566"); err != nil || e.Name != "cs566" {
		t.Fatalf("find by name %+v %v", e, err)
	}
	if e, err := ix.Find(vs["robotics"].Config.ID[:8]); err != nil || e.Name != "robotics" {
		t.Fatalf("find by id prefix %+v %v", e, err)
	}
	if e, err := ix.Find(vs["self-study"].Root); err != nil || e.Name != "self-study" {
		t.Fatalf("find by path %+v %v", e, err)
	}
	if _, err := ix.Find("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find missing: %v", err)
	}
	if e := ix.ByID(vs["cs566"].Config.ID); e.Rel() != "projects/cs566" || e.Wiki() != filepath.Join(e.Path, "wiki") || e.RepoDir("hw") != filepath.Join(e.Path, "repos", "hw") {
		t.Fatalf("rel %q wiki %q repodir %q", e.Rel(), e.Wiki(), e.RepoDir("hw"))
	}
}

func TestScanResolvesMountsReposAndGrants(t *testing.T) {
	cfg, vs := fixture(t)
	kb, p := vs["ai-ml"], vs["cs566"]
	// A guarded knowledge base that grants cs566 read; cs566 asks for write; self-study asks for write with no grant.
	if err := vault.UpdateConfig(kb.Root, "guard", now, func(c *vault.Config) error {
		c.Access = vault.AccessGuarded
		c.Grants = []vault.Grant{{ID: p.Config.ID, Name: "cs566", Access: vault.AccessRead}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := vault.UpdateConfig(p.Root, "mount", now, func(c *vault.Config) error {
		c.Mounts = []vault.Mount{{ID: kb.Config.ID, Name: "ai-ml", Access: vault.AccessWrite}, {ID: "00000000-0000-4000-8000-000000000009", Name: "gone", Access: vault.AccessRead}}
		c.Repos = []vault.Repo{{Name: "hw", Changes: "commit"}, {Name: "paper", Remote: "git@x:y/paper.git"}, {Name: "lost"}}
		c.Tags = []string{"usc", "fall"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	self := vs["self-study"]
	if err := vault.UpdateConfig(self.Root, "mount", now, func(c *vault.Config) error {
		c.Mounts = []vault.Mount{{ID: kb.Config.ID, Name: "ai-ml", Access: vault.AccessWrite}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(p.Root, "repos", "hw"), 0o755)
	elsewhere := t.TempDir()
	cfg.Repos = map[string]string{p.Config.ID + "/paper": elsewhere}
	ix, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := ix.ByID(p.Config.ID)
	if e.Rel() != "projects/usc/cs566" {
		t.Fatalf("rel %q", e.Rel())
	}
	if len(e.Mounts) != 2 || e.Mounts[0].Effective != vault.AccessRead || e.Mounts[0].Path != filepath.Join(kb.Root, "wiki") || e.Mounts[1].Error == "" || e.Mounts[1].Effective != "" {
		t.Fatalf("mounts %+v", e.Mounts)
	}
	if len(e.Repos) != 3 || e.Repos[0].Path != filepath.Join(p.Root, "repos", "hw") || e.Repos[1].Path != elsewhere || e.Repos[2].Path != "" || e.Repos[2].Error == "" {
		t.Fatalf("repos %+v", e.Repos)
	}
	k := ix.ByID(kb.Config.ID)
	if len(k.MountedBy) != 2 || k.MountedBy[0].Name != "cs566" || k.MountedBy[0].Access != vault.AccessRead || k.MountedBy[1].Name != "self-study" || k.MountedBy[1].Access != vault.AccessRead {
		t.Fatalf("mounted by %+v", k.MountedBy)
	}
	open := ix.ByID(vs["robotics"].Config.ID)
	if len(open.MountedBy) != 0 {
		t.Fatalf("robotics mounted by %+v", open.MountedBy)
	}
}

func TestStateFileRoundTrips(t *testing.T) {
	cfg, _ := fixture(t)
	ix, _ := Scan(cfg)
	dir := filepath.Join(t.TempDir(), "state")
	if _, _, err := Read(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read before write: %v", err)
	}
	four := 4
	ix.Entries[0].State = &State{GeneratedAt: "2026-09-15T12:00:00Z", VaultOK: true, Pages: &four, Heat: "new", OpenThreads: []string{}}
	if err := Write(dir, ix.Entries, "2026-09-15T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	entries, generated, err := Read(dir)
	if err != nil || generated != "2026-09-15T12:00:00Z" || len(entries) != len(ix.Entries) || entries[0].State == nil || *entries[0].State.Pages != 4 {
		t.Fatalf("round trip %v %s %+v", err, generated, entries)
	}
	data, _ := os.ReadFile(File(dir))
	if !strings.Contains(string(data), `"schema": "claude-atlas.registry.v1"`) {
		t.Fatalf("file:\n%s", data)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/registry/`
Expected: compile errors (`undefined: Scan`, `vault.UpdateConfig` — Task 3 provides `UpdateConfig`; **do Tasks 2 and 3 before this task's Step 3**, then return here).

- [ ] **Step 3: Write the package**

`internal/registry/registry.go`, with these rules:

- `Scan`: walk `cfg.VaultsDir` with `filepath.WalkDir`; skip a directory whose name starts with `.` or is `node_modules`; at each directory, if `vault.Marker` exists there, take it as a vault root and `SkipDir`; stop descending when `strings.Count(rel, "/") >= 5`. Then visit each path in `cfg.Vaults` (skip one already found; a missing folder is a `Problem` "not found"). For each root: `vault.ReadConfig`; when `!ok` and the marker file exists, `Problem{Path, "identity file is not JSON; run claude-atlas adopt"}` plus an `Entry{Path, Error}`; when `cfg.Schema == vault.SchemaV1`, `Problem{Path, "v1 vault; run claude-atlas adopt PATH --as knowledge|project"}` plus an `Entry{Path, Error}`; other schemas: a Problem "unsupported schema". A valid config becomes an Entry: `ID, Kind, Name (default filepath.Base), Path, Mode (default generic), Created, Tags, Scope, Access, Grants`; for a project, `Mounts` and `Repos` copied from the config, then resolved in a second pass once every entry is known.
- Resolution pass: for each project mount, `ByID`; when found and it is a knowledge base, `Path = kb.Wiki()` and `Effective = effective(mount.Access, kbAccess(kb, project.ID))`; else `Error = "no knowledge base with id " + id`. `kbAccess`: `open` → `write`; `guarded` → the grant's access for the project id, else `read`. `effective(request, grant)`: `read` if either is `read`, else `write`. For each repo: `Path` = `RepoDir(name)` if that directory exists, else `cfg.Repos[id+"/"+name]` if set, else `Error = "no folder; link it with claude-atlas link"`. `MountedBy` on the knowledge base: one `Ref{project.ID, project.Name, Effective}` per resolved mount, in project order.
- Entries with `Error` set are kept in `Entries` (so `list` and `doctor` show them) but excluded from `Find`, `Projects`, `Knowledge`; sorting puts them last.
- `Find`: exact path match after `filepath.Abs(home.Expand(arg))` when the arg contains a separator or `~`; else name match (`strings.EqualFold`); else id equality or prefix when `len(arg) >= 8`. Several name matches → `fmt.Errorf("%w: %s is the name of %d vaults (%s); use the path or the id", ErrAmbiguous, ...)`.
- `Rel`: knowledge → `"knowledge/" + Name`; project with tags → `"projects/" + Tags[0] + "/" + Name`; else `"projects/" + Name`.
- `Write`: `os.MkdirAll(stateDir)`, marshal `{schema, generated_at, entries}` with indent, write `registry.json` atomically (temp + rename). `Read`: parse; a missing file returns `os.ErrNotExist` wrapped.
- Move `Unfinished`, `TaskLine`, `TaskSummary` and the two `Unfinished` methods from `internal/tree/tree.go` into this package unchanged (copy now; Task 9 deletes the originals).

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./internal/registry/ ./internal/vault/ ./internal/home/`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/registry/registry.go internal/registry/registry_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "registry: scan the vaults directory for identity files and resolve mounts and repositories"
```

---

### Task 2: Config v2 fields

**Files:**
- Modify: `internal/home/home.go`
- Modify: `internal/home/home_test.go`

**Interfaces:**
- Produces: `const ConfigSchema = "claude-atlas.config.v2"`, `const ConfigSchemaV1 = "claude-atlas.config.v1"`; `Config.Vaults []string` (`vaults,omitempty`), `Config.Repos map[string]string` (`repos,omitempty`); `Config.AtlasVault` stays, tagged `atlas_vault,omitempty` (Task 9 removes it); `func (c *Config) TreeRoot() string` returns `""` when `AtlasVault` is empty; `func RepoKey(projectID, name string) string` (`id + "/" + name`); `func (c *Config) RepoPath(projectID, name string) string`; `func (c *Config) SetRepoPath(projectID, name, path string)` (empty path deletes the key); `func (c *Config) AddVault(root string) bool` (dedupes, reports whether added); `func (c *Config) RemoveVault(root string) bool`; `func (c *Config) Inside(root string) bool` (root is under `VaultsDir`).
- `Load` accepts both schemas and returns the config with `Schema = ConfigSchema`, so the next `Save` writes v2. Paths in `Vaults` and `Repos` are expanded with `Expand`.

- [ ] **Step 1: Write the failing test**

Add to `internal/home/home_test.go` (use the file's existing helpers for a temp home; if it has none, `Home{Root: t.TempDir()}`):

```go
func TestConfigV2FieldsAndV1Upgrade(t *testing.T) {
	h := Home{Root: t.TempDir()}
	cfg := h.Default("~/Vaults", "")
	cfg.Schema = ConfigSchemaV1
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := h.Load()
	if err != nil || loaded.Schema != ConfigSchema {
		t.Fatalf("v1 config loads as v2: %+v %v", loaded, err)
	}
	if loaded.TreeRoot() != "" {
		t.Fatalf("no atlas vault, no tree root: %q", loaded.TreeRoot())
	}
	if !loaded.AddVault("~/Elsewhere/side") || loaded.AddVault("~/Elsewhere/side") {
		t.Fatal("AddVault dedupes")
	}
	loaded.SetRepoPath("id-1", "paper", "~/Code/paper")
	if err := h.Save(loaded); err != nil {
		t.Fatal(err)
	}
	again, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Vaults) != 1 || again.Vaults[0] != Expand("~/Elsewhere/side") || again.RepoPath("id-1", "paper") != Expand("~/Code/paper") || again.RepoPath("id-1", "nope") != "" {
		t.Fatalf("v2 fields %+v", again)
	}
	if !again.Inside(filepath.Join(again.VaultsDir, "projects", "x")) || again.Inside(again.Vaults[0]) {
		t.Fatal("Inside")
	}
	again.SetRepoPath("id-1", "paper", "")
	if !again.RemoveVault(again.Vaults[0]) || len(again.Repos) != 0 || len(again.Vaults) != 0 {
		t.Fatalf("removal %+v", again)
	}
	data, _ := os.ReadFile(h.ConfigPath())
	if !strings.Contains(string(data), `"schema": "claude-atlas.config.v2"`) {
		t.Fatalf("saved schema:\n%s", data)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/home/ -run TestConfigV2FieldsAndV1Upgrade`
Expected: compile errors: `undefined: ConfigSchemaV1`, `loaded.AddVault undefined`.

- [ ] **Step 3: Add the fields and helpers**

In `home.go`: the two schema constants; the struct fields; in `Load`, replace the schema check with `switch cfg.Schema { case ConfigSchema: case ConfigSchemaV1: cfg.Schema = ConfigSchema; default: unsupported }`, then expand every `Vaults` entry and every `Repos` value; `TreeRoot` guards the empty case; the helpers as one-line-doc functions. `Inside` uses `filepath.Rel` and rejects `..` prefixes (copy `under` from `internal/vaults/manage.go`).

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass; `TestSetupCreatesHomeAtlasAndFirstVault` still passes because the wizard still writes `AtlasVault`.

- [ ] **Step 5: Commit**

```bash
git add internal/home/home.go internal/home/home_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "home: config v2 lists vault roots and repository paths the atlas cannot compute"
```

---

### Task 3: Identity edits through one function; repository helpers in one file

**Files:**
- Modify: `internal/vault/vault.go`, `internal/vault/vault_test.go`
- Modify: `internal/vault/templates/project/.gitignore`
- Create: `internal/links/repos.go`; Modify: `internal/links/pages.go` (cut the moved functions), `internal/links/links.go`, `internal/links/links_test.go`

**Interfaces:**
- Produces in `vault`:

```go
// UpdateConfig rewrites the identity file through change and commits it as one setup
// operation named by summary. An unchanged file makes no commit. It is the one way a
// vault's own facts (name, tags, scope, access, grants, mounts, repos) change.
func UpdateConfig(root, summary string, now time.Time, change func(*Config) error) error
// ValidAccess reports whether s is a knowledge base access level (open, guarded) when
// forKB, or a mount and grant level (read, write) otherwise.
func ValidAccess(s string, forKB bool) bool
```

- Produces in `links` (`repos.go`): everything repository-related that `pages.go` held — `IsRepo`, `InitRepo`, `CreateRepo`, `Clone`, `IsRemoteURL`, `NameFromURL`, `CleanName`, `ChangesPR`, `ChangesCommit`, `Policies`, `PolicyText` — moved unchanged, plus:

```go
// RemoteURL is the origin remote of the repository at path, or "".
func RemoteURL(path string) string
// Policy is how sessions land changes: the recorded policy, else pr when there is a
// remote and commit when there is none.
func Policy(changes, remote string) string
```

`Page.Policy()` in `pages.go` becomes a call to `Policy(p.Changes, p.Remote)`.

- [ ] **Step 1: Write the failing tests**

In `internal/vault/vault_test.go`:

```go
func TestUpdateConfigCommitsOnceAndValidates(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "p")
	if _, err := Init(root, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	err := UpdateConfig(root, "tag usc", now, func(c *Config) error { c.Tags = []string{"usc"}; return nil })
	if err != nil {
		t.Fatal(err)
	}
	v, _ := Open(root)
	if len(v.Config.Tags) != 1 || v.Config.Tags[0] != "usc" {
		t.Fatalf("tags %+v", v.Config)
	}
	repo := v.Repo()
	commits, _ := repo.Log(1)
	if commits[0].Subject != "setup: tag usc" || commits[0].Trailers["atlas-operation"] == "" {
		t.Fatalf("commit %+v", commits[0])
	}
	if dirty, _ := repo.Dirty(); dirty {
		t.Fatal("tree should be clean")
	}
	if err := UpdateConfig(root, "tag usc", now, func(c *Config) error { c.Tags = []string{"usc"}; return nil }); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.Log(1); again[0].SHA != commits[0].SHA {
		t.Fatal("an unchanged file makes no commit")
	}
	if err := UpdateConfig(root, "bad", now, func(c *Config) error { c.Kind = "bogus"; return nil }); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("validation: %v", err)
	}
	if err := UpdateConfig(root, "bad", now, func(c *Config) error { return errors.New("no") }); err == nil {
		t.Fatal("change's error is returned")
	}
	if !ValidAccess("guarded", true) || ValidAccess("read", true) || !ValidAccess("read", false) || ValidAccess("open", false) {
		t.Fatal("ValidAccess")
	}
}
```

In `TestInitLayoutByKind`, extend the project `.gitignore` assertion to require both `/kb/` and `/repos/`.

In `internal/links/links_test.go`:

```go
func TestPolicyAndRemoteURL(t *testing.T) {
	if Policy("commit", "git@x:y") != "commit" || Policy("", "git@x:y") != "pr" || Policy("", "") != "commit" {
		t.Fatal("Policy")
	}
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	if err := CreateRepo(dir, "x"); err != nil {
		t.Fatal(err)
	}
	if RemoteURL(dir) != "" {
		t.Fatal("no remote yet")
	}
	exec.Command("git", "-C", dir, "remote", "add", "origin", "git@example.com:a/x.git").Run()
	if RemoteURL(dir) != "git@example.com:a/x.git" {
		t.Fatalf("remote %q", RemoteURL(dir))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vault/ ./internal/links/ 2>&1 | head`
Expected: compile errors `undefined: UpdateConfig`, `undefined: Policy`, `undefined: RemoteURL`.

- [ ] **Step 3: Write the code**

`UpdateConfig`: `Open(root)`; copy `v.Config`; call `change(&cfg)`; validate: `ParseKind`, `ParseMode`, `cfg.Kind == v.Config.Kind` (kind does not change: "a vault's kind does not change"), `cfg.ID == v.Config.ID`, `cfg.Access` empty or `ValidAccess(cfg.Access, true)` for a knowledge base, every grant and mount access `ValidAccess(a, false)`, every repo `Changes` empty or in `links`' policies — but `vault` must not import `links`; check `Changes` against `[]string{"", "pr", "commit"}` with a local list; `cfg.Name` not blank; a project carries no `Scope`/`Access`/`Grants`, a knowledge base no `Tags`/`Mounts`/`Repos` (same rule as lint). Compare `cfg.Encode()` with the file on disk; equal → return nil. Else `writeFile`, `repo.Add(Marker)`, `repo.Commit(CommitMessage("setup", summary, NewOperationID("setup", now)))`.

`links/repos.go`: move the functions with `git mv`-style edits (cut and paste, keep doc comments). `RemoteURL` uses `gitx.Repo{Dir: path}.RemoteURL()`. `Policy` is the rule `Page.Policy()` used; rewrite `Page.Policy()` to call it.

Template: append to `internal/vault/templates/project/.gitignore`:

```
# a project's repositories: each keeps its own history
/repos/
```

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/vault.go internal/vault/vault_test.go internal/vault/templates/project/.gitignore internal/links/repos.go internal/links/pages.go internal/links/links.go internal/links/links_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "vault, links: identity edits commit through UpdateConfig; repository helpers in one file"
```

---

### Task 4: Registering, editing, and mounting repositories over the identity file

**Files:**
- Create: `internal/vaults/identity.go`, `internal/vaults/identity_test.go`

**Interfaces:**
- Consumes: `registry.Entry` (Task 1), `home.Config` helpers (Task 2), `vault.UpdateConfig`, `links` repo helpers (Task 3).
- Produces:

```go
// PathFor is where a new vault of a kind goes by default.
func PathFor(vaultsDir string, kind vault.Kind, name string) string
// ResolvePath takes anything path-like as the vault's path and puts a bare name at PathFor.
func ResolvePath(arg, vaultsDir string, kind vault.Kind) (string, error)
// Register lists a vault outside the vaults directory in the config; it reports whether
// the config changed. A vault inside the vaults directory needs no entry.
func Register(h home.Home, cfg *home.Config, root string) (bool, error)
// Unregister removes a vault from the config. A vault inside the vaults directory cannot
// be forgotten: the scan finds it; the error says to move or delete the folder.
func Unregister(h home.Home, cfg *home.Config, root string) error

// Edit changes a vault's own facts. Nil means unchanged.
type Edit struct {
	Name   string
	Tags   *[]string // project
	Scope  *string   // knowledge base
	Access *string   // knowledge base: open or guarded
}
func EditIdentity(e registry.Entry, edit Edit, now time.Time) error

type NotRepoError struct{ Path string } // as today

// AddRepo mounts a folder on a project: under <project>/repos/ it needs no config entry;
// elsewhere the config records its path. A folder that is not a git repository is refused
// with NotRepoError unless initGit is set. It returns the identity entry and the path.
func AddRepo(h home.Home, cfg *home.Config, e registry.Entry, target string, initGit bool, now time.Time) (vault.Repo, string, error)
// CreateRepo makes a repository at <project>/repos/<name>, or at `at`, and mounts it.
func CreateRepo(h home.Home, cfg *home.Config, e registry.Entry, name, at string, now time.Time) (vault.Repo, string, error)
// CloneRepo clones url into <project>/repos/<name>, or at `at`, and mounts it with the remote recorded.
func CloneRepo(h home.Home, cfg *home.Config, e registry.Entry, url, at string, now time.Time) (vault.Repo, string, error)
// RemoveRepo drops a repository from the identity file and the config; the folder stays.
func RemoveRepo(h home.Home, cfg *home.Config, e registry.Entry, name string, now time.Time) error

type RepoEdit struct {
	Remote  *string // "" clears
	Changes *string // pr, commit, or "" for the default
	Path    string  // point the entry at another folder
}
func EditRepo(h home.Home, cfg *home.Config, e registry.Entry, name string, edit RepoEdit, now time.Time) (vault.Repo, error)
```

Rules: every function refuses a knowledge base entry with "a knowledge base has no repositories". A repository name is `links.CleanName(filepath.Base(path))` and must be unique in the project. A path inside the vault root but not under `repos/` is refused: "a repository goes under repos/ or outside the vault". `AddRepo` records `Remote: links.RemoteURL(path)` when the folder has one. `RemoveRepo` and `EditRepo` with a new path update `cfg.Repos` and save the config.

- [ ] **Step 1: Write the failing tests**

`identity_test.go`, with a helper that makes a project and a knowledge base under a temp vaults dir and returns `cfg`, `h`, and the two entries from `registry.Scan`. Test functions:

- `TestPathForAndResolvePath`: `PathFor(dir, vault.Knowledge, "ai-ml")` ends in `knowledge/ai-ml`; `ResolvePath("cs566", dir, vault.Project)` ends in `projects/cs566`; `ResolvePath("./x", …)` and `~/x` resolve as paths.
- `TestRegisterAndUnregister`: a vault under `VaultsDir` → `Register` returns false and `cfg.Vaults` stays empty; a vault elsewhere → true, saved (reload with `h.Load()`), second call false; `Unregister` of the outside vault removes it; of an inside vault → error containing "vaults directory".
- `TestEditIdentityByKind`: tags on a project → identity file has them and a `setup: edit tags` commit exists; scope on a project → error "knowledge base"; scope and access on the knowledge base → written; `Access: "sometimes"` → error; `Name` on either → written.
- `TestAddCreateCloneRemoveAndEditRepos`: `CreateRepo(e, "hw", "")` makes `<project>/repos/hw/.git`, identity gains `{Name: "hw", Changes: ""}`, no config entry; `AddRepo` on a plain folder outside → `NotRepoError`; with `initGit` → mounted, `cfg.Repos[id/name]` set and saved; `AddRepo` of the same folder again → error "already"; a folder inside the vault root not under `repos/` → error "under repos/"; `CloneRepo` from a local bare repository (make one with `git init --bare` in a temp dir) into `repos/` → remote recorded; `EditRepo` `Changes: "commit"` → written; `Changes: "later"` → error; `RemoveRepo("hw")` → gone from identity, folder still exists; on the knowledge base entry every function errors with "knowledge base".

Write each with concrete assertions on the identity file (`vault.Open(...).Config.Repos`) and the saved config (`h.Load()`), in the style of `internal/vaults/manage_test.go` (which Task 9 deletes).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vaults/ -run 'TestPathFor|TestRegister|TestEditIdentity|TestAddCreate'`
Expected: compile errors for every new name.

- [ ] **Step 3: Write `identity.go`**

Implement the interfaces above over `vault.UpdateConfig` (summaries: `edit name`, `edit tags`, `edit scope`, `edit access`, `add repository <name>`, `remove repository <name>`, `edit repository <name>`), `links.InitRepo`, `links.CreateRepo`, `links.Clone`, `links.RemoteURL`, `links.NameFromURL`, `home.Config.SetRepoPath`/`RepoPath`/`AddVault`/`RemoveVault`/`Inside`, and `h.Save`. Keep the old functions in `manage.go` untouched; Task 9 deletes them.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/vaults/identity.go internal/vaults/identity_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "vaults: register, edit, and mount repositories through the identity file and the config"
```

---

### Task 5: Refresh derives the registry

**Files:**
- Create: `internal/refresh/derive.go`
- Modify: `internal/refresh/refresh.go` (reuse; nothing deleted yet), `internal/refresh/refresh_test.go`

**Interfaces:**
- Consumes: `registry.Scan`, `registry.Entry`, `registry.State`, `registry.Write`, `lint.Run`, `tasks.Current`, `txn.Pending`, `links.Inspect`, the existing `Heat`, `NewestLogDate`, `CreatedDate`, `NewestWikiMtime`, `ActiveThreads`.
- Produces:

```go
// Derive observes one vault and returns what refresh records for it.
func Derive(e registry.Entry, today time.Time, generatedAt string, newDays int) *registry.State
// Registry scans, derives every readable entry, writes the registry file, and returns the entries.
func Registry(cfg *home.Config, stateDir string, today time.Time) ([]registry.Entry, *registry.Index, error)
// Signals lists what needs attention on one entry.
func Signals(e registry.Entry, today time.Time) []string
```

`Derive`: an entry with `Error` gets `State{VaultOK: false, VaultError: e.Error}`. Otherwise: `LastOperation` from the log, `LastTouched` from the log, the wiki mtime, and each resolved repo's `links.Inspect(links.Repo, path)` `Touched()`; `Heat(daysIdle, daysOld, newDays)` with `daysOld` from `e.Created`; `OpenThreads`; lint counts into `Pages` and `Unfinished`; for a project, `Tasks` from `taskSummary` (move it to `derive.go`, returning `*registry.TaskSummary`); `PendingRecovery` from `txn.Pending`; `RepoFacts[name]` for each repo with a path. `Registry`: `registry.Scan`, `Derive` each, `registry.Write(stateDir, entries, generatedAt)`; the state dir's old per-project JSON files are removed first (`os.RemoveAll(stateDir)` then write). `Signals`: `VaultError`; pending recovery ("an operation was interrupted; run `claude-atlas recover PATH`"); a mount with `Error`; a repo with `Error` or whose facts say `!OK`; blocked and stale tasks (the two sentences from today's `Signals`). No priority, state, or review-date signals: those fields are gone.

- [ ] **Step 1: Write the failing tests**

Add to `refresh_test.go`:

```go
func TestRegistryDerivesEveryEntry(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{VaultsDir: filepath.Join(root, "Vaults"), Heat: &home.HeatConfig{NewDays: 7}}
	kb := filepath.Join(cfg.VaultsDir, "knowledge", "ai-ml")
	p := filepath.Join(cfg.VaultsDir, "projects", "cs566")
	for path, opts := range map[string]vault.Options{kb: {Kind: vault.Knowledge, Name: "ai-ml"}, p: {Kind: vault.Project, Name: "cs566"}} {
		if _, err := vault.Init(path, opts, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(cfg.VaultsDir, "old", "wiki"), 0o755)
	os.WriteFile(filepath.Join(cfg.VaultsDir, "old", vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1"}`), 0o644)
	stateDir := filepath.Join(root, "state")
	entries, ix, err := Registry(cfg, stateDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || len(ix.Problems) != 1 {
		t.Fatalf("entries %d problems %+v", len(entries), ix.Problems)
	}
	for _, e := range entries {
		switch e.Name {
		case "ai-ml", "cs566":
			if e.State == nil || !e.State.VaultOK || e.State.Heat != "new" || e.State.Pages == nil || *e.State.Pages < 4 {
				t.Errorf("%s state %+v", e.Name, e.State)
			}
			if e.Kind == vault.Project && e.State.Tasks == nil {
				t.Errorf("project has a task summary: %+v", e.State)
			}
			if e.Kind == vault.Knowledge && e.State.Tasks != nil {
				t.Errorf("knowledge base has no tasks: %+v", e.State)
			}
		default:
			if e.State == nil || e.State.VaultOK || !strings.Contains(e.State.VaultError, "v1") {
				t.Errorf("v1 entry %+v", e)
			}
		}
	}
	read, _, err := registry.Read(stateDir)
	if err != nil || len(read) != 3 {
		t.Fatalf("registry file %v %d", err, len(read))
	}
}

func TestSignalsOverAnEntry(t *testing.T) {
	e := registry.Entry{Name: "p", Kind: vault.Project, Path: "/v/p",
		Mounts: []registry.Mount{{Name: "gone", Error: "no knowledge base with id x"}},
		Repos:  []registry.Repo{{Name: "lost", Error: "no folder; link it with claude-atlas link"}},
		State:  &registry.State{VaultOK: true, PendingRecovery: true, Tasks: &registry.TaskSummary{Open: []registry.TaskLine{{Title: "A", Status: "blocked"}, {Title: "B", Status: "active", Stale: true}}}},
	}
	got := strings.Join(Signals(e, time.Now()), "\n")
	for _, want := range []string{"interrupted", "gone", "lost", "1 blocked task: A", "1 stale task"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if got := Signals(registry.Entry{Name: "k", Error: "v1 vault"}, time.Now()); len(got) != 1 || !strings.Contains(got[0], "v1 vault") {
		t.Fatalf("error entry %v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/refresh/ -run 'TestRegistryDerivesEveryEntry|TestSignalsOverAnEntry'`
Expected: compile errors `undefined: Registry`, and `Signals` has the wrong signature (the old `Signals(node, state, today)` exists; name the new one `Signals` and rename the old one `treeSignals` in this task, updating its callers in `refresh.go` and `cli.go`; Task 9 deletes it).

- [ ] **Step 3: Write `derive.go`**

As described. Keep `refresh.go`'s existing functions compiling.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/refresh/derive.go internal/refresh/refresh.go internal/refresh/refresh_test.go internal/cli/cli.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "refresh: derive the registry from a scan"
```

---

### Task 6: Discovery through the registry; the hook and the repos tool follow

**Files:**
- Modify: `internal/discover/discover.go`, `discover_test.go`
- Modify: `internal/hooks/hooks.go`, `hooks_test.go`
- Modify: `internal/mcpserver/server.go`, `server_test.go`

**Interfaces:**
- Produces:

```go
// Match is a project whose repository holds the directory.
type Match struct {
	Project registry.Entry
	Repo    registry.Repo
}
func Vault(h home.Home, dir string) (*Match, []Match, error)  // as today: one match, or candidates
func Repos(h home.Home, root string) ([]registry.Repo, error) // the repositories of the project at root
func Describe(candidates []Match) string
```

`Vault`: load the config (`ErrNoAtlas` → nothing); `registry.Scan`; for each project entry and each repo with a `Path`, when `dir` is inside it, a candidate with the longest path; one candidate wins. `Repos`: `ix.ByPath(root)`; nil for a knowledge base or an unknown root.

Hooks: `SessionStart`'s `via` sentence becomes "claude-atlas: this folder is the repository REPO of the project NAME, whose vault is at PATH. The atlas tools use that vault. Search the wiki (the wiki-query skill, or Grep under its wiki/) before answering from the code alone; keep a decision with the save skill." and the policy line stays, using `links.Policy(repo.Changes, repo.Remote)` and `links.PolicyText`. `Stop` unchanged.

Server: `RepoInfo` built from `registry.Repo` (`Name, Path, Remote, Changes: links.Policy(...), Policy: PolicyText, Branch, Dirty` via `links.Inspect`); `status.Repository` when the session's cwd matches; the `repos` tool over `discover.Repos`.

- [ ] **Step 1: Rewrite the tests**

`discover_test.go`: `TestVaultThroughARepository`: a temp vaults dir with a project (`vault.Init`), a repository created with `links.CreateRepo(filepath.Join(project, "repos", "code"), "code")` and mounted with `vaults.AddRepo` (or by writing the identity `Repos` entry with `vault.UpdateConfig` — either is fine), and a second project mounting a folder outside the vaults dir via `cfg.Repos`. Assert: a dir inside `repos/code/src` matches the first project with `Repo.Name == "code"`; a dir inside the outside folder matches the second; a dir under both (nest the outside folder inside the first's repo to force it) yields two candidates and no match; a temp dir matches nothing; a home with no config matches nothing.

`hooks_test.go` `TestSessionStartListsTasksAndFindsAVaultThroughTheAtlas`: rebuild the fixture the same way (no `vaults.Register`, `vaults.AddLink`, `links.Walk`, or `vaults.UpdateLink`); set the repo's remote through `vault.UpdateConfig` on the project (`Repos[0].Remote = "git@example.com:a/code.git"`); expect "the repository code of the project", the policy sentence for `pr`, and the task lines as before. The test near line 177 that mounts a folder inside the vault: make it `repos/inside`.

`server_test.go` `TestReposToolAndStatusInARepository`: the same fixture; expect the `repos` tool to list `code` with `changes: "pr"` and `status.repository.name == "code"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/discover/ ./internal/hooks/ ./internal/mcpserver/ 2>&1 | head`
Expected: compile errors on `Match.Project`/`Match.Repo` and the removed helpers.

- [ ] **Step 3: Rewrite `discover.go` and adapt the two consumers**

As described. Remove the `links.Page` and `tree` imports from all three files.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/discover/discover.go internal/discover/discover_test.go internal/hooks/hooks.go internal/hooks/hooks_test.go internal/mcpserver/server.go internal/mcpserver/server_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "discover: a session finds its project through the registry's repositories"
```

---

### Task 7: The CLI and the wizard run over the registry

**Files:**
- Modify: `internal/cli/cli.go`, `internal/cli/cli_test.go`
- Modify: `internal/wizard/wizard.go`
- Modify: `internal/home/home.go` (`Default(vaultsDir string)`; `AtlasVault` no longer set by anyone)

**Interfaces:**
- Consumes: `registry.Scan`, `registry.Index.Find`, `registry.Read`, `refresh.Registry`, `refresh.Signals`, `vaults.PathFor`, `vaults.ResolvePath`, `vaults.Register`, `vaults.Unregister`, `vaults.EditIdentity`, `vaults.AddRepo`, `vaults.CreateRepo`, `vaults.CloneRepo`, `vaults.RemoveRepo`, `vaults.EditRepo`, `discover`, `links.Policy`.
- Produces the v2 command table. The TUI keeps compiling: `e.hooks(cfg)` still builds the old `tui.Hooks`, with `Load` returning no projects when `cfg.TreeRoot() == ""` (Task 8 replaces it). `view` therefore shows an empty tree until Task 8; that is expected inside this phase.

The command table after this task:

| Command | Does |
|---|---|
| `setup [--vaults-dir D] [--first-vault NAME] [--plugin-source S] [--no-plugin]` | home, config v2, plugin, and a first project; no atlas vault; `--atlas-vault` removed |
| `new-project NAME\|PATH [--name N] [--tags a,b] [--mode M]` | create a project at `PathFor` or the path; register when outside `vaults_dir`; refresh |
| `new-knowledge NAME\|PATH [--name N] [--scope S] [--access open\|guarded] [--mode M]` | the same for a knowledge base |
| `adopt PATH [--as K] [--name N] [--mode M]` | `vault.Adopt`; register when outside `vaults_dir`; refresh. The interactive form (no PATH) asks path, kind, name, mode. |
| `view` | the TUI |
| `open-vault [NAME\|PATH]` | as today; with no argument, the current directory's vault |
| `open-claude NAME [--task ID] [--in DIR]` | as today over `Find` |
| `ingest NAME [PATH...]` | as today; refuses a knowledge base ("knowledge enters through a project") |
| `link NAME PATH\|URL [--init] [--at DIR] [--changes pr\|commit]` | `AddRepo` or `CloneRepo`; the `settleChanges` question stays |
| `new-repo NAME REPO [--at DIR]` | `CreateRepo` |
| `unlink NAME REPO` | `RemoveRepo` |
| `repos [NAME]` | one project's repositories, or every project's; replaces `links` |
| `edit-repo NAME REPO [--remote URL] [--changes P] [--path DIR]` | `EditRepo`; replaces `edit-link` |
| `list` | every entry: kind, heat, name, path; problems after |
| `show NAME` | identity, mounts, repositories, state, signals |
| `edit NAME [--name N] [--tags a,b] [--scope S] [--access A]` | `EditIdentity`; the old fields are gone |
| `remove NAME` | `Unregister` |
| `refresh` | `refresh.Registry`; prints one step per entry and the problems |
| `lint`, `stub`, `history`, `undo`, `recover`, `mode`, `plant`, `tasks`, `upgrade`, `config`, `apply`, `mcp`, `hook`, `info`, `doctor`, `version` | as today, resolving `NAME` through `Find` where they took a project |

Removed: `new-vault`, `relate`, `unrelate`, `edit-link`, `links`. `usage` text updated to this table.

Rules for the rewrite:
- `e.entry(cfg, arg) (registry.Entry, error)` replaces `e.project`: `registry.Scan(cfg)` then `Find`; an `ErrAmbiguous` error is returned as is; `ErrNotFound` says "no vault named %q; see `claude-atlas list`".
- `e.refreshAll(cfg)` calls `refresh.Registry(cfg, e.home.StateDir(), time.Now())` and prints nothing itself; callers print `refreshed N vaults`.
- `list` reads the registry file (`registry.Read`); when it does not exist, it runs `refresh.Registry` first. Line format: `  %-9s %-5s %-24s %s` for kind, heat (or `off`/`v1`), name, `home.Display(path)`; entries with `Error` print `  %-9s %-5s %-24s %s` with kind `?`, heat `v1` or `bad`, and the path; problems as `console.Fail` steps.
- `show`: rows Name, Kind, Id, Path, Mode, Created; Tags or Scope and Access; each mount (`name  effective  path or error`); each repository (`name  path  changes  remote`); then the derived rows as today (Vault check, Heat, Last touched, Idle, Last operation, Pages, Unfinished, Open threads, Tasks, Refreshed) from `State`, and one `Signal` row per `refresh.Signals` line.
- `doctor`: lists every entry with a status word `ok`, `recover`, `no git`, `v1`, `bad`; then unresolved mounts and repositories as `console.Fail` steps; `ok = false` for any of those.
- `info`: drop the atlas vault, overview, and tree rows; add `registry` (the state file path) and `vaults` (count, then one row per entry).
- `tasks` with no argument lists every project's open tasks from the scan.
- `ingest`: `capture` staging as today over the entry's vault.
- `open-vault` with no argument: the vault at or above the current directory (`vault.FindAbove`); "the atlas" no longer exists.
- Wizard: no atlas vault; `home.Default(vaultsDir)`; the first vault is `new-project` at `PathFor(vaultsDir, Project, name)` with the purpose sentence gone; the closing lines name `claude-atlas new-project`, `new-knowledge`, `open-claude`, `refresh`; `plan(c, "atlas vault", …)` and `pages.Write` removed; `refresh.Registry` at the end.

- [ ] **Step 1: Rewrite the CLI tests**

Replace the tree-based tests in `cli_test.go` with these (keep `setup`, `harness`, and every test that does not touch the tree: `TestNewVaultKindAndAdoptAs` becomes `TestNewProjectNewKnowledgeAndAdoptAs`, `TestCommandsNeedSetupFirst`, `TestTaskCommands`, `TestAV1VaultIsNamedByDoctorAndUpgrade`, `TestConfigNewDays` (drop its About.md assertion), `TestStubCommand`, `TestBareCommandOpensTheTreeOrExplains`):

- `setup(t)` runs `setup --no-plugin --vaults-dir V --first-vault welcome`; `TestSetupCreatesHomeAndFirstProject` asserts `V/projects/welcome/.claude-atlas.json` with `"kind": "project"`, `home/config.json` with `"schema": "claude-atlas.config.v2"` and no `atlas_vault`, `home/state/registry.json` naming `welcome`, and no `Atlas` directory beside the home. A second `setup --no-plugin` says `keep` for the vaults.
- `TestNewProjectNewKnowledgeAndAdoptAs`: `new-project cs566 --tags usc,fall` creates `V/projects/cs566` with tags; `new-knowledge ai-ml --scope "ML."` creates `V/knowledge/ai-ml` with the scope and `access: open`; `new-knowledge x --access sometimes` exits 2; `new-project` with a path outside `V` registers it (`config.json` lists it) and `list` shows it; `adopt OLD --as knowledge` on a plain dir outside `V` adopts and registers; `adopt` again `--as project` exits 1 with "does not change".
- `TestListShowEditRemove`: `list` shows `project   new   welcome`; `show welcome` shows `Kind`, `Id`, `Path`, `Heat`; `edit welcome --tags a,b` then `show` has `a, b`; `edit welcome --scope x` exits 1 with "knowledge base"; `remove welcome` exits 1 with "vaults directory"; `remove` of the registered outside vault removes it from the config and `list`.
- `TestRepoCommands` (from `TestLinkCommands`, `TestEditLinkCommand`, `TestLinkFromAURLAndChanges`): `link welcome DIR` on a plain folder exits 1 with "not a git repository"; with `--init` mounts it, `config.json` maps `<id>/<name>` to it, `repos welcome` lists it with `changes: commit`; linking it again exits 1 with "already"; `new-repo welcome paper` creates `V/projects/welcome/repos/paper/.git` and lists it with no config entry; `link welcome URL` (a local bare repository as the URL) clones into `repos/` and records the remote with `changes: pr`; `edit-repo welcome upstream --changes commit` changes it; `--changes later` exits 2; `unlink welcome paper` removes the entry and leaves the folder; `repos` with no name lists every project's repositories.
- `TestRefreshAndDoctorReportProblems`: a v1 vault placed under `V` makes `refresh` print a `Fail` step naming it, `list` show it as `v1`, and `doctor` exit 1 with `v1`; a project whose identity names a mount id nobody has makes `doctor` print an unresolved-mount step.
- `TestOpenVaultResolvesNamesAndPaths`: `resolveVault`'s replacement (a function over the index) maps a name to its path, a path to itself, and an unknown name to an error.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ 2>&1 | head -20`
Expected: failures on the removed flags and commands (`flag provided but not defined: -first-vault` is not one — that flag stays; `new-project` prints "unknown command").

- [ ] **Step 3: Rewrite the commands and the wizard**

Follow the table and rules above. Delete the functions that served the tree (`nodeFlags`, `finishVault`'s tree lines, `relate`, `editLink`, `allLinks`, `linkLabel`, `linkLine`, `relName`, `followCategory`, the category prompts in `newVaultInteractive`). Keep `newVaultInteractive` as `newProjectInteractive` calling the existing add screen until Task 8 replaces the screen (pass `Kind: Project`).

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass. The TUI tests still pass because the TUI code is unchanged in this task.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go internal/wizard/wizard.go internal/home/home.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "cli, wizard: every command runs over the registry; the atlas vault is no longer created"
```

---

### Task 8: The TUI runs over the registry

**Files:**
- Modify: every file in `internal/tui/` and its tests
- Modify: `internal/cli/cli.go` (`hooks`, `view`, `newProjectInteractive`, the adopt form)

**Interfaces:**
- Produces:

```go
// Item is one vault in the view.
type Item struct {
	Entry registry.Entry // State is read from Entry.State
}

type Hooks struct {
	Load       func() ([]registry.Entry, error)       // the registry file, refreshed when missing
	Create     func(AddVault) (string, error)        // makes or adopts a vault; returns its path
	Refresh    func() error
	Edit       func(registry.Entry, vaults.Edit) error
	Unregister func(registry.Entry) error
	StagePlan  func(registry.Entry, string) (*capture.StagePlan, error)
	Stage      func(registry.Entry, *capture.StagePlan) (*capture.StageResult, []string, error)
	Sources    func(registry.Entry) []string
	AddRepo    func(registry.Entry, string, bool) (vault.Repo, string, error)
	NewRepo    func(registry.Entry, string, string) (vault.Repo, string, error)
	CloneRepo  func(registry.Entry, string, string) (vault.Repo, string, error)
	RemoveRepo func(registry.Entry, string) error
	EditRepo   func(registry.Entry, string, vaults.RepoEdit) (vault.Repo, error)
	Tasks      func(registry.Entry) (tasks.Ledger, []string, error)
	Plant      func(registry.Entry, tasks.Plant) (txn.Planted, error)
	VaultsDir  string
}

type AddVault struct {
	Kind   vault.Kind
	Name   string
	Path   string
	Mode   string
	Tags   []string // project
	Scope  string   // knowledge base
	Adopt  bool
}
```

The view keeps its tree renderer: rows are built from `Entry.Rel()`, so the top level shows `projects` and `knowledge`, a project's first tag makes a folder under `projects`, and each vault is a box as today (name, heat mark, pages, idle, unfinished). Keys: `n` new project, `N` new knowledge base, `a` adopt, `Enter` detail, `o` Obsidian, `c` Claude Code, `i` ingest (projects), `t` tasks and `T` every project's tasks (projects), `l` repositories (projects), `e` edit, `R` refresh, `q`. `r` inside the editor unregisters (the hook returns the "vaults directory" error for a vault under it, and the editor shows it). The detail pane shows the identity rows and the state rows `show` prints. The add screen asks kind first (a toggle), then name, mode, tags or scope, and the path (default `PathFor`); the adopt screen asks path, kind, name, mode. The editor edits name and tags for a project, name, scope, and access for a knowledge base. The repositories screen lists `Entry.Repos` with facts from `Entry.State.RepoFacts`; its keys `n`, `a`, `e`, `u` map to `NewRepo`, `AddRepo`/`CloneRepo`, `EditRepo`, `RemoveRepo`. The tasks and ingest screens take an entry.

- [ ] **Step 1: Rewrite the TUI tests**

Replace `item(rel, name, heat)` in `view_test.go` with one that builds an `Item{Entry: registry.Entry{ID, Kind, Name, Path, Tags, State: &registry.State{...}}}`; `sample()` returns two projects (one tagged `usc`), one untagged project, and two knowledge bases. Keep every view test's intent: three-layer tree (`projects`, `usc`, boxes; `knowledge`, boxes), folding, detail, open, Claude hand-off, new and adopt, refresh, ingest. Add `TestNewKnowledgeKeyOpensTheAddScreenWithKindSet`. Rewrite `addvault_test.go` for the kind toggle, tags, scope, and `PathFor` defaults (drop the category tests). Rewrite `editor_test.go`: rename and tag a project, scope and access on a knowledge base, `Esc` warning, `r` unregister calls the hook, hooks required. Rewrite `linkscreen_test.go` over `Entry.Repos` and the new hooks. `taskscreen_test.go` and the ingest test take an entry.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ 2>&1 | head`
Expected: compile errors on `Item.Project`, `tree.*`, `links.Page`.

- [ ] **Step 3: Port the screens**

Work file by file: `view.go` (rows from `Rel`, detail from `Entry`), `addvault.go`, `editor.go`, `linkscreen.go`, `taskscreen.go`, `ingest.go`; delete `Categories`. Then rewire `cli.go`'s `hooks` to the `vaults` identity functions and `registry`, `view` to build items from `registry.Read` (refreshing when missing), and the interactive `new-project`, `new-knowledge`, and `adopt` forms to `AddVault`.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui internal/cli/cli.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "tui: the view, the forms, and the screens run over the registry"
```

---

### Task 9: Delete what the registry replaced

**Files:**
- Delete: `internal/tree/`, `internal/pages/`, `internal/links/pages.go`, `internal/refresh/graph.go`
- Modify: `internal/refresh/refresh.go` (delete `Tree`, `Run`, `Render`, `Result`, `Row`, `LinkRow`, `LinksState`, `WriteCategories`, `treeSignals`, `LinkSignals`, `related`, `categoryCell`, `heatLabel`, `taskCell`, `openTasks`, `TaskLink`, `linkCell`, `linkLabel`, `inspectLinks`, `inspectPages`; keep the date and heat helpers, `LinkSummary`, `NowUTC`, `PlainText`), `internal/refresh/refresh_test.go`
- Modify: `internal/vaults/manage.go` (delete everything but `NotRepoError` if `identity.go` did not already define it; delete `manage_test.go`), `internal/vaults/vaults.go` (delete `DefaultPath`, `ResolveNewPath`, `Register`, `RegisterOptions`; keep `Create`, `CheckNewPath`, `ErrCancelled`), `vaults_test.go`
- Modify: `internal/home/home.go` (delete `AtlasVault`, `DefaultAtlas`, `TreeRoot`), `home_test.go`
- Modify: `internal/links/links.go` (delete `Materials`, `DetectKind`, `inspectMaterials`, the materials facts)
- Modify: `internal/obsidian/obsidian.go` if it references the atlas vault

- [ ] **Step 1: Delete and fix the build**

Run `go build ./... 2>&1` after each deletion and fix every reference. `grep -rn "tree\.\|pages\.\|AtlasVault\|TreeRoot\|links.Page\|links.Materials" --include='*.go' internal/ cmd/` must return nothing.

- [ ] **Step 2: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass. `refresh_test.go` keeps `TestHeat`, `TestCreatedDateComesFromTheIndexPage`, `TestNewestLogDate`, `TestActiveThreadsJoinsContinuationLines`, `TestPlainTextStripsWikilinks`, `TestDeriveTakesLaterOfLogAndMtime` (over an entry), `TestRegistryDerivesEveryEntry`, `TestSignalsOverAnEntry`; the others go.

- [ ] **Step 3: Commit**

```bash
git add -A internal/tree internal/pages internal/links internal/refresh internal/vaults internal/home internal/obsidian
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "atlas: delete the tree, the link pages, the overview rendering, and the atlas vault config"
```

---

### Task 10: Docs for the new atlas

**Files:**
- Modify: `CLAUDE.md`, `docs/usage.md`, `docs/v2-design.md`, `README.md`

- [ ] **Step 1: CLAUDE.md**

Rewrite the opening paragraph's last sentence ("The atlas side reports on every vault from one Obsidian page") to "The atlas side lists every vault from a scan of the vaults directory and shows them in a terminal view." In "Sources of truth", the v2 row reads "(phases 1–2 built: kinds, the registry)". Replace "Three rules for the atlas" with the v2 rules 5–7 from `docs/v2-design.md` ("A knowledge base never learns who mounts it", "A project never links another project", "Ids travel; paths stay") and the sentence "`~/.claude-atlas/state/registry.json` is derived and `refresh` rebuilds it in full." In "Layout", replace the `internal/tree`, `internal/refresh`, `internal/pages`, `internal/vaults`, `internal/links`, `internal/tui` lines with what they hold now, add `internal/registry/`, and fix the paragraph about `~/.claude-atlas/` and `~/Documents` (no atlas vault; `<vaults dir>/knowledge/<name>` and `<vaults dir>/projects/<name>`). In "Constraints", delete the two bullets about `tree.UpdateFrontmatter` and `[[dir/name|name]]` links; add "A vault's own facts change only through `vault.UpdateConfig`, which commits the identity file as a `setup` operation." Delete the "Obsidian facts" bullets about properties, the graph view filter, and `graph.json`; keep the `obsidian://` bullet. Delete the "iCloud Drive facts" section (the category move is gone).

- [ ] **Step 2: usage.md**

Rewrite "Two layers" (the key table), "Create a vault" (`new-project`, `new-knowledge`), "Edit a project", "The atlas", "Mount repositories", "Relate projects" (delete), "The graph" (delete), "Edit the tree by hand" (delete), "Adopt an existing vault", "Setup and health", and "Configuration" (`vaults`, `repos`; no `atlas_vault`) to the command table in Task 7. Keep the in-vault sections.

- [ ] **Step 3: v2-design.md and README**

In `v2-design.md`, the status line: "Phases 1 and 2 are built." In "The atlas", correct anything the implementation settled differently (the five-level scan, `projects/<tag>/<name>` in the view, `PathFor`). In `README.md`, replace mentions of the atlas vault and `Overview.md` with the registry and `claude-atlas view`; keep it short.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/usage.md docs/v2-design.md README.md
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "docs: the registry replaces the atlas vault"
```

---

## Execution order

2, 3, 1, 4, 5, 6, 7, 8, 9, 10. Task 1's tests use `vault.UpdateConfig` from Task 3 and the config fields from Task 2.

## Self-review

**Spec coverage (phase 2):** config v2 (Task 2, finished in Task 9), the scan and `registry.json` (Task 1), `refresh` (Task 5), discovery through the scan (Task 6), deletion of `tree`, `pages`, `links/pages.go`, the atlas-vault rendering, and the `relate` commands (Tasks 7 and 9), `new-knowledge` and `new-project` (Task 7), the TUI over the registry (Task 8; the spec puts `view` in phase 4, but the TUI must compile and work after `tree` goes, so its port comes here and phase 4 adds the mount and grant keys). The stubs counts move to the registry's `Unfinished` (Task 1 and 5), as the phase note in `v2-design.md` says.

**Placeholders:** Tasks 7 and 8 describe behavior and tests rather than showing every line of a 2,000-line rewrite; their implementers run on the most capable model and read the existing files. Every other task shows its code or its exact rule.

**Type consistency:** `registry.Entry`, `registry.Repo`, `registry.Mount`, `registry.State` are defined in Task 1 and used with the same field names in Tasks 4–8. `vaults.Edit` and `vaults.RepoEdit` (Task 4) are the types the TUI hooks carry (Task 8). `vault.UpdateConfig(root, summary, now, change)` has the same argument order everywhere. `discover.Match{Project, Repo}` (Task 6) is what the hook and the server read.
