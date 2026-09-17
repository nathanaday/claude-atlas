# Atlas tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every configuration task the CLI and the view offer becomes possible from a Claude Code session: seven MCP tools bound to the same functions the view uses, and five skills that carry the guided flows.

**Architecture:** `tui.Hooks` moves to `internal/actions` as `actions.Atlas` with one constructor, `Bind`, that the CLI, the view, and the MCP server all use. Seven tools in a new file `internal/mcpserver/atlas.go` call the struct; each write tool is a noun with an `action` field. Skills `atlas`, `atlas-project`, `atlas-knowledge`, `atlas-mount`, `atlas-repo` carry the questions and the confirmation; no `plan`/`apply`.

**Tech Stack:** Go 1.24, the official MCP `go-sdk` v1.4.0 (schemas inferred from struct tags by `jsonschema-go`), Bubble Tea. Tests: `go test`, in-process MCP over the in-memory transport.

**Spec:** `docs/superpowers/specs/2026-09-16-atlas-tools-design.md`

## Global Constraints

- Dependencies stay `gopkg.in/yaml.v3`, `go-sdk` v1.4.0, Bubble Tea, Bubbles, Lip Gloss. Nothing new in `go.mod`.
- Go 1.24. Do not pull `golang.org/x/*`.
- Commits use the identity `nathanaday <nraday1221@gmail.com>` and carry no `Co-Authored-By` line. Every commit command below passes `-c user.name=nathanaday -c user.email=nraday1221@gmail.com`.
- Commit subjects follow the log's form: `area: what changed`, lowercase, no period (`vaults: a rename takes the folder with it`).
- Tests never touch a real `~/.claude-atlas`; every test builds its atlas under `t.TempDir()` and skips when `gitx.Available()` is false.
- Every tool description is one line. A write tool is a noun; its `action` field is the verb; every argument's `jsonschema` doc names the actions that read it.
- Prose in skills and docs follows the user's writing guide: short sentences, active voice, no "flag", "genuine", "honest", "shape", "load bearing", "judgement call".
- An action lives once, in `internal/vaults`, `internal/refresh`, `internal/vault`, `internal/txn`, or `internal/capture`. `actions.Bind` is the one place the struct is built. `tui` and `mcpserver` import `actions`; neither imports the other; `actions` imports neither.
- `make test` and `go vet ./...` pass at the end of every task. Run `gofmt -l internal/` and expect no output.

---

## File map

| File | Responsibility |
|---|---|
| `docs/superpowers/specs/2026-09-16-atlas-tools-design.md` | amended in Task 1 |
| `CLAUDE.md` | the pending "Tool, skill, or hook" section (Task 1); the three-layers rule, layout row, constraint, version line (Task 11) |
| `internal/refresh/derive.go` | `All`, `Entries`, `Derived` (Task 2) |
| `internal/capture/stage.go` | `SourcesFor` (Task 2) |
| `internal/actions/actions.go` | `AddVault`, `Atlas`, `Bind` (Task 3) |
| `internal/actions/create.go` | `createOrAdopt`, `recordFacts`, `mountChoice`, `memberChoice`, `plantTask` (Task 3) |
| `internal/actions/actions_test.go` | `Bind` sets every field; create, edit, forget over a temp atlas (Task 3) |
| `internal/tui/*.go` | `Hooks` → `actions.Atlas`, `AddVault` → `actions.AddVault`; `Refresh` and `StagePlan` call sites (Task 3) |
| `internal/cli/cli.go`, `cli_test.go` | the moved helpers removed; `actions.Bind` (Task 3) |
| `internal/mcpserver/atlas.go` | `bind`, name resolution, the seven tools (Tasks 4–9) |
| `internal/mcpserver/atlas_test.go` | one test function per tool (Tasks 4–9) |
| `internal/mcpserver/server.go` | seven `mcp.AddTool` lines (Tasks 4–9) |
| `skills/atlas/SKILL.md`, `skills/atlas-project/SKILL.md`, `skills/atlas-knowledge/SKILL.md`, `skills/atlas-mount/SKILL.md`, `skills/atlas-repo/SKILL.md` | new (Task 10) |
| `skills/wiki/SKILL.md`, `skills/wiki-ingest/SKILL.md` | hand-offs (Task 10) |
| `docs/usage.md`, `docs/core-design.md`, `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json` | docs and 1.3.0 (Task 11) |

---

### Task 1: Amend the spec and commit the pending CLAUDE.md section

The spec was written before three facts settled: the server binds per call, `Refresh` and `StagePlan` change signature, and `AddVault` now carries `MemberIDs` (commit `722149b`). `CLAUDE.md` has an uncommitted "Tool, skill, or hook" section the user approved.

**Files:**
- Modify: `docs/superpowers/specs/2026-09-16-atlas-tools-design.md`
- Commit as-is: `CLAUDE.md`

- [ ] **Step 1: Apply the four spec amendments**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && python3 - <<'EOF'
import io
p='docs/superpowers/specs/2026-09-16-atlas-tools-design.md'
s=io.open(p,encoding='utf-8').read()
def rep(old,new):
    global s
    assert old in s, old[:60]
    s=s.replace(old,new,1)
rep('''The CLI calls `Bind` and keeps none of it. The `mcp`
subcommand runs inside the CLI, so it hands the same value to
`mcpserver.Options.Atlas`; the server's tests, which cannot import `cli`,
call `Bind` themselves. `tui` and `mcpserver` import `actions`; neither
imports the other, and `actions` imports neither.''',
'''The CLI calls `Bind` and keeps none of it. The server
calls `Bind` itself, once per tool call with a config it has just loaded,
because the CLI in another process may change `config.json` between two
calls. `tui` and `mcpserver` import `actions`; neither imports the other,
and `actions` imports neither.''')
rep('''The struct gains one field: `Scan func() (*registry.Index, error)`, a fresh
scan with no write. Every write tool resolves its names through it, because
every command that acts on a vault scans afresh and `registry.json` is for
display only.''',
'''The struct gains one field, `Scan func() (*registry.Index, error)`: a fresh
scan with every entry's state derived and nothing written. Every tool
resolves its names through it, because every command that acts on a vault
scans afresh and `registry.json` is for display only. Two signatures change:
`Refresh` returns the index and each project's changes, and `StagePlan`
takes a list of sources. `AddVault` gains `Access` and `InRepo`, so one
`Create` covers every way the CLI makes a vault.''')
rep('''create: kind, name, path?, mode?, tags?, scope?, access?, in_repo?, mount?''',
'''create: kind, name, path?, mode?, tags?, scope?, access?, in_repo?, mount?, members?''')
rep('''`mount` names a knowledge base a
  new project mounts with write, as the view's add screen does.''',
'''`mount` names a knowledge base a
  new project mounts with write, and `members` the knowledge bases a new
  cluster gathers, as the view's add screen does.''')
rep('''Two vaults that share a name are refused with
both ids in the message; the skill passes the id.''',
'''Two vaults that share a name are refused with
both paths in the message (`registry.Index.Find`); the skill passes the path
or the id.''')
rep('''and for a cluster, the
members, through `cluster add` after creation. One line, yes, `vault create`,
then `cluster` as chosen.''',
'''and for a cluster, the
members, passed as `members` on `vault create`. One line, yes, `vault
create`.''')
io.open(p,'w',encoding='utf-8').write(s)
EOF
```

- [ ] **Step 2: Commit both**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com add CLAUDE.md docs/superpowers/specs/2026-09-16-atlas-tools-design.md && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -q -m "docs: tool, skill, or hook; the atlas tools spec binds per call" && git log --oneline -1
```

---

### Task 2: `refresh.All`, `refresh.Entries`, `refresh.Derived`, and `capture.SourcesFor`

Four helpers the CLI keeps as private methods today, moved next to the functions they wrap so `actions` and `mcpserver` can reach them.

**Files:**
- Modify: `internal/refresh/derive.go`
- Create: `internal/refresh/all_test.go`
- Modify: `internal/capture/stage.go`
- Modify: `internal/capture/stage_test.go`

**Interfaces:**
- Produces:
  - `func All(h home.Home, cfg *home.Config, today time.Time) ([]registry.Entry, *registry.Index, []ProjectChange, error)` — `Registry` with `h.StateDir()` and `ensure=true`.
  - `func Entries(h home.Home, cfg *home.Config, today time.Time) ([]registry.Entry, error)` — the registry file, written first when absent.
  - `func Derived(cfg *home.Config, today time.Time) (*registry.Index, error)` — a scan with every entry's `State`, nothing written.
  - `func SourcesFor(v *vault.Vault, given []string) ([]string, error)` in `capture` — the given paths expanded, or the folders the vault staged from before; an error when neither exists.

- [ ] **Step 1: Write the failing tests**

`internal/refresh/all_test.go`:

```go
package refresh

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// oneVault builds an atlas home whose vaults directory holds one project.
func oneVault(t *testing.T) (home.Home, *home.Config) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default(filepath.Join(root, "Vaults"))
	if err := os.MkdirAll(h.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	if _, err := vault.Init(vaults.PathFor(cfg.VaultsDir, vault.Project, "p"), vault.Options{Kind: vault.Project, Name: "p"}, now); err != nil {
		t.Fatal(err)
	}
	return h, cfg
}

func TestDerivedWritesNothing(t *testing.T) {
	h, cfg := oneVault(t)
	ix, err := Derived(cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Entries) != 1 || ix.Entries[0].State == nil {
		t.Fatalf("one derived entry expected, got %+v", ix.Entries)
	}
	if _, err := os.Stat(registry.File(h.StateDir())); !os.IsNotExist(err) {
		t.Fatalf("Derived must not write the registry: %v", err)
	}
}

func TestEntriesWritesTheRegistryOnceThenReadsIt(t *testing.T) {
	h, cfg := oneVault(t)
	entries, err := Entries(h, cfg, time.Now())
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries: %v %v", entries, err)
	}
	if _, err := os.Stat(registry.File(h.StateDir())); err != nil {
		t.Fatalf("Entries writes the registry when none exists: %v", err)
	}
	again, err := Entries(h, cfg, time.Now())
	if err != nil || len(again) != 1 {
		t.Fatalf("second read: %v %v", again, err)
	}
}

func TestAllReturnsTheIndexAndTheChanges(t *testing.T) {
	h, cfg := oneVault(t)
	entries, ix, changes, err := All(h, cfg, time.Now())
	if err != nil || len(entries) != 1 || ix == nil || len(ix.Entries) != 1 {
		t.Fatalf("all: %d entries, ix %v, err %v", len(entries), ix, err)
	}
	if len(changes) != 0 {
		t.Fatalf("a fresh project needs no change, got %+v", changes)
	}
}
```

Append to `internal/capture/stage_test.go`:

```go
func TestSourcesForExpandsGivenPathsAndFallsBackToRememberedFolders(t *testing.T) {
	v := newVault(t)
	got, err := SourcesFor(v, []string{"~/Desktop/notes"})
	if err != nil || len(got) != 1 || strings.HasPrefix(got[0], "~") {
		t.Fatalf("given paths are expanded: %v %v", got, err)
	}
	if _, err := SourcesFor(v, nil); err == nil || !strings.Contains(err.Error(), v.Name()) {
		t.Fatalf("with nothing remembered the error names the vault: %v", err)
	}
	src := filepath.Join(t.TempDir(), "src")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.md"), []byte("a"), 0o644)
	plan, err := PlanStage(v, []string{src}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyStage(v, plan, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err = SourcesFor(v, nil)
	if err != nil || len(got) != 1 || got[0] != src {
		t.Fatalf("remembered folders are the fallback: %v %v", got, err)
	}
}
```

`newVault` is the helper `capture_test.go` already defines in this package. Add `"strings"`, `"os"`, `"path/filepath"`, and `"time"` to `stage_test.go`'s imports if they are missing.

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/refresh/ ./internal/capture/ 2>&1 | head -20`
Expected: build errors `undefined: Derived`, `undefined: Entries`, `undefined: All`, `undefined: SourcesFor`.

- [ ] **Step 3: Implement**

Append to `internal/refresh/derive.go`:

```go
// All rebuilds the registry from a scan and brings every project's local state up to
// date: the CLI runs it after every change.
func All(h home.Home, cfg *home.Config, today time.Time) ([]registry.Entry, *registry.Index, []ProjectChange, error) {
	return Registry(h, cfg, h.StateDir(), today, true)
}

// Entries reads the registry the last refresh wrote, writing one first when none exists.
func Entries(h home.Home, cfg *home.Config, today time.Time) ([]registry.Entry, error) {
	entries, _, err := registry.Read(h.StateDir())
	if errors.Is(err, os.ErrNotExist) {
		entries, _, _, err = All(h, cfg, today)
	}
	return entries, err
}

// Derived scans and derives every entry's state, and writes nothing: what a tool reads
// when it wants the atlas as it is now.
func Derived(cfg *home.Config, today time.Time) (*registry.Index, error) {
	ix, err := registry.Scan(cfg)
	if err != nil {
		return nil, err
	}
	generatedAt := NowUTC()
	newDays := cfg.NewDays()
	for i := range ix.Entries {
		ix.Entries[i].State = Derive(ix.Entries[i], today, generatedAt, newDays)
	}
	return ix, nil
}
```

Add `"errors"` to `derive.go`'s imports.

Append to `internal/capture/stage.go`:

```go
// SourcesFor is what a staging reads: the given paths, expanded, or the folders the vault
// staged from before. With neither it says so.
func SourcesFor(v *vault.Vault, given []string) ([]string, error) {
	if len(given) > 0 {
		out := make([]string, 0, len(given))
		for _, g := range given {
			if g = strings.TrimSpace(g); g != "" {
				out = append(out, home.Expand(g))
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	sources := Sources(v)
	if len(sources) == 0 {
		return nil, fmt.Errorf("name a file or folder to stage; %s has not staged from a folder yet", v.Name())
	}
	return sources, nil
}
```

Add `"github.com/nathanaday/claude-atlas/internal/home"` to `stage.go`'s imports.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/refresh/ ./internal/capture/`
Expected: `ok` for both.

- [ ] **Step 5: Commit**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com add internal/refresh internal/capture && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -q -m "refresh, capture: the helpers the CLI kept to itself, for the view and the tools" && git log --oneline -1
```

---

### Task 3: `internal/actions`: the struct, `AddVault`, and `Bind`

Move `tui.Hooks` and `tui.AddVault` out of `tui`, and `cli.hooks` with its helpers out of `cli`, into one package. The view and the CLI change only their imports and two call sites.

**Files:**
- Create: `internal/actions/actions.go`, `internal/actions/create.go`, `internal/actions/actions_test.go`
- Modify: `internal/tui/editor.go` (remove `type Hooks`), `internal/tui/addvault.go` (remove `type AddVault`), `internal/tui/view.go:528`, `internal/tui/ingest.go:69`, every `internal/tui/*.go` that names `Hooks` or `AddVault`
- Modify: `internal/cli/cli.go` (remove `createOrAdopt`, `memberChoice`, `mountChoice`, `recordFacts`, `hooks`, `plantTask`, `ingestSources`, `planStage`, `stage`; rewrite `refreshAll`, `registryEntries`), `internal/cli/cli_test.go`

**Interfaces:**
- Consumes: `refresh.All`, `refresh.Entries`, `refresh.Derived`, `capture.SourcesFor` (Task 2).
- Produces:

```go
package actions

type AddVault struct {
	Kind      vault.Kind
	Name      string
	Path      string
	Mode      string
	Tags      []string
	Scope     string
	Access    string   // knowledge base: open or guarded; "" keeps the template's open
	InRepo    string   // project: a repository's top level; the vault goes to REPO/atlas and Path is ignored
	Adopt     bool
	MountID   string
	Cluster   bool
	MemberIDs []string
}

type Atlas struct {
	Load         func() ([]registry.Entry, error)
	Scan         func() (*registry.Index, error)
	Create       func(AddVault) (string, error)
	Refresh      func() (*registry.Index, []refresh.ProjectChange, error)
	Edit         func(registry.Entry, vaults.Edit) (string, error)
	Unregister   func(registry.Entry) error
	StagePlan    func(registry.Entry, []string) (*capture.StagePlan, error)
	Stage        func(registry.Entry, *capture.StagePlan) (*capture.StageResult, []string, error)
	Sources      func(registry.Entry) []string
	AddRepo      func(registry.Entry, string, bool) (vault.Repo, string, error)
	NewRepo      func(registry.Entry, string, string) (vault.Repo, string, error)
	CloneRepo    func(registry.Entry, string, string) (vault.Repo, string, error)
	RemoveRepo   func(registry.Entry, string) error
	EditRepo     func(registry.Entry, string, vaults.RepoEdit) (vault.Repo, error)
	Mount        func(project, kb registry.Entry, access, name string) (vault.Mount, error)
	Unmount      func(project registry.Entry, target string) error
	EditMount    func(project registry.Entry, target, access string) (vault.Mount, error)
	Grant        func(kb, project registry.Entry, access string) error
	Revoke       func(kb registry.Entry, projectID string) error
	AddMember    func(cluster, kb registry.Entry) error
	RemoveMember func(cluster registry.Entry, target string) error
	Tasks        func(registry.Entry) (tasks.Ledger, []string, error)
	Plant        func(registry.Entry, tasks.Plant) (txn.Planted, error)
	VaultsDir    string
}

func Bind(h home.Home, cfg *home.Config, c *console.Console) Atlas
```

- [ ] **Step 1: Write the failing test**

`internal/actions/actions_test.go`:

```go
package actions

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

var now = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

// atlas builds an atlas home whose vaults directory holds one knowledge base, kb.
func atlas(t *testing.T) (home.Home, *home.Config, registry.Entry) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default(filepath.Join(root, "Vaults"))
	if err := os.MkdirAll(h.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	kbPath := vaults.PathFor(cfg.VaultsDir, vault.Knowledge, "kb")
	if _, err := vault.Init(kbPath, vault.Options{Kind: vault.Knowledge, Name: "kb"}, now); err != nil {
		t.Fatal(err)
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	kb := ix.ByPath(kbPath)
	if kb == nil {
		t.Fatal("the scan did not find kb")
	}
	return h, cfg, *kb
}

func TestBindSetsEveryField(t *testing.T) {
	a := Bind(home.Home{Root: t.TempDir()}, &home.Config{}, nil)
	v := reflect.ValueOf(a)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() == reflect.Func && f.IsNil() {
			t.Errorf("Bind leaves %s nil", v.Type().Field(i).Name)
		}
	}
}

func TestCreateRecordsFactsMountsAndGathers(t *testing.T) {
	h, cfg, kb := atlas(t)
	a := Bind(h, cfg, nil)
	// A project with tags that mounts kb at creation.
	path, err := a.Create(AddVault{Kind: vault.Project, Name: "p", Path: vaults.PathFor(cfg.VaultsDir, vault.Project, "p"), Mode: "generic", Tags: []string{"usc"}, MountID: kb.ID})
	if err != nil {
		t.Fatal(err)
	}
	ix, err := a.Scan()
	if err != nil {
		t.Fatal(err)
	}
	p := ix.ByPath(path)
	if p == nil || p.Kind != vault.Project || len(p.Tags) != 1 || p.Tags[0] != "usc" || len(p.Mounts) != 1 || p.Mounts[0].Name != "kb" || p.State == nil {
		t.Fatalf("project: %+v", p)
	}
	// A guarded knowledge base with a scope.
	kb2, err := a.Create(AddVault{Kind: vault.Knowledge, Name: "papers", Path: vaults.PathFor(cfg.VaultsDir, vault.Knowledge, "papers"), Mode: "generic", Scope: "Papers.", Access: vault.AccessGuarded})
	if err != nil {
		t.Fatal(err)
	}
	ix, _ = a.Scan()
	if e := ix.ByPath(kb2); e == nil || e.Scope != "Papers." || e.Access != vault.AccessGuarded {
		t.Fatalf("knowledge base: %+v", e)
	}
	// A cluster that gathers kb.
	cl, err := a.Create(AddVault{Kind: vault.Knowledge, Name: "domain", Path: vaults.PathFor(cfg.VaultsDir, vault.Knowledge, "domain"), Mode: "generic", Cluster: true, MemberIDs: []string{kb.ID}})
	if err != nil {
		t.Fatal(err)
	}
	ix, _ = a.Scan()
	if e := ix.ByPath(cl); e == nil || len(e.Members) != 1 || e.Members[0].Name != "kb" {
		t.Fatalf("cluster: %+v", e)
	}
}

func TestCreateInARepositoryAndEditAndForget(t *testing.T) {
	h, cfg, _ := atlas(t)
	a := Bind(h, cfg, nil)
	repo := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(repo, 0o755)
	if err := (gitx.Repo{Dir: repo}).Init(); err != nil {
		t.Fatal(err)
	}
	path, err := a.Create(AddVault{Kind: vault.Project, Name: "code", InRepo: repo, Mode: "generic"})
	if err != nil || path != filepath.Join(repo, vault.InRepoDir) {
		t.Fatalf("in repo: %q %v", path, err)
	}
	ix, _ := a.Scan()
	e := ix.ByPath(path)
	if e == nil || e.Host != repo {
		t.Fatalf("the host repository is the project's first repository: %+v", e)
	}
	moved, err := a.Edit(*e, vaults.Edit{Name: "Code"})
	if err != nil || moved != path {
		t.Fatalf("a project at REPO/atlas keeps its folder: %q %v", moved, err)
	}
	// A vault outside the vaults directory is registered and can be forgotten; one inside cannot.
	if err := a.Unregister(*e); err != nil {
		t.Fatalf("forget outside: %v", err)
	}
	inside, err := a.Create(AddVault{Kind: vault.Project, Name: "q", Path: vaults.PathFor(cfg.VaultsDir, vault.Project, "q"), Mode: "generic"})
	if err != nil {
		t.Fatal(err)
	}
	ix, _ = a.Scan()
	if err := a.Unregister(*ix.ByPath(inside)); err == nil || !strings.Contains(err.Error(), "vaults directory") {
		t.Fatalf("forget inside: %v", err)
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./internal/actions/ 2>&1 | head -5`
Expected: `no Go files` or `undefined: Bind`.

- [ ] **Step 3: Create the package**

`internal/actions/actions.go`:

```go
// Package actions is every change to the atlas the view and the tools can make, as one
// struct of functions over the packages that own them. Bind builds it in one place; the
// CLI, the view, and the MCP server never bind a function a second time.
package actions

import (
	"time"

	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// AddVault is what a caller chose for a new or adopted vault.
type AddVault struct {
	Kind  vault.Kind
	Name  string // display name as typed, or the folder's name when adopting
	Path  string // where the vault will be created, or the vault being adopted
	Mode  string // generic or lyt
	Tags  []string
	Scope string
	// Access is a knowledge base's access, open or guarded; "" keeps the template's open.
	Access string
	// InRepo is a repository's top level; the project goes to REPO/atlas and Path is ignored.
	InRepo string
	Adopt  bool // Path exists already and is adopted rather than created
	// MountID is the knowledge base a new project mounts once it exists; "" mounts none.
	// The mount asks for write, and the mounts screen changes that.
	MountID string
	// Cluster is set when the user asked for a knowledge base that gathers others, and
	// MemberIDs are the knowledge bases it gathers. The vault is an ordinary knowledge
	// base until it has a member.
	Cluster   bool
	MemberIDs []string
}

// Atlas is every action the view and the tools reach. Each field is one function from
// the package that owns the action, bound to the atlas home and its config.
type Atlas struct {
	// Load reads the registry, refreshing it first when no refresh has run yet. Scan
	// reads every vault afresh, with its state derived, and writes nothing.
	Load func() ([]registry.Entry, error)
	Scan func() (*registry.Index, error)
	// Create makes or adopts a vault and registers it; it returns the vault's path.
	Create func(AddVault) (string, error)
	// Refresh reads every vault again, rewrites the registry, and repairs each project's
	// local state; it returns the index and what each project needed.
	Refresh func() (*registry.Index, []refresh.ProjectChange, error)
	// Edit changes a vault's identity and returns the path it sits at afterwards; a
	// rename moves the folder, so that path may not be the one it was given. Unregister
	// forgets a vault the config names; the folder stays, and a vault inside the vaults
	// directory cannot be forgotten.
	Edit       func(registry.Entry, vaults.Edit) (string, error)
	Unregister func(registry.Entry) error
	// StagePlan says which files under the sources are new to a project's vault; no
	// sources means the folders it staged from before. Stage copies a plan's files into
	// the inbox and reports the folders the vault now remembers. Sources lists them.
	StagePlan func(registry.Entry, []string) (*capture.StagePlan, error)
	Stage     func(registry.Entry, *capture.StagePlan) (*capture.StageResult, []string, error)
	Sources   func(registry.Entry) []string
	// The repository calls: mount a folder, initializing git there when asked; create
	// one; clone one from a URL; drop one; and edit its remote, its folder, or how
	// changes land. The first three also report the folder the repository sits in.
	AddRepo    func(registry.Entry, string, bool) (vault.Repo, string, error)
	NewRepo    func(registry.Entry, string, string) (vault.Repo, string, error)
	CloneRepo  func(registry.Entry, string, string) (vault.Repo, string, error)
	RemoveRepo func(registry.Entry, string) error
	EditRepo   func(registry.Entry, string, vaults.RepoEdit) (vault.Repo, error)
	// The mount calls: mount a knowledge base on a project with an access and a mount name
	// (empty means write and the knowledge base's name); unmount by knowledge base id, name,
	// or mount name; grant a project write or read on a knowledge base; revoke by project id.
	Mount   func(project, kb registry.Entry, access, name string) (vault.Mount, error)
	Unmount func(project registry.Entry, target string) error
	// EditMount changes what a mount asks for, read or write.
	EditMount func(project registry.Entry, target, access string) (vault.Mount, error)
	Grant     func(kb, project registry.Entry, access string) error
	Revoke    func(kb registry.Entry, projectID string) error
	// The cluster calls: add a knowledge base to a cluster's member list, and drop one by
	// id or name.
	AddMember    func(cluster, kb registry.Entry) error
	RemoveMember func(cluster registry.Entry, target string) error
	// Tasks reads a project's task ledger and the notes waiting in inbox/tasks/; Plant
	// plants a task in its vault.
	Tasks func(registry.Entry) (tasks.Ledger, []string, error)
	Plant func(registry.Entry, tasks.Plant) (txn.Planted, error)
	// VaultsDir is where a new vault goes by default.
	VaultsDir string
}

// Bind builds the struct over an atlas home and its loaded config. The console is for
// Create's preview when a caller wants one; nil and the view pass none.
func Bind(h home.Home, cfg *home.Config, c *console.Console) Atlas {
	return Atlas{
		Load: func() ([]registry.Entry, error) { return refresh.Entries(h, cfg, time.Now()) },
		Scan: func() (*registry.Index, error) { return refresh.Derived(cfg, time.Now()) },
		Create: func(choice AddVault) (string, error) { return createOrAdopt(h, cfg, c, choice) },
		Refresh: func() (*registry.Index, []refresh.ProjectChange, error) {
			_, ix, changes, err := refresh.All(h, cfg, time.Now())
			return ix, changes, err
		},
		Edit: func(en registry.Entry, edit vaults.Edit) (string, error) {
			return vaults.EditIdentity(h, cfg, en, edit, time.Now())
		},
		Unregister: func(en registry.Entry) error { return vaults.Unregister(h, cfg, en.Path) },
		StagePlan: func(en registry.Entry, given []string) (*capture.StagePlan, error) {
			v, err := vault.Open(en.Path)
			if err != nil {
				return nil, err
			}
			sources, err := capture.SourcesFor(v, given)
			if err != nil {
				return nil, err
			}
			return capture.PlanStage(v, sources, time.Now())
		},
		Stage: func(en registry.Entry, plan *capture.StagePlan) (*capture.StageResult, []string, error) {
			v, err := vault.Open(en.Path)
			if err != nil {
				return nil, nil, err
			}
			res, err := capture.ApplyStage(v, plan, time.Now())
			if err != nil {
				return res, nil, err
			}
			return res, res.Remembered, nil
		},
		Sources: func(en registry.Entry) []string {
			v, err := vault.Open(en.Path)
			if err != nil {
				return nil
			}
			return capture.Sources(v)
		},
		AddRepo: func(en registry.Entry, target string, initGit bool) (vault.Repo, string, error) {
			return vaults.AddRepo(h, cfg, en, target, initGit, time.Now())
		},
		NewRepo: func(en registry.Entry, name, at string) (vault.Repo, string, error) {
			return vaults.CreateRepo(h, cfg, en, name, at, time.Now())
		},
		CloneRepo: func(en registry.Entry, url, at string) (vault.Repo, string, error) {
			return vaults.CloneRepo(h, cfg, en, url, at, time.Now())
		},
		RemoveRepo: func(en registry.Entry, name string) error {
			return vaults.RemoveRepo(h, cfg, en, name, time.Now())
		},
		EditRepo: func(en registry.Entry, name string, edit vaults.RepoEdit) (vault.Repo, error) {
			return vaults.EditRepo(h, cfg, en, name, edit, time.Now())
		},
		Mount: func(project, kb registry.Entry, access, name string) (vault.Mount, error) {
			return vaults.Mount(project, kb, access, name, time.Now())
		},
		Unmount: func(project registry.Entry, target string) error {
			return vaults.Unmount(project, target, time.Now())
		},
		EditMount: func(project registry.Entry, target, access string) (vault.Mount, error) {
			return vaults.SetMountAccess(project, target, access, time.Now())
		},
		Grant: func(kb, project registry.Entry, access string) error {
			return vaults.Grant(kb, project, access, time.Now())
		},
		Revoke: func(kb registry.Entry, projectID string) error {
			return vaults.RevokeID(kb, projectID, time.Now())
		},
		AddMember: func(cluster, kb registry.Entry) error {
			return vaults.AddMember(cluster, kb, time.Now())
		},
		RemoveMember: func(cluster registry.Entry, target string) error {
			return vaults.RemoveMember(cluster, target, time.Now())
		},
		Tasks: func(en registry.Entry) (tasks.Ledger, []string, error) {
			v, err := vault.Open(en.Path)
			if err != nil {
				return tasks.Ledger{}, nil, err
			}
			led, err := tasks.Current(v, time.Now())
			return led, tasks.Notes(v), err
		},
		Plant: func(en registry.Entry, plant tasks.Plant) (txn.Planted, error) {
			v, err := vault.Open(en.Path)
			if err != nil {
				return txn.Planted{}, err
			}
			return plantTask(v, plant)
		},
		VaultsDir: cfg.VaultsDir,
	}
}
```

`internal/actions/create.go`:

```go
package actions

import (
	"fmt"
	"time"

	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// createOrAdopt makes or adopts the vault, records the facts the template does not
// carry, registers it, and mounts or gathers what the caller chose. The preview, when
// there is one, is the caller's: Create runs with confirm off, and CreateIn with no
// console.
func createOrAdopt(h home.Home, cfg *home.Config, c *console.Console, choice AddVault) (string, error) {
	mode := vault.Generic
	if choice.Mode != "" {
		var err error
		if mode, err = vault.ParseMode(choice.Mode); err != nil {
			return "", err
		}
	}
	opts := vault.Options{Kind: choice.Kind, Mode: mode, Name: choice.Name}
	switch {
	case choice.Adopt:
		if _, err := vault.Adopt(choice.Path, opts, time.Now()); err != nil {
			return "", err
		}
	case choice.InRepo != "":
		path, err := vaults.CreateIn(choice.InRepo, opts, nil)
		if err != nil {
			return "", err
		}
		choice.Path = path
	default:
		if _, err := vaults.Create(choice.Path, opts, c, false); err != nil {
			return "", err
		}
	}
	if err := recordFacts(h, cfg, choice); err != nil {
		return "", err
	}
	if _, err := vaults.Register(h, cfg, choice.Path); err != nil {
		return "", err
	}
	if err := mountChoice(cfg, choice); err != nil {
		return choice.Path, err
	}
	if err := memberChoice(cfg, choice); err != nil {
		return choice.Path, err
	}
	return choice.Path, nil
}

// recordFacts writes the identity fields the template does not carry: a project's tags,
// a knowledge base's scope and access.
func recordFacts(h home.Home, cfg *home.Config, choice AddVault) error {
	edit := vaults.Edit{}
	switch choice.Kind {
	case vault.Knowledge:
		if choice.Scope != "" {
			edit.Scope = &choice.Scope
		}
		if choice.Access != "" {
			edit.Access = &choice.Access
		}
	case vault.Project:
		if len(choice.Tags) > 0 {
			edit.Tags = &choice.Tags
		}
	}
	if edit == (vaults.Edit{}) {
		return nil
	}
	_, err := vaults.EditIdentity(h, cfg, registry.Entry{Path: choice.Path, Kind: choice.Kind}, edit, time.Now())
	return err
}

// mountChoice mounts the knowledge base a new project chose. The project is already
// written, so a failure here names the mount and leaves the vault.
func mountChoice(cfg *home.Config, choice AddVault) error {
	if choice.MountID == "" {
		return nil
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		return err
	}
	kb := ix.ByID(choice.MountID)
	if kb == nil {
		return fmt.Errorf("no knowledge base with id %s to mount", choice.MountID)
	}
	project := ix.ByPath(choice.Path)
	if project == nil {
		return fmt.Errorf("%s is not in the scan yet; mount %s by hand", choice.Name, kb.Name)
	}
	_, err = vaults.Mount(*project, *kb, vault.AccessWrite, "", time.Now())
	return err
}

// memberChoice records the knowledge bases a new cluster gathers. The cluster is already
// written, so a failure here names the member and leaves the vault.
func memberChoice(cfg *home.Config, choice AddVault) error {
	if len(choice.MemberIDs) == 0 {
		return nil
	}
	for _, id := range choice.MemberIDs {
		ix, err := registry.Scan(cfg)
		if err != nil {
			return err
		}
		cluster := ix.ByPath(choice.Path)
		if cluster == nil {
			return fmt.Errorf("%s is not in the scan yet; add its members by hand", choice.Name)
		}
		kb := ix.ByID(id)
		if kb == nil {
			return fmt.Errorf("no knowledge base with id %s to gather", id)
		}
		if err := vaults.AddMember(*cluster, *kb, time.Now()); err != nil {
			return err
		}
	}
	return nil
}

// plantTask plants one task in a vault as a single operation.
func plantTask(v *vault.Vault, plant tasks.Plant) (txn.Planted, error) {
	now := time.Now()
	req, planted, err := txn.PlantRequest(v, plant, "", now)
	if err != nil {
		return txn.Planted{}, err
	}
	plan, err := txn.Prepare(v, req, now)
	if err != nil {
		return txn.Planted{}, err
	}
	if _, err := txn.Apply(v, plan, now); err != nil {
		return txn.Planted{}, err
	}
	return planted, nil
}
```

`recordFacts` compares `edit == (vaults.Edit{})`: `vaults.Edit` holds a string and three pointers, so it is comparable. If `go vet` objects, replace the check with `if edit.Scope == nil && edit.Access == nil && edit.Tags == nil { return nil }`.

- [ ] **Step 4: Run the actions tests**

Run: `go test ./internal/actions/`
Expected: `ok`. The `tui` and `cli` packages do not build yet; that is the next step.

- [ ] **Step 5: Point `tui` at the package**

Remove `type Hooks struct { ... }` from `internal/tui/editor.go` (the whole block, comment included, from `// Hooks is` or the line before `type Hooks struct` through its closing `}`) and `type AddVault struct { ... }` from `internal/tui/addvault.go` (the block and its `// AddVault is what the user chose` comment). Then rename every use and add the import:

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && for f in internal/tui/*.go; do
  perl -pi -e 's/\btui\.Hooks\b/actions.Atlas/g; s/\bHooks\b/actions.Atlas/g; s/\bAddVault\b/actions.AddVault/g' "$f"
  if grep -q 'actions\.' "$f" && ! grep -q 'internal/actions"' "$f"; then
    perl -0pi -e 's/(\t"github.com\/nathanaday\/claude-atlas\/internal\/)/\t"github.com\/nathanaday\/claude-atlas\/internal\/actions"\n$1/' "$f"
  fi
done
gofmt -l internal/tui/
```

`\bHooks\b` leaves `taskHooks`, `RunAddVault`, and `TestEditNeedsHooks` alone: no word boundary sits inside them. The perl insert puts the import before the first `internal/` import, which keeps gofmt's order since `actions` sorts first. A file that uses `actions.` and has no `internal/` import gets no line; `go vet ./internal/tui/` names it with `undefined: actions`, and the fix is to add `"github.com/nathanaday/claude-atlas/internal/actions"` to that file's import block by hand.

Then the two call sites whose signatures changed. `internal/tui/view.go`, in `refreshCmd`:

```go
	fn := v.hooks.Refresh
	return func() tea.Msg {
		_, _, err := fn()
		return refreshedMsg{err: err}
	}
```

`internal/tui/ingest.go`, where `StagePlan` is called with `s.source.value()`:

```go
				var given []string
				if src := strings.TrimSpace(s.source.value()); src != "" {
					given = []string{src}
				}
				plan, err := s.hooks.StagePlan(s.entry, given)
```

Add `"strings"` to `ingest.go`'s imports if it is missing. `RunAddVault` and `RunAdopt` now return `*actions.AddVault`; the rename did that.

Run: `go vet ./internal/tui/ && go test ./internal/tui/`
Expected: no vet output; `ok`.

- [ ] **Step 6: Point `cli` at the package**

In `internal/cli/cli.go`:

1. Delete these functions whole: `createOrAdopt`, `memberChoice`, `mountChoice`, `recordFacts`, `hooks`, `plantTask`, `ingestSources`, `planStage`, `stage`.
2. Replace the bodies of `refreshAll` and `registryEntries`:

```go
func (e *env) refreshAll(cfg *home.Config) ([]registry.Entry, *registry.Index, error) {
	entries, ix, _, err := refresh.All(e.home, cfg, time.Now())
	return entries, ix, err
}

func (e *env) registryEntries(cfg *home.Config) ([]registry.Entry, error) {
	return refresh.Entries(e.home, cfg, time.Now())
}
```

3. Where the `view` command built `hooks := e.hooks(cfg)`, write `hooks := actions.Bind(e.home, cfg, e.console)`.
4. Where `newVaultInteractive` reads `choice.Mode` through `parseMode(choice.Mode)` after `tui.RunAddVault`, nothing changes except the type: `choice` is now `*actions.AddVault`.
5. In the `ingest` command, replace `sources, err := ingestSources(v, entry.Name, positional[1:])` with `sources, err := capture.SourcesFor(v, positional[1:])`, and replace the call `stage(entry.Path, plan)` with:

```go
		res, remembered, err := actions.Bind(e.home, cfg, e.console).Stage(entry, plan)
```

6. Replace every `tui.AddVault` with `actions.AddVault` and every `tui.Hooks` with `actions.Atlas`:

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && perl -pi -e 's/\btui\.AddVault\b/actions.AddVault/g; s/\btui\.Hooks\b/actions.Atlas/g' internal/cli/cli.go internal/cli/cli_test.go
```

7. Add `"github.com/nathanaday/claude-atlas/internal/actions"` and `"github.com/nathanaday/claude-atlas/internal/refresh"` to `cli.go`'s imports if absent (`refresh` is already imported; check). Remove imports the deletions orphaned; `go vet` names them.

In `internal/cli/cli_test.go`, `TestViewHooksCreateEditAndForget`: replace `hooks := e.hooks(cfg)` with `hooks := actions.Bind(home.Home{Root: h.home}, cfg, e.console)`, and `if err := hooks.Refresh(); err != nil {` with `if _, _, err := hooks.Refresh(); err != nil {`. Add the `actions` import.

Run: `go vet ./... && go test ./internal/cli/ ./internal/tui/ ./internal/actions/`
Expected: no vet output; three `ok`.

- [ ] **Step 7: Run everything and commit**

Run: `make test && gofmt -l internal/`
Expected: every package `ok`; gofmt prints nothing.

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com add internal/ && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -q -m "actions: the view's hooks become one struct the CLI, the view, and the tools bind once" && git log --oneline -1
```

---
