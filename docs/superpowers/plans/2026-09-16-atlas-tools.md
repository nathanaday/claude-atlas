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
- Work on a branch `atlas-tools` from `main`, as `clusters` was. Task 12 ends before any merge; merging is the user's call.

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
	r := gitx.Repo{Dir: repo}
	if err := r.Init(); err != nil {
		t.Fatal(err)
	}
	path, err := a.Create(AddVault{Kind: vault.Project, Name: "code", InRepo: repo, Mode: "generic"})
	if err != nil || path != filepath.Join(repo, vault.InRepoDir) {
		t.Fatalf("in repo: %q %v", path, err)
	}
	ix, _ := a.Scan()
	e := ix.ByPath(path)
	if e == nil || e.Host == "" || filepath.Base(e.Host) != "code" {
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

Add `"strings"` to `ingest.go`'s imports if it is missing. `RunAddVault` and `RunAdopt` now return `*actions.AddVault`; the rename did that. If `go vet` reports an import `addvault.go` or `editor.go` no longer uses after the deletions, remove that import.

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

### Task 4: The server binds per call, and the `atlas` read tool

A new file holds everything atlas-level so `server.go` stays the vault-level server. Every atlas tool starts with `s.bind()`, which loads the config afresh.

**Files:**
- Create: `internal/mcpserver/atlas.go`, `internal/mcpserver/atlas_test.go`
- Modify: `internal/mcpserver/server.go` (`MCP()`: one `mcp.AddTool` line)

**Interfaces:**
- Consumes: `actions.Bind`, `actions.Atlas` (Task 3); `refresh.ProjectChange` (Task 2); the test helpers `connect`, `connectIn`, `mounted`, `client.call` in `server_test.go`.
- Produces, for Tasks 5–9: `func (s *Server) bind() (actions.Atlas, *home.Config, error)`, `func entryOf(ix *registry.Index, arg string) (registry.Entry, error)`, `func anyEntryOf(ix *registry.Index, arg string) (registry.Entry, error)`, `type Settings`, `func settingsOf(cfg *home.Config) Settings`.

- [ ] **Step 1: Write the failing test**

`internal/mcpserver/atlas_test.go`:

```go
package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

func TestAtlasReadsWithoutWritingAndRefreshWrites(t *testing.T) {
	if msg := connect(t, t.TempDir()).call("atlas", map[string]any{}, nil); !strings.Contains(msg, "no atlas") {
		t.Fatalf("without an atlas the tool says so: %q", msg)
	}
	h, cfg, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	var out AtlasOut
	if msg := c.call("atlas", map[string]any{}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Vaults) != 2 || out.Settings.VaultsDir != cfg.VaultsDir || out.Settings.RepoChanges != "commit" {
		t.Fatalf("atlas: %+v", out)
	}
	for _, e := range out.Vaults {
		if e.State == nil {
			t.Fatalf("every entry carries its state: %+v", e)
		}
	}
	if _, err := os.Stat(registry.File(h.StateDir())); !os.IsNotExist(err) {
		t.Fatalf("a plain read writes no registry: %v", err)
	}
	if msg := c.call("atlas", map[string]any{"refresh": true}, &out); msg != "" {
		t.Fatal(msg)
	}
	if _, err := os.Stat(registry.File(h.StateDir())); err != nil {
		t.Fatalf("refresh writes the registry: %v", err)
	}
}
```

The imports `exec`, `gitx`, `links`, `vault`, and `vaults` are for the tests Tasks 5–9 add to this file; Go refuses unused imports, so until those tests exist keep only the ones this test uses (`os`, `path/filepath` is unused too, `strings`, `testing`, `registry`) and add the rest with each task.

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./internal/mcpserver/ -run TestAtlasReads 2>&1 | head -5`
Expected: `undefined: AtlasOut`.

- [ ] **Step 3: Implement**

`internal/mcpserver/atlas.go`:

```go
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nathanaday/claude-atlas/internal/actions"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

// The atlas tools: the whole atlas as one read, and the writes that configure it. They
// bind the same functions the view calls, once per call over a config loaded for that
// call, because the CLI in another process may change config.json between two calls.

// bind loads the config and binds the atlas actions for one call.
func (s *Server) bind() (actions.Atlas, *home.Config, error) {
	cfg, err := s.home().Load()
	if err != nil {
		return actions.Atlas{}, nil, err
	}
	return actions.Bind(s.home(), cfg, nil), cfg, nil
}

// entryOf resolves a vault a tool names: by name, by id, or by path. A vault the atlas
// cannot read is an error that says why.
func entryOf(ix *registry.Index, arg string) (registry.Entry, error) {
	en, err := anyEntryOf(ix, arg)
	if err != nil {
		return registry.Entry{}, err
	}
	if en.Error != "" {
		return registry.Entry{}, fmt.Errorf("%s: %s", home.Display(en.Path), en.Error)
	}
	return en, nil
}

// anyEntryOf is entryOf for a vault the atlas cannot read too, so forget can drop it.
func anyEntryOf(ix *registry.Index, arg string) (registry.Entry, error) {
	if strings.TrimSpace(arg) == "" {
		return registry.Entry{}, errors.New("name a vault: its name, id, or path")
	}
	found, err := ix.Find(arg)
	if err == nil {
		return *found, nil
	}
	if !errors.Is(err, registry.ErrNotFound) {
		return registry.Entry{}, err
	}
	abs, aerr := filepath.Abs(home.Expand(arg))
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error == "" {
			continue
		}
		if (aerr == nil && e.Path == abs) || strings.EqualFold(filepath.Base(e.Path), arg) {
			return *e, nil
		}
	}
	return registry.Entry{}, fmt.Errorf("no vault named %q; the atlas tool lists them", arg)
}

// Settings are the atlas settings a session may read and set.
type Settings struct {
	VaultsDir   string `json:"vaults_dir"`
	NewDays     int    `json:"new_days"`
	RepoChanges string `json:"repo_changes"`
}

func settingsOf(cfg *home.Config) Settings {
	return Settings{VaultsDir: cfg.VaultsDir, NewDays: cfg.NewDays(), RepoChanges: cfg.DefaultChanges()}
}

type AtlasArgs struct {
	Refresh bool `json:"refresh,omitempty" jsonschema:"also rewrite the registry, recreate each project's kb/ links, and adopt repositories waiting under repos/: what claude-atlas refresh does"`
}

// AtlasOut is the whole atlas: every vault with its state, the folders the atlas cannot
// read, the settings, and, after a refresh, what each project needed.
type AtlasOut struct {
	Vaults   []registry.Entry        `json:"vaults"`
	Problems []registry.Problem      `json:"problems"`
	Settings Settings                `json:"settings"`
	Changes  []refresh.ProjectChange `json:"changes,omitempty"`
}

func (s *Server) atlasTool(ctx context.Context, req *mcp.CallToolRequest, a AtlasArgs) (*mcp.CallToolResult, AtlasOut, error) {
	acts, cfg, err := s.bind()
	if err != nil {
		return nil, AtlasOut{}, err
	}
	var ix *registry.Index
	var changes []refresh.ProjectChange
	if a.Refresh {
		ix, changes, err = acts.Refresh()
	} else {
		ix, err = acts.Scan()
	}
	if err != nil {
		return nil, AtlasOut{}, err
	}
	out := AtlasOut{Vaults: ix.Entries, Problems: ix.Problems, Settings: settingsOf(cfg), Changes: changes}
	if out.Vaults == nil {
		out.Vaults = []registry.Entry{}
	}
	if out.Problems == nil {
		out.Problems = []registry.Problem{}
	}
	return nil, out, nil
}
```

In `server.go`, `MCP()`, after the `mode` registration and before `return server`:

```go
	mcp.AddTool(server, &mcp.Tool{Name: "atlas", Annotations: ro(),
		Description: "Read the whole atlas: every vault with its kind, path, tags or scope, access, mounts with effective access, repositories and their change policy, members, clusters, who mounts it, and state; the folders the atlas cannot read; and the settings. Pass refresh to also rewrite the registry and adopt repositories waiting under repos/."}, s.atlasTool)
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/mcpserver/ -run TestAtlasReads`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com add internal/mcpserver && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -q -m "mcpserver: the atlas tool reads every vault and the settings" && git log --oneline -1
```

---

### Task 5: The `vault` tool

**Files:**
- Modify: `internal/mcpserver/atlas.go`, `internal/mcpserver/atlas_test.go`, `internal/mcpserver/server.go`

**Interfaces:**
- Consumes: `bind`, `entryOf`, `anyEntryOf` (Task 4); `actions.AddVault` with `Access`, `InRepo`, `MemberIDs` (Task 3); `vaults.ResolvePath`, `vaults.CheckForget`, `vaults.Edit`; `vault.ParseKind`, `vault.InRepoDir`.
- Produces: `type VaultToolArgs`, `type VaultToolOut`, `func (s *Server) vaultTool`, `func (s *Server) entryOut(acts actions.Atlas, path string) (*mcp.CallToolResult, VaultToolOut, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/mcpserver/atlas_test.go` (and add `os`, `path/filepath`, `gitx`, `vault`, `vaults` to its imports):

```go
func TestVaultCreateAdoptEditForget(t *testing.T) {
	h, cfg, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	var out VaultToolOut
	// A project with tags that mounts kb for writing.
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project", "name": "q", "tags": []string{"usc"}, "mount": "kb"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Vault == nil || out.Vault.Path != vaults.PathFor(cfg.VaultsDir, vault.Project, "q") || len(out.Vault.Tags) != 1 || len(out.Vault.Mounts) != 1 || out.Vault.Mounts[0].Effective != vault.AccessWrite {
		t.Fatalf("create project: %+v", out.Vault)
	}
	// A guarded knowledge base with a scope, then a cluster that gathers both knowledge bases.
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "knowledge", "name": "papers", "scope": "Papers.", "access": "guarded"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Vault.Scope != "Papers." || out.Vault.Access != vault.AccessGuarded {
		t.Fatalf("create knowledge: %+v", out.Vault)
	}
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "knowledge", "name": "domain", "members": []string{"kb", "papers"}}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Vault.Members) != 2 {
		t.Fatalf("create cluster: %+v", out.Vault)
	}
	// Refusals: a taken path, a project as a member, an unknown action, a create with no name.
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project", "name": "q"}, nil); !strings.Contains(msg, "already exists") {
		t.Fatalf("taken path: %q", msg)
	}
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "knowledge", "name": "bad", "members": []string{"q"}}, nil); msg == "" {
		t.Fatal("a project cannot be a member")
	}
	if msg := c.call("vault", map[string]any{"action": "rename"}, nil); !strings.Contains(msg, "action must be") {
		t.Fatalf("unknown action: %q", msg)
	}
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project"}, nil); !strings.Contains(msg, "needs name") {
		t.Fatalf("no name: %q", msg)
	}
	// Edit: a rename moves the folder; an empty scope clears it; access stays.
	if msg := c.call("vault", map[string]any{"action": "edit", "target": "papers", "name": "articles", "scope": ""}, &out); msg != "" {
		t.Fatal(msg)
	}
	if filepath.Base(out.Vault.Path) != "articles" || out.Vault.Scope != "" || out.Vault.Access != vault.AccessGuarded {
		t.Fatalf("edit: %+v", out.Vault)
	}
	// Adopt a plain folder outside the vaults directory and forget it; a vault inside cannot be forgotten.
	outside := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if msg := c.call("vault", map[string]any{"action": "adopt", "path": outside, "kind": "knowledge"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Vault.Kind != vault.Knowledge || out.Vault.Name != "notes" {
		t.Fatalf("adopt: %+v", out.Vault)
	}
	if msg := c.call("vault", map[string]any{"action": "forget", "target": outside}, &out); msg != "" || filepath.Base(out.Forgotten) != "notes" {
		t.Fatalf("forget outside: %q %+v", msg, out)
	}
	if msg := c.call("vault", map[string]any{"action": "forget", "target": "q"}, nil); !strings.Contains(msg, "vaults directory") {
		t.Fatalf("forget inside: %q", msg)
	}
}

func TestVaultCreateInARepository(t *testing.T) {
	h, _, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	repo := filepath.Join(t.TempDir(), "code")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	r := gitx.Repo{Dir: repo}
	if err := r.Init(); err != nil {
		t.Fatal(err)
	}
	var out VaultToolOut
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project", "name": "code", "in_repo": repo}, &out); msg != "" {
		t.Fatal(msg)
	}
	if filepath.Base(out.Vault.Path) != vault.InRepoDir || filepath.Base(filepath.Dir(out.Vault.Path)) != "code" || out.Vault.Host == "" {
		t.Fatalf("in repo: %+v", out.Vault)
	}
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project", "name": "x", "in_repo": repo, "path": "/tmp/x"}, nil); !strings.Contains(msg, "not both") {
		t.Fatalf("path with in_repo: %q", msg)
	}
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "knowledge", "name": "k", "in_repo": repo}, nil); !strings.Contains(msg, "only a project") {
		t.Fatalf("a knowledge base in a repository: %q", msg)
	}
}
```

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/mcpserver/ -run TestVault 2>&1 | head -5`
Expected: `undefined: VaultToolOut`.

- [ ] **Step 3: Implement**

Append to `internal/mcpserver/atlas.go`; add `"github.com/nathanaday/claude-atlas/internal/vault"` and `"github.com/nathanaday/claude-atlas/internal/vaults"` to its imports:

```go
type VaultToolArgs struct {
	Action  string    `json:"action" jsonschema:"create, adopt, edit, or forget"`
	Target  string    `json:"target,omitempty" jsonschema:"edit, forget: the vault, by name, id, or path"`
	Kind    string    `json:"kind,omitempty" jsonschema:"create, adopt: project (default) or knowledge; a cluster is a knowledge base with members"`
	Name    string    `json:"name,omitempty" jsonschema:"create: the vault's name; adopt: its display name, default the folder's; edit: the new name, which renames the folder too"`
	Path    string    `json:"path,omitempty" jsonschema:"create: where the vault goes, default <vaults dir>/projects/<name> or knowledge/<name>; adopt: the folder to adopt"`
	Mode    string    `json:"mode,omitempty" jsonschema:"create, adopt: the filing mode, generic (default) or lyt"`
	Tags    *[]string `json:"tags,omitempty" jsonschema:"create, edit: a project's tags; on edit an empty list clears them"`
	Scope   *string   `json:"scope,omitempty" jsonschema:"create, edit: what a knowledge base covers, one or two sentences; on edit an empty string clears it"`
	Access  *string   `json:"access,omitempty" jsonschema:"create, edit: a knowledge base's access, open (default) or guarded"`
	InRepo  string    `json:"in_repo,omitempty" jsonschema:"create: a git repository's top level; the project goes to REPO/atlas/ and shares its git; not with path"`
	Mount   string    `json:"mount,omitempty" jsonschema:"create: a knowledge base a new project mounts for writing, by name, id, or path"`
	Members []string  `json:"members,omitempty" jsonschema:"create: the knowledge bases a new cluster gathers, by name, id, or path"`
}

// VaultToolOut is the vault as the atlas sees it after the change, or the path forget dropped.
type VaultToolOut struct {
	Vault     *registry.Entry `json:"vault,omitempty"`
	Forgotten string          `json:"forgotten,omitempty" jsonschema:"the path the atlas no longer lists; the folder stays"`
}

func (s *Server) vaultTool(ctx context.Context, req *mcp.CallToolRequest, a VaultToolArgs) (*mcp.CallToolResult, VaultToolOut, error) {
	acts, cfg, err := s.bind()
	if err != nil {
		return nil, VaultToolOut{}, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, VaultToolOut{}, err
	}
	switch a.Action {
	case "create", "adopt":
		choice, err := vaultChoice(ix, cfg, a)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		path, err := acts.Create(choice)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		return s.entryOut(acts, path)
	case "edit":
		en, err := entryOf(ix, a.Target)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		path, err := acts.Edit(en, vaults.Edit{Name: a.Name, Tags: a.Tags, Scope: a.Scope, Access: a.Access})
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		return s.entryOut(acts, path)
	case "forget":
		en, err := anyEntryOf(ix, a.Target)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		if err := vaults.CheckForget(cfg, en.Path); err != nil {
			return nil, VaultToolOut{}, err
		}
		if err := acts.Unregister(en); err != nil {
			return nil, VaultToolOut{}, err
		}
		return nil, VaultToolOut{Forgotten: en.Path}, nil
	}
	return nil, VaultToolOut{}, fmt.Errorf("action must be create, adopt, edit, or forget, not %q", a.Action)
}

// vaultChoice turns the create and adopt arguments into what Create takes, with the
// mount and the members resolved to ids before anything is written.
func vaultChoice(ix *registry.Index, cfg *home.Config, a VaultToolArgs) (actions.AddVault, error) {
	kind := vault.Project
	if a.Kind != "" {
		var err error
		if kind, err = vault.ParseKind(a.Kind); err != nil {
			return actions.AddVault{}, err
		}
	}
	choice := actions.AddVault{Kind: kind, Name: a.Name, Mode: a.Mode, Adopt: a.Action == "adopt"}
	if a.Tags != nil {
		choice.Tags = *a.Tags
	}
	if a.Scope != nil {
		choice.Scope = *a.Scope
	}
	if a.Access != nil {
		choice.Access = *a.Access
	}
	switch {
	case choice.Adopt:
		if a.Path == "" {
			return actions.AddVault{}, errors.New("adopt needs path: the folder to adopt")
		}
		abs, err := filepath.Abs(home.Expand(a.Path))
		if err != nil {
			return actions.AddVault{}, err
		}
		choice.Path = abs
		if choice.Name == "" {
			choice.Name = filepath.Base(abs)
		}
	case a.InRepo != "":
		if a.Path != "" {
			return actions.AddVault{}, errors.New("give path or in_repo, not both")
		}
		if kind != vault.Project {
			return actions.AddVault{}, errors.New("only a project lives inside a repository")
		}
		if a.Name == "" {
			return actions.AddVault{}, errors.New("create needs name")
		}
		choice.InRepo = home.Expand(a.InRepo)
	default:
		if a.Name == "" {
			return actions.AddVault{}, errors.New("create needs name")
		}
		arg := a.Path
		if arg == "" {
			arg = a.Name
		}
		path, err := vaults.ResolvePath(arg, cfg.VaultsDir, kind)
		if err != nil {
			return actions.AddVault{}, err
		}
		choice.Path = path
	}
	if a.Mount != "" {
		if kind != vault.Project {
			return actions.AddVault{}, errors.New("only a project mounts a knowledge base")
		}
		kb, err := entryOf(ix, a.Mount)
		if err != nil {
			return actions.AddVault{}, err
		}
		if kb.Kind != vault.Knowledge {
			return actions.AddVault{}, fmt.Errorf("%s is a project; a project mounts knowledge bases", kb.Name)
		}
		choice.MountID = kb.ID
	}
	for _, m := range a.Members {
		if kind != vault.Knowledge {
			return actions.AddVault{}, errors.New("only a knowledge base gathers members")
		}
		kb, err := entryOf(ix, m)
		if err != nil {
			return actions.AddVault{}, err
		}
		if kb.Kind != vault.Knowledge {
			return actions.AddVault{}, fmt.Errorf("%s is a project; a cluster gathers knowledge bases", kb.Name)
		}
		choice.Cluster = true
		choice.MemberIDs = append(choice.MemberIDs, kb.ID)
	}
	return choice, nil
}

// entryOut scans again and returns the vault at path as the atlas now sees it.
func (s *Server) entryOut(acts actions.Atlas, path string) (*mcp.CallToolResult, VaultToolOut, error) {
	ix, err := acts.Scan()
	if err != nil {
		return nil, VaultToolOut{}, err
	}
	en := ix.ByPath(path)
	if en == nil {
		return nil, VaultToolOut{}, fmt.Errorf("%s was written but the scan does not list it; call atlas with refresh", home.Display(path))
	}
	return nil, VaultToolOut{Vault: en}, nil
}
```

In `server.go`, `MCP()`, after the `atlas` registration:

```go
	mcp.AddTool(server, &mcp.Tool{Name: "vault",
		Description: "Create or adopt a vault, edit its name, tags, scope, or access, or forget one the atlas lists (the folder stays); action is create, adopt, edit, or forget. A create may mount a knowledge base or gather members. State the change and get a yes before calling."}, s.vaultTool)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/mcpserver/ -run 'TestVault|TestAtlas'`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com add internal/mcpserver && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -q -m "mcpserver: the vault tool creates, adopts, edits, and forgets" && git log --oneline -1
```

---

### Task 6: The `mount` tool

**Files:**
- Modify: `internal/mcpserver/atlas.go`, `internal/mcpserver/atlas_test.go`, `internal/mcpserver/server.go`

**Interfaces:**
- Consumes: `bind`, `entryOf` (Task 4); `actions.Atlas.Mount`, `Unmount`, `EditMount`, `Grant`, `Revoke` (Task 3); `registry.Mount`, `registry.Grant`.
- Produces: `type MountToolArgs`, `type MountToolOut`, `func (s *Server) mountTool`, `func checkAccess(access string) error`.

- [ ] **Step 1: Write the failing test**

Append to `internal/mcpserver/atlas_test.go`:

```go
func TestMountAccessGrantRevokeUnmount(t *testing.T) {
	h, _, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	// A second project, and kb made guarded so grants decide what it may do.
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project", "name": "q"}, nil); msg != "" {
		t.Fatal(msg)
	}
	if msg := c.call("vault", map[string]any{"action": "edit", "target": "kb", "access": "guarded"}, nil); msg != "" {
		t.Fatal(msg)
	}
	var out MountToolOut
	if msg := c.call("mount", map[string]any{"action": "mount", "project": "q", "knowledge": "kb", "access": "read"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Mount == nil || out.Mount.Access != vault.AccessRead || out.Mount.Effective != vault.AccessRead {
		t.Fatalf("mount: %+v", out.Mount)
	}
	if msg := c.call("mount", map[string]any{"action": "access", "project": "q", "knowledge": "kb", "access": "write"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Mount.Access != vault.AccessWrite || out.Mount.Effective != vault.AccessRead {
		t.Fatalf("guarded with no grant reads: %+v", out.Mount)
	}
	if msg := c.call("mount", map[string]any{"action": "grant", "knowledge": "kb", "project": "q", "access": "write"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Grants) != 1 || out.Grants[0].Access != vault.AccessWrite || out.Grants[0].Name != "q" {
		t.Fatalf("grant: %+v", out.Grants)
	}
	if msg := c.call("mount", map[string]any{"action": "access", "project": "q", "knowledge": "kb", "access": "read"}, &out); msg != "" || out.Mount.Effective != vault.AccessRead {
		t.Fatalf("asking for read reads: %q %+v", msg, out.Mount)
	}
	if msg := c.call("mount", map[string]any{"action": "access", "project": "q", "knowledge": "kb", "access": "write"}, &out); msg != "" || out.Mount.Effective != vault.AccessWrite {
		t.Fatalf("after the grant the mount writes: %q %+v", msg, out.Mount)
	}
	if msg := c.call("mount", map[string]any{"action": "revoke", "knowledge": "kb", "project": "q"}, &out); msg != "" || len(out.Grants) != 0 {
		t.Fatalf("revoke: %q %+v", msg, out.Grants)
	}
	if msg := c.call("mount", map[string]any{"action": "grant", "knowledge": "kb", "project": "q", "access": "all"}, nil); !strings.Contains(msg, "read or write") {
		t.Fatalf("bad access: %q", msg)
	}
	if msg := c.call("mount", map[string]any{"action": "mount", "project": "kb", "knowledge": "q"}, nil); msg == "" {
		t.Fatal("a knowledge base mounts nothing")
	}
	if msg := c.call("mount", map[string]any{"action": "unmount", "project": "q", "knowledge": "kb"}, &out); msg != "" || len(out.Mounts) != 0 {
		t.Fatalf("unmount: %q %+v", msg, out.Mounts)
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./internal/mcpserver/ -run TestMountAccess 2>&1 | head -5`
Expected: `undefined: MountToolOut`.

- [ ] **Step 3: Implement**

Append to `internal/mcpserver/atlas.go`:

```go
type MountToolArgs struct {
	Action    string `json:"action" jsonschema:"mount, unmount, access, grant, or revoke"`
	Project   string `json:"project,omitempty" jsonschema:"the project, by name, id, or path; on revoke a stale grant's id works too"`
	Knowledge string `json:"knowledge,omitempty" jsonschema:"the knowledge base, by name, id, or path; on unmount and access the mount's own name works too"`
	Access    string `json:"access,omitempty" jsonschema:"mount: what the project asks for, write (default) or read; access and grant: read or write"`
	As        string `json:"as,omitempty" jsonschema:"mount: the name the project reaches it under, default the knowledge base's name"`
}

// MountToolOut is the mount as it now stands, with its effective access; after unmount,
// what the project still mounts; after grant or revoke, the knowledge base's grants.
type MountToolOut struct {
	Mount  *registry.Mount  `json:"mount,omitempty"`
	Mounts []registry.Mount `json:"mounts,omitempty"`
	Grants []registry.Grant `json:"grants,omitempty"`
}

func checkAccess(access string) error {
	if access != vault.AccessRead && access != vault.AccessWrite {
		return fmt.Errorf("access must be read or write, not %q", access)
	}
	return nil
}

// mountTarget is what Unmount and SetMountAccess take: the knowledge base's id when the
// argument names a vault, else the argument as a mount name.
func mountTarget(ix *registry.Index, arg string) string {
	if en, err := ix.Find(arg); err == nil {
		return en.ID
	}
	return arg
}

func (s *Server) mountTool(ctx context.Context, req *mcp.CallToolRequest, a MountToolArgs) (*mcp.CallToolResult, MountToolOut, error) {
	acts, _, err := s.bind()
	if err != nil {
		return nil, MountToolOut{}, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, MountToolOut{}, err
	}
	switch a.Action {
	case "mount":
		project, err := entryOf(ix, a.Project)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		kb, err := entryOf(ix, a.Knowledge)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		access := a.Access
		if access == "" {
			access = vault.AccessWrite
		}
		if err := checkAccess(access); err != nil {
			return nil, MountToolOut{}, err
		}
		m, err := acts.Mount(project, kb, access, a.As)
		if err != nil {
			if m.ID != "" {
				return nil, MountToolOut{}, fmt.Errorf("%w; the mount is recorded, so atlas with refresh can make kb/%s", err, m.Name)
			}
			return nil, MountToolOut{}, err
		}
		return s.mountOut(acts, project.ID, m.Name)
	case "access":
		project, err := entryOf(ix, a.Project)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		if err := checkAccess(a.Access); err != nil {
			return nil, MountToolOut{}, err
		}
		m, err := acts.EditMount(project, mountTarget(ix, a.Knowledge), a.Access)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		return s.mountOut(acts, project.ID, m.Name)
	case "unmount":
		project, err := entryOf(ix, a.Project)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		if err := acts.Unmount(project, mountTarget(ix, a.Knowledge)); err != nil {
			return nil, MountToolOut{}, err
		}
		after, err := acts.Scan()
		if err != nil {
			return nil, MountToolOut{}, err
		}
		out := MountToolOut{Mounts: []registry.Mount{}}
		if p := after.ByID(project.ID); p != nil && p.Mounts != nil {
			out.Mounts = p.Mounts
		}
		return nil, out, nil
	case "grant", "revoke":
		kb, err := entryOf(ix, a.Knowledge)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		if a.Action == "grant" {
			project, err := entryOf(ix, a.Project)
			if err != nil {
				return nil, MountToolOut{}, err
			}
			if err := checkAccess(a.Access); err != nil {
				return nil, MountToolOut{}, err
			}
			if err := acts.Grant(kb, project, a.Access); err != nil {
				return nil, MountToolOut{}, err
			}
		} else {
			// A stale grant's id first, so a project the scan lost can still be revoked.
			id := ""
			for _, g := range kb.Grants {
				if g.ID == a.Project {
					id = g.ID
				}
			}
			if id == "" {
				project, err := entryOf(ix, a.Project)
				if err != nil {
					return nil, MountToolOut{}, err
				}
				id = project.ID
			}
			if err := acts.Revoke(kb, id); err != nil {
				return nil, MountToolOut{}, err
			}
		}
		after, err := acts.Scan()
		if err != nil {
			return nil, MountToolOut{}, err
		}
		out := MountToolOut{Grants: []registry.Grant{}}
		if k := after.ByID(kb.ID); k != nil && k.Grants != nil {
			out.Grants = k.Grants
		}
		return nil, out, nil
	}
	return nil, MountToolOut{}, fmt.Errorf("action must be mount, unmount, access, grant, or revoke, not %q", a.Action)
}

// mountOut scans again and returns the project's mount by name, with its effective access.
func (s *Server) mountOut(acts actions.Atlas, projectID, mountName string) (*mcp.CallToolResult, MountToolOut, error) {
	ix, err := acts.Scan()
	if err != nil {
		return nil, MountToolOut{}, err
	}
	if p := ix.ByID(projectID); p != nil {
		for i := range p.Mounts {
			if p.Mounts[i].Name == mountName {
				return nil, MountToolOut{Mount: &p.Mounts[i]}, nil
			}
		}
	}
	return nil, MountToolOut{}, fmt.Errorf("kb/%s is recorded but the scan does not list it; call atlas with refresh", mountName)
}
```

In `server.go`, `MCP()`, after the `vault` registration:

```go
	mcp.AddTool(server, &mcp.Tool{Name: "mount",
		Description: "Change how a project reaches a knowledge base: mount or unmount it, set what the mount asks for (read or write), grant or revoke a project on a guarded knowledge base; action is mount, unmount, access, grant, or revoke. Returns the mount with its effective access, or the grants. State the change and get a yes before calling."}, s.mountTool)
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/mcpserver/ -run TestMountAccess`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com add internal/mcpserver && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -q -m "mcpserver: the mount tool mounts, grants, and revokes" && git log --oneline -1
```

---

### Task 7: The `cluster` tool

**Files:**
- Modify: `internal/mcpserver/atlas.go`, `internal/mcpserver/atlas_test.go`, `internal/mcpserver/server.go`

**Interfaces:**
- Consumes: `bind`, `entryOf` (Task 4); `actions.Atlas.AddMember`, `RemoveMember` (Task 3); `registry.Ref`.
- Produces: `type ClusterToolArgs`, `type ClusterToolOut`, `func (s *Server) clusterTool`.

- [ ] **Step 1: Write the failing test**

Append to `internal/mcpserver/atlas_test.go`:

```go
func TestClusterAddAndRemove(t *testing.T) {
	h, _, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "knowledge", "name": "domain"}, nil); msg != "" {
		t.Fatal(msg)
	}
	var out ClusterToolOut
	if msg := c.call("cluster", map[string]any{"action": "add", "cluster": "domain", "knowledge": "kb"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Cluster != "domain" || len(out.Members) != 1 || out.Members[0].Name != "kb" {
		t.Fatalf("add: %+v", out)
	}
	if msg := c.call("cluster", map[string]any{"action": "add", "cluster": "domain", "knowledge": "p"}, nil); msg == "" {
		t.Fatal("a project is not a member")
	}
	if msg := c.call("cluster", map[string]any{"action": "remove", "cluster": "domain", "knowledge": "nobody"}, nil); !strings.Contains(msg, "no member") {
		t.Fatalf("unknown member: %q", msg)
	}
	if msg := c.call("cluster", map[string]any{"action": "remove", "cluster": "domain", "knowledge": "kb"}, &out); msg != "" || len(out.Members) != 0 {
		t.Fatalf("remove: %q %+v", msg, out)
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./internal/mcpserver/ -run TestClusterAdd 2>&1 | head -5`
Expected: `undefined: ClusterToolOut`.

- [ ] **Step 3: Implement**

Append to `internal/mcpserver/atlas.go`:

```go
type ClusterToolArgs struct {
	Action    string `json:"action" jsonschema:"add or remove"`
	Cluster   string `json:"cluster" jsonschema:"the cluster, by name, id, or path; a knowledge base becomes a cluster with its first member"`
	Knowledge string `json:"knowledge" jsonschema:"add: the knowledge base to gather, by name, id, or path; remove: a member by name or id, even one the scan lost"`
}

// ClusterToolOut is the cluster's members after the change.
type ClusterToolOut struct {
	Cluster string         `json:"cluster"`
	Members []registry.Ref `json:"members"`
}

func (s *Server) clusterTool(ctx context.Context, req *mcp.CallToolRequest, a ClusterToolArgs) (*mcp.CallToolResult, ClusterToolOut, error) {
	acts, _, err := s.bind()
	if err != nil {
		return nil, ClusterToolOut{}, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, ClusterToolOut{}, err
	}
	cluster, err := entryOf(ix, a.Cluster)
	if err != nil {
		return nil, ClusterToolOut{}, err
	}
	switch a.Action {
	case "add":
		kb, err := entryOf(ix, a.Knowledge)
		if err != nil {
			return nil, ClusterToolOut{}, err
		}
		if err := acts.AddMember(cluster, kb); err != nil {
			return nil, ClusterToolOut{}, err
		}
	case "remove":
		// The member first, by name or id, so one the scan lost still drops; only when
		// the cluster holds no such member does the argument name a vault.
		target := ""
		for _, m := range cluster.Members {
			if m.ID == a.Knowledge || strings.EqualFold(m.Name, a.Knowledge) {
				target = m.ID
			}
		}
		if target == "" {
			kb, err := entryOf(ix, a.Knowledge)
			if err != nil {
				return nil, ClusterToolOut{}, fmt.Errorf("%s holds no member named %q", cluster.Name, a.Knowledge)
			}
			target = kb.ID
			held := false
			for _, m := range cluster.Members {
				held = held || m.ID == target
			}
			if !held {
				return nil, ClusterToolOut{}, fmt.Errorf("%s holds no member named %q", cluster.Name, kb.Name)
			}
		}
		if err := acts.RemoveMember(cluster, target); err != nil {
			return nil, ClusterToolOut{}, err
		}
	default:
		return nil, ClusterToolOut{}, fmt.Errorf("action must be add or remove, not %q", a.Action)
	}
	after, err := acts.Scan()
	if err != nil {
		return nil, ClusterToolOut{}, err
	}
	out := ClusterToolOut{Cluster: cluster.Name, Members: []registry.Ref{}}
	if c := after.ByID(cluster.ID); c != nil && c.Members != nil {
		out.Members = c.Members
	}
	return nil, out, nil
}
```

In `server.go`, `MCP()`, after the `mount` registration:

```go
	mcp.AddTool(server, &mcp.Tool{Name: "cluster",
		Description: "Add a knowledge base to a cluster or drop a member; action is add or remove. A cluster is a knowledge base with members, and a project that mounts it reaches every member. State the change and get a yes before calling."}, s.clusterTool)
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/mcpserver/ -run TestClusterAdd`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com add internal/mcpserver && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -q -m "mcpserver: the cluster tool adds and drops members" && git log --oneline -1
```

---

### Task 8: The `repo` tool

**Files:**
- Modify: `internal/mcpserver/atlas.go`, `internal/mcpserver/atlas_test.go`, `internal/mcpserver/server.go`

**Interfaces:**
- Consumes: `bind`, `entryOf` (Task 4); `actions.Atlas.AddRepo`, `NewRepo`, `CloneRepo`, `RemoveRepo`, `EditRepo` (Task 3); `vaults.RepoEdit`, `vaults.NotRepoError`; `links.IsRemoteURL`, `links.ChangesPR`, `links.ChangesCommit`; `repoInfo` and `RepoInfo` in `server.go`.
- Produces: `type RepoToolArgs`, `type RepoToolOut`, `func (s *Server) repoTool`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/mcpserver/atlas_test.go` (add `os/exec` and `links` to its imports):

```go
func TestRepoLinkNewEditUnlink(t *testing.T) {
	h, _, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	code := filepath.Join(t.TempDir(), "code")
	if err := os.MkdirAll(code, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(code, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out RepoToolOut
	if msg := c.call("repo", map[string]any{"action": "link", "project": "p", "path": code}, nil); !strings.Contains(msg, "init") {
		t.Fatalf("a plain folder needs init: %q", msg)
	}
	if msg := c.call("repo", map[string]any{"action": "link", "project": "p", "path": code, "init": true}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Repo == nil || out.Repo.Name != "code" || out.Repo.Changes != links.ChangesCommit || !links.IsRepo(out.Repo.Path) {
		t.Fatalf("link: %+v", out.Repo)
	}
	if msg := c.call("repo", map[string]any{"action": "edit", "project": "p", "name": "code", "changes": "pr"}, &out); msg != "" || out.Repo.Changes != links.ChangesPR {
		t.Fatalf("edit: %q %+v", msg, out.Repo)
	}
	if msg := c.call("repo", map[string]any{"action": "edit", "project": "p", "name": "code", "changes": "maybe"}, nil); !strings.Contains(msg, "pr or commit") {
		t.Fatalf("bad policy: %q", msg)
	}
	if msg := c.call("repo", map[string]any{"action": "new", "project": "p", "name": "tool"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Repo.Path != p.Path("repos/tool") || !links.IsRepo(out.Repo.Path) {
		t.Fatalf("new: %+v", out.Repo)
	}
	// The folder decides membership: a repository still under repos/ cannot be unlinked; one outside can.
	if msg := c.call("repo", map[string]any{"action": "unlink", "project": "p", "name": "tool"}, nil); msg == "" {
		t.Fatal("a repository under repos/ is refused")
	}
	if msg := c.call("repo", map[string]any{"action": "unlink", "project": "p", "name": "code"}, &out); msg != "" || out.Unlinked != "code" {
		t.Fatalf("unlink: %q %+v", msg, out)
	}
	if _, err := os.Stat(code); err != nil {
		t.Fatal("unlink leaves the folder")
	}
	if msg := c.call("repo", map[string]any{"action": "link", "project": "kb", "path": code}, nil); msg == "" {
		t.Fatal("a knowledge base has no repositories")
	}
}

func TestRepoCloneFromAFileURL(t *testing.T) {
	h, _, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	bare := filepath.Join(t.TempDir(), "upstream.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var out RepoToolOut
	if msg := c.call("repo", map[string]any{"action": "clone", "project": "p", "url": "file://" + bare}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Repo.Name != "upstream" || out.Repo.Path != p.Path("repos/upstream") || !strings.Contains(out.Repo.Remote, "upstream.git") {
		t.Fatalf("clone: %+v", out.Repo)
	}
	if msg := c.call("repo", map[string]any{"action": "link", "project": "p", "path": "https://example.com/x.git"}, nil); !strings.Contains(msg, "clone") {
		t.Fatalf("a URL on link points at clone: %q", msg)
	}
}
```

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/mcpserver/ -run TestRepo 2>&1 | head -5`
Expected: `undefined: RepoToolOut`.

- [ ] **Step 3: Implement**

Append to `internal/mcpserver/atlas.go`; add `"github.com/nathanaday/claude-atlas/internal/links"` to its imports:

```go
type RepoToolArgs struct {
	Action  string  `json:"action" jsonschema:"link, new, clone, unlink, or edit"`
	Project string  `json:"project" jsonschema:"the project, by name, id, or path"`
	Path    string  `json:"path,omitempty" jsonschema:"link: the repository's folder; edit: point the entry at another folder"`
	Init    bool    `json:"init,omitempty" jsonschema:"link: make a plain folder a git repository first, with one commit of what it holds"`
	URL     string  `json:"url,omitempty" jsonschema:"clone: an https, ssh, git, or file URL, or git@host:path"`
	Name    string  `json:"name,omitempty" jsonschema:"new: the repository's name; unlink and edit: which repository"`
	At      string  `json:"at,omitempty" jsonschema:"new and clone: where the repository goes, default repos/ inside the project"`
	Remote  *string `json:"remote,omitempty" jsonschema:"edit: the remote URL; an empty string clears it"`
	Changes *string `json:"changes,omitempty" jsonschema:"edit: how changes land there, pr or commit; an empty string returns to the atlas default"`
}

// RepoToolOut is the repository as the atlas now sees it, or the name unlink dropped.
type RepoToolOut struct {
	Repo     *RepoInfo `json:"repo,omitempty"`
	Unlinked string    `json:"unlinked,omitempty" jsonschema:"the repository dropped from the project; its folder stays"`
}

func (s *Server) repoTool(ctx context.Context, req *mcp.CallToolRequest, a RepoToolArgs) (*mcp.CallToolResult, RepoToolOut, error) {
	acts, _, err := s.bind()
	if err != nil {
		return nil, RepoToolOut{}, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, RepoToolOut{}, err
	}
	project, err := entryOf(ix, a.Project)
	if err != nil {
		return nil, RepoToolOut{}, err
	}
	if project.Kind != vault.Project {
		return nil, RepoToolOut{}, fmt.Errorf("%s is a knowledge base and has no repositories; they belong to a project", project.Name)
	}
	var name string
	switch a.Action {
	case "link":
		if links.IsRemoteURL(a.Path) {
			return nil, RepoToolOut{}, fmt.Errorf("%s is a URL; pass it as url with action clone", a.Path)
		}
		repo, _, err := acts.AddRepo(project, home.Expand(a.Path), a.Init)
		var notRepo *vaults.NotRepoError
		if errors.As(err, &notRepo) {
			return nil, RepoToolOut{}, fmt.Errorf("%w; pass init to make it one", err)
		}
		if err != nil {
			return nil, RepoToolOut{}, err
		}
		name = repo.Name
	case "new":
		repo, _, err := acts.NewRepo(project, a.Name, home.Expand(a.At))
		if err != nil {
			return nil, RepoToolOut{}, err
		}
		name = repo.Name
	case "clone":
		repo, _, err := acts.CloneRepo(project, a.URL, home.Expand(a.At))
		if err != nil {
			return nil, RepoToolOut{}, err
		}
		name = repo.Name
	case "unlink":
		if err := acts.RemoveRepo(project, a.Name); err != nil {
			return nil, RepoToolOut{}, err
		}
		return nil, RepoToolOut{Unlinked: a.Name}, nil
	case "edit":
		if a.Changes != nil && *a.Changes != "" && *a.Changes != links.ChangesPR && *a.Changes != links.ChangesCommit {
			return nil, RepoToolOut{}, fmt.Errorf("changes must be pr or commit, not %q", *a.Changes)
		}
		repo, err := acts.EditRepo(project, a.Name, vaults.RepoEdit{Remote: a.Remote, Changes: a.Changes, Path: home.Expand(a.Path)})
		if err != nil {
			return nil, RepoToolOut{}, err
		}
		name = repo.Name
	default:
		return nil, RepoToolOut{}, fmt.Errorf("action must be link, new, clone, unlink, or edit, not %q", a.Action)
	}
	after, err := acts.Scan()
	if err != nil {
		return nil, RepoToolOut{}, err
	}
	if p := after.ByID(project.ID); p != nil {
		for _, r := range p.Repos {
			if r.Name == name {
				info := repoInfo(r)
				return nil, RepoToolOut{Repo: &info}, nil
			}
		}
	}
	return nil, RepoToolOut{}, fmt.Errorf("%s is recorded but the scan does not list it; call atlas with refresh", name)
}
```

`home.Expand("")` returns `""`, so an absent `at` or `path` stays absent.

In `server.go`, `MCP()`, after the `cluster` registration:

```go
	mcp.AddTool(server, &mcp.Tool{Name: "repo",
		Description: "Change a project's repositories: link a folder (init makes a plain folder one), create one, clone a URL, unlink one (the folder stays), or edit its remote, folder, or change policy (pr or commit); action is link, new, clone, unlink, or edit. State the change and get a yes before calling."}, s.repoTool)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/mcpserver/ -run TestRepo`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com add internal/mcpserver && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -q -m "mcpserver: the repo tool links, creates, clones, unlinks, and edits" && git log --oneline -1
```

---

### Task 9: The `settings` and `stage` tools

**Files:**
- Modify: `internal/mcpserver/atlas.go`, `internal/mcpserver/atlas_test.go`, `internal/mcpserver/server.go`

**Interfaces:**
- Consumes: `bind`, `entryOf`, `Settings`, `settingsOf` (Task 4); `actions.Atlas.StagePlan`, `Stage`, `Refresh` (Task 3); `home.Config.SetNewDays`, `SetDefaultChanges`, `home.Home.Save`; `capture.StagePlan`, `capture.StageResult`.
- Produces: `type SettingsArgs`, `func (s *Server) settingsTool`, `type StageArgs`, `type StageOut`, `func (s *Server) stageTool`.

- [ ] **Step 1: Write the failing test**

Append to `internal/mcpserver/atlas_test.go`:

```go
func TestSettingsAndStage(t *testing.T) {
	h, _, p, kb := mounted(t)
	c := connectIn(t, h, p.Root)
	var st Settings
	if msg := c.call("settings", map[string]any{"new_days": 3, "repo_changes": "pr"}, &st); msg != "" || st.NewDays != 3 || st.RepoChanges != "pr" {
		t.Fatalf("settings: %q %+v", msg, st)
	}
	if msg := c.call("settings", map[string]any{"repo_changes": "maybe"}, nil); msg == "" {
		t.Fatal("a bad policy is refused")
	}
	if msg := c.call("settings", map[string]any{}, &st); msg != "" || st.NewDays != 3 || st.RepoChanges != "pr" {
		t.Fatalf("a read returns what was saved: %q %+v", msg, st)
	}
	src := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.md"), []byte("# a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out StageOut
	if msg := c.call("stage", map[string]any{"project": "p", "paths": []string{src}, "dry_run": true}, &out); msg != "" || out.Plan == nil || len(out.Plan.New) != 1 || out.Result != nil {
		t.Fatalf("dry run: %q %+v", msg, out)
	}
	if _, err := os.Stat(p.Path("inbox/a.md")); !os.IsNotExist(err) {
		t.Fatal("a dry run copies nothing")
	}
	if msg := c.call("stage", map[string]any{"project": "p", "paths": []string{src}}, &out); msg != "" || out.Result == nil || len(out.Result.Staged) != 1 || len(out.Remembered) != 1 {
		t.Fatalf("stage: %q %+v", msg, out)
	}
	if _, err := os.Stat(p.Path("inbox/a.md")); err != nil {
		t.Fatal("the file is in the inbox")
	}
	if msg := c.call("stage", map[string]any{"project": "p"}, &out); msg != "" || len(out.Plan.New) != 0 || len(out.Plan.Unchanged) != 1 {
		t.Fatalf("the remembered folder has nothing new: %q %+v", msg, out.Plan)
	}
	if msg := c.call("stage", map[string]any{"project": kb.Root}, nil); !strings.Contains(msg, "no inbox") {
		t.Fatalf("a knowledge base: %q", msg)
	}
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `go test ./internal/mcpserver/ -run TestSettingsAndStage 2>&1 | head -5`
Expected: `undefined: StageOut`.

- [ ] **Step 3: Implement**

Append to `internal/mcpserver/atlas.go`; add `"github.com/nathanaday/claude-atlas/internal/capture"` to its imports:

```go
type SettingsArgs struct {
	NewDays     *int    `json:"new_days,omitempty" jsonschema:"a vault is new for this many days after its creation; 0 turns it off"`
	RepoChanges *string `json:"repo_changes,omitempty" jsonschema:"how a newly linked repository lands its changes: commit or pr"`
}

func (s *Server) settingsTool(ctx context.Context, req *mcp.CallToolRequest, a SettingsArgs) (*mcp.CallToolResult, Settings, error) {
	acts, cfg, err := s.bind()
	if err != nil {
		return nil, Settings{}, err
	}
	if a.NewDays != nil {
		if err := cfg.SetNewDays(*a.NewDays); err != nil {
			return nil, Settings{}, err
		}
	}
	if a.RepoChanges != nil {
		if err := cfg.SetDefaultChanges(*a.RepoChanges); err != nil {
			return nil, Settings{}, err
		}
	}
	if a.NewDays != nil || a.RepoChanges != nil {
		if err := s.home().Save(cfg); err != nil {
			return nil, Settings{}, err
		}
		if _, _, err := acts.Refresh(); err != nil {
			return nil, Settings{}, err
		}
	}
	return nil, settingsOf(cfg), nil
}

type StageArgs struct {
	Project string   `json:"project" jsonschema:"the project whose inbox receives the files, by name, id, or path"`
	Paths   []string `json:"paths,omitempty" jsonschema:"files or folders outside the vault; omit to stage what is new in the folders the project staged from before"`
	DryRun  bool     `json:"dry_run,omitempty" jsonschema:"plan only: say what would be copied and copy nothing"`
}

// StageOut is the plan, and after a copy, what was copied and the folders the project
// now stages from when paths is omitted.
type StageOut struct {
	Plan       *capture.StagePlan   `json:"plan"`
	Result     *capture.StageResult `json:"result,omitempty"`
	Remembered []string             `json:"remembered,omitempty"`
}

func (s *Server) stageTool(ctx context.Context, req *mcp.CallToolRequest, a StageArgs) (*mcp.CallToolResult, StageOut, error) {
	acts, _, err := s.bind()
	if err != nil {
		return nil, StageOut{}, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, StageOut{}, err
	}
	project, err := entryOf(ix, a.Project)
	if err != nil {
		return nil, StageOut{}, err
	}
	if project.Kind != vault.Project {
		return nil, StageOut{}, fmt.Errorf("%s is a knowledge base and has no inbox; stage into a project that mounts it", project.Name)
	}
	plan, err := acts.StagePlan(project, a.Paths)
	if err != nil {
		return nil, StageOut{}, err
	}
	out := StageOut{Plan: plan}
	if a.DryRun {
		return nil, out, nil
	}
	res, remembered, err := acts.Stage(project, plan)
	if err != nil {
		return nil, StageOut{}, err
	}
	out.Result, out.Remembered = res, remembered
	return nil, out, nil
}
```

In `server.go`, `MCP()`, after the `repo` registration:

```go
	mcp.AddTool(server, &mcp.Tool{Name: "settings",
		Description: "Set an atlas setting and return them all: new_days (how long a vault counts as new) and repo_changes (the change policy a newly linked repository takes, commit or pr). With no arguments it only reads."}, s.settingsTool)
	mcp.AddTool(server, &mcp.Tool{Name: "stage",
		Description: "Copy files or folders from outside a project into its inbox, skipping what the project already captured or already holds; omit paths to stage what is new in the folders it staged from before. dry_run plans and copies nothing. Then ingest with the wiki-ingest skill."}, s.stageTool)
```

- [ ] **Step 4: Run every server test, then everything**

Run: `go test ./internal/mcpserver/ && make test && go vet ./... && gofmt -l internal/`
Expected: `ok` everywhere; no vet output; gofmt prints nothing.

- [ ] **Step 5: Commit**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com add internal/mcpserver && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -q -m "mcpserver: the settings and stage tools" && git log --oneline -1
```

---

### Task 10: The five skills, and the two hand-offs

Five new `SKILL.md` files in the `wiki` and `task` family style: frontmatter with `name` and a `description` that lists trigger phrases, then short sections. Two existing skills stop naming terminal commands.

**Files:**
- Create: `skills/atlas/SKILL.md`, `skills/atlas-project/SKILL.md`, `skills/atlas-knowledge/SKILL.md`, `skills/atlas-mount/SKILL.md`, `skills/atlas-repo/SKILL.md`
- Modify: `skills/wiki/SKILL.md`, `skills/wiki-ingest/SKILL.md`

**Interfaces:**
- Consumes: the seven tools' argument names from Tasks 4–9, exactly as the `json` tags spell them.

- [ ] **Step 1: Write `skills/atlas/SKILL.md`**

```markdown
---
name: atlas
description: "Orient in the whole atlas and route: every project and knowledge base, how they connect, what is wrong, the settings. Use for /atlas, my vaults, list vaults, what projects do I have, which knowledge bases, refresh the atlas, atlas settings, set new-days, default change policy, what is wrong with the atlas."
---

# The atlas

Tools: `atlas` and `settings` on the atlas MCP server. Both work from any
session: in a vault, in a project's repository, or in a folder the atlas
does not know.

## See what exists

1. Call `atlas`. It returns every vault with its kind, path, tags or scope,
   access, mounts with `effective` access, repositories with their change
   policy, members, the clusters that hold it, who mounts it, and its state;
   the folders the atlas cannot read, with a reason; and the settings.
2. Show a compact picture: projects with their mounts and repositories, then
   knowledge bases with who mounts them, then clusters with their members.
   Name each problem entry and its reason. Keep it short; the user asked for
   orientation, not a dump.
3. Say what needs attention: a mount whose `effective` is below its
   `access`, a member the scan lost, a repository with no folder.

## Route

| The user wants | Skill |
|---|---|
| Create, rename, retag, or forget a project | `atlas-project` |
| Create a knowledge base or a cluster; change scope, access, or members | `atlas-knowledge` |
| Mount, unmount, read or write, grant, revoke | `atlas-mount` |
| Link, clone, create, or unlink a repository; change how changes land | `atlas-repo` |
| Work inside one vault | `wiki` |

## Settings and refresh

- `settings` with `new_days` or `repo_changes` sets one and returns all;
  with no arguments it reads. Say what a value means before changing it:
  `new_days` is how long a vault counts as new in the view; `repo_changes`
  is the policy a newly linked repository takes, `commit` (on the current
  branch) or `pr` (a branch and a pull request).
- `atlas` with `refresh: true` reads every vault again, rewrites the
  registry, recreates each project's `kb/` links, and adopts repositories
  waiting under `repos/`. Run it after the user moved a folder by hand or
  cloned a repository into `repos/` themselves; report `changes`.

## What stays in the terminal

`claude-atlas doctor` (the installation check), `upgrade`, `recover`,
`setup`, `open-vault`, and `open-claude` are commands, not tools. Name the
command; do not run it through Bash unless the user asks.

Every write here is reversible or leaves the folder alone, so no plan preview
exists; the skill that writes states the change in one line and waits for
yes. Never edit `.claude-atlas.json`, `~/.claude-atlas/config.json`, or
`registry.json` with Write or Edit.
```

- [ ] **Step 2: Write `skills/atlas-project/SKILL.md`**

```markdown
---
name: atlas-project
description: "Create a project step by step, with suggestions and defaults: name, location, tags, which knowledge bases to mount, a repository now or later. Also rename, retag, or forget one. Use for new project, create a project, make a project for this repo, project in this repository, rename the project, tag the project, forget this vault."
---

# Create or change a project

Tools: `atlas`, `vault`, `mount`, `repo` on the atlas MCP server. A project
is where work happens: an inbox, tasks, questions, ideas, and repositories.
Its knowledge lives in the knowledge bases it mounts.

Call `atlas` first: the vaults directory, the knowledge bases and their
scopes, and the names already taken.

## Ask, in this order

Ask one question at a time. Offer the default; accept a yes.

1. **Name.** When the session sits in a git repository no project holds,
   suggest the repository's folder name. A name becomes the folder's name.
2. **Location.** Default: `<vaults dir>/projects/<name>`. When the session
   sits at a repository's top level, offer `in_repo`: the project goes to
   `REPO/atlas/`, its history lives in the repository's git, and the
   repository becomes the project's first repository. Otherwise a path the
   user names.
3. **Tags**, optional. One or two words each; the view groups projects by the
   first.
4. **Knowledge bases to mount.** Read each scope from `atlas` and suggest
   the ones that fit what the project is for. One mount goes on `vault`
   `create` as `mount`; more go through `mount` afterwards. A guarded
   knowledge base needs a grant too; say so and offer `atlas-mount`.
5. **A repository**, now or later: the one the session is in, a URL to clone,
   or a new one. Later is fine; `atlas-repo` does it any time.

## Confirm and create

State the whole change in one line, in the user's words:

> Create project `sensor-triage` at `~/Documents/Vaults/projects/sensor-triage`, tags `usc, fall`, mounting `ai-ml` for writing, linking `~/code/triage` (changes: commit)?

On yes: `vault` with `action: create`, `kind: project`, and the answers;
then `mount` for each further knowledge base; then `repo` with `link`,
`clone`, or `new`. Report each tool's result. Then say how to work there:
`claude-atlas open-claude <name>`, or `cd` into the vault and start `claude`.
A session in a linked repository reaches the project too.

If a tool refuses, say why in the tool's words and ask again for that one
answer; do not retry with a guess. A taken path means adopt or another name.

## Change a project

- Rename: `vault` with `action: edit`, `target`, and `name`. The folder moves
  with the name; a project at `REPO/atlas/` keeps its folder. Say so.
- Retag: `edit` with `tags`; an empty list clears them.
- Forget: `vault` with `action: forget`. The folder stays. A project inside
  the vaults directory cannot be forgotten, because the scan finds it there;
  the tool says so, and the answer is to move or delete the folder by hand.

Every question comes before the tool call, and the tool call comes after a
yes. Never make the folder or its files yourself.
```

- [ ] **Step 3: Write `skills/atlas-knowledge/SKILL.md`**

```markdown
---
name: atlas-knowledge
description: "Create a knowledge base or a cluster step by step: name, location, a scope written well, open or guarded, members. Also change scope, access, or members. Use for new knowledge base, create a knowledge base, new cluster, make a cluster, knowledge base for papers, change the scope, make it guarded, add a member, drop a member."
---

# Create or change a knowledge base

Tools: `atlas`, `vault`, `cluster` on the atlas MCP server. A knowledge base
holds sources, entities, and concepts, and nothing else: no inbox, no
tasks. Knowledge enters through a project that mounts it. A cluster is a
knowledge base with members; a project that mounts the cluster reaches every
member.

Call `atlas` first: the knowledge bases that exist, their scopes, and the
names already taken.

## Ask, in this order

1. **Name.** It becomes the folder's name.
2. **Location.** Default `<vaults dir>/knowledge/<name>`, or a path.
3. **Scope.** Two sentences: what it holds, and what it does not. The ingest
   skill reads every mounted scope to decide where a source belongs, so a
   scope that overlaps another's sends sources to the wrong place. Draft one
   from the user's words, read the existing scopes back, and adjust until
   each is distinct. For a cluster, the scope names the whole domain; the
   members' scopes name the parts.
4. **Access.** `open` (default): every project that mounts it may write.
   `guarded`: a project writes only with a grant, which `atlas-mount` gives.
   Suggest guarded for a knowledge base several projects share and one
   person curates.
5. **Members**, for a cluster: which knowledge bases it gathers. A member is
   a knowledge base, never a project and never another cluster.

## Confirm and create

One line, then yes:

> Create knowledge base `ai-ml` at `~/Documents/Vaults/knowledge/ai-ml`, guarded, scope "Machine learning: models, training, evaluation, agents. Not the projects that use them."?

On yes: `vault` with `action: create`, `kind: knowledge`, `scope`, `access`,
and for a cluster `members`. Report the result. Then say what comes next:
a project mounts it (`atlas-mount`), and sources reach it through that
project's inbox.

## Change a knowledge base

- Scope or access: `vault` with `action: edit`, `target`, and the field. An
  empty `scope` clears it. Changing to `guarded` cuts every mount without a
  grant to read; list the projects `atlas` shows under `mounted_by` before
  the user says yes.
- Members: `cluster` with `action: add` or `remove`. A removed member's
  knowledge base is untouched; the projects that mount the cluster lose it
  at their next refresh.
- Rename or forget: as in `atlas-project`, with the same tools and refusals.

The tool call comes after a yes. Never make the folder or its files yourself.
```

- [ ] **Step 4: Write `skills/atlas-mount/SKILL.md`**

```markdown
---
name: atlas-mount
description: "Change how a project reaches a knowledge base: mount, unmount, ask for read or write, grant or revoke a project on a guarded knowledge base, add or drop a cluster member. Explains effective access. Use for mount, unmount, read-only mount, make it writable, grant write, revoke, why can't I write to the knowledge base, effective access, add to cluster."
---

# Mounts, grants, and members

Tools: `atlas`, `mount`, `cluster` on the atlas MCP server.

Call `atlas` first and read the project's `mounts` and the knowledge base's
`access`, `grants`, and `mounted_by`. Three things decide what a project may
do in a knowledge base, and `effective` is the lesser of them:

- the mount's own `access`: what the project asks for, `read` or `write`;
- the knowledge base's `access`: `open` grants write to everyone, `guarded`
  grants what each project's grant says, or read with none;
- the grant, on a guarded knowledge base: `read` or `write` for one project.

So a mount that asks for write on a guarded knowledge base reads until the
knowledge base grants it write. When the user asks why a write was refused,
name which of the three is the one to change, and change only that.

## Actions

| The user wants | Call |
|---|---|
| Reach a knowledge base from a project | `mount` with `action: mount`, `project`, `knowledge`; `access: read` for read-only; `as` to name the folder under `kb/` |
| Stop reaching it | `mount` with `action: unmount`; the knowledge base is untouched |
| Ask for read or write | `mount` with `action: access`, `access` |
| Let a project write a guarded knowledge base | `mount` with `action: grant`, `knowledge`, `project`, `access: write` |
| Take that back | `mount` with `action: revoke`; a stale grant's id works when the project is gone |
| Gather a knowledge base in a cluster, or drop it | `cluster` with `action: add` or `remove` |

Mounting a cluster reaches every member; `atlas` shows such a mount with
`through` set to the cluster's name, and unmounting means unmounting the
cluster.

## Confirm

State the change in one line and wait for yes:

> Mount `ai-ml` on `sensor-triage` for writing? It is guarded, so the project reads until `ai-ml` grants it write; I can do that next.

After the call, report the mount's `effective` access, or the grants. If it
is still below what the user wanted, say which of the three to change and
offer to. On a grant for an `open` knowledge base, say it changes nothing
until the knowledge base is guarded.

An unmount, a revoke, and a member removal leave every folder alone. The
project's `kb/<name>` link goes with the mount; a folder there that holds
files is refused, and the tool says so.
```

- [ ] **Step 5: Write `skills/atlas-repo/SKILL.md`**

```markdown
---
name: atlas-repo
description: "Change a project's repositories: link a folder, clone a URL, create a new one, unlink one, set the remote or how changes land (pr or commit). Use for link repo, add a repository, clone into the project, new repo, unlink, change policy, use pull requests, commit directly, where do deliverables go."
---

# A project's repositories

Tools: `atlas`, `repo` on the atlas MCP server. A repository is where a
project's deliverables go, always a git repository; memory stays in the
vault. A repository under `<project>/repos/` belongs to that project by its
folder alone; one elsewhere is recorded in the atlas config.

Call `atlas` first and read the project's `repos` and the `repo_changes`
setting.

## Actions

| The user wants | Call |
|---|---|
| Link a folder that is a git repository | `repo` with `action: link`, `project`, `path`; `init: true` when it is a plain folder, which makes one commit of what it holds |
| Clone a URL into the project | `repo` with `action: clone`, `url`; `at` for a folder other than `repos/` |
| Start a new repository | `repo` with `action: new`, `name`; `at` as above |
| Drop one from the project | `repo` with `action: unlink`, `name`; the folder stays, and one still under `repos/` cannot be unlinked, because the folder decides |
| Set the remote, the folder, or the policy | `repo` with `action: edit`, `name`, and `remote`, `path`, or `changes` |

## The change policy

`changes` is how a session lands work in that repository. `commit`: on the
current branch. `pr`: a branch and a pull request. Every repository records
it; the atlas default is the `repo_changes` setting. When the user is unsure,
ask who reviews the work: nobody, `commit`; someone, `pr`. The session hook
and the `repos` tool tell every later session which applies.

## Confirm

One line, then yes:

> Link `~/code/triage` to `sensor-triage` as `triage`, changes: commit?

After the call, report the repository's name, path, remote, and policy from
the tool's result. A link of a plain folder without `init` is refused; ask
before passing `init`, because it makes a commit. A project at `REPO/atlas/`
already has its host repository first; `link`, `new`, and `unlink` refuse
its name, and `edit` sets its policy.

Never run git in a repository to link it; the tool does what is needed.
```

- [ ] **Step 6: The hand-offs in `wiki` and `wiki-ingest`**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && python3 - <<'EOF'
import io
def edit(p, pairs):
    s=io.open(p,encoding='utf-8').read()
    for old,new in pairs:
        assert old in s, (p, old[:50])
        s=s.replace(old,new,1)
    io.open(p,'w',encoding='utf-8').write(s)

edit('skills/wiki/SKILL.md', [
('''If `status` fails because no vault is selected, stop and tell the user to run one
of these in a terminal, then start a session inside the vault:

```bash
claude-atlas new-project               # create a project, step by step
claude-atlas new-knowledge             # create a knowledge base, step by step
claude-atlas adopt /path/to/vault      # an existing Obsidian or claude-obsidian vault
```
''',
'''If `status` fails because no vault is selected, the session is outside every
vault and every project's repository. Hand off: `atlas-project` creates a
project, `atlas-knowledge` a knowledge base, and either adopts an existing
Obsidian vault; `atlas` shows what exists. A new vault's session starts with
`claude-atlas open-claude NAME`, or `cd` there and `claude`.
'''),
('''| Mount, unmount, grant, or revoke access to a knowledge base | the CLI (`claude-atlas mount …`), not a tool |''',
'''| See the whole atlas, refresh it, or change a setting | `atlas` |
| Create, rename, retag, or forget a project | `atlas-project` |
| Create a knowledge base or a cluster, or change its scope, access, or members | `atlas-knowledge` |
| Mount, unmount, grant, or revoke access to a knowledge base | `atlas-mount` |
| Link, clone, create, or unlink a repository, or change how changes land | `atlas-repo` |'''),
])

edit('skills/wiki-ingest/SKILL.md', [
('''No network is needed. If the user gives a URL, ask them to save the page into
`inbox/` (or paste the text). Do not fetch it yourself.''',
'''No network is needed. If the user gives a URL, ask them to save the page into
`inbox/` (or paste the text). Do not fetch it yourself. A file or folder
outside the vault enters through the `stage` tool: it copies what is new into
`inbox/` and skips what the project already captured. Never copy a file into
`inbox/` with Write.'''),
('''"`<project> mounts <kb> read-only`". Stop before capturing and tell the user
which command to run in a terminal:

- the mount's own `access` is `read`: `claude-atlas mount <project> <kb>`
  without `--read`;
- `access` is `write` but `effective` is `read`: the knowledge base is guarded,
  so `claude-atlas grant <kb> <project> --write`.
''',
'''"`<project> mounts <kb> read-only`". Stop before capturing and hand off to
the `atlas-mount` skill, which reads `effective` and changes the right thing:
the mount's own `access` when it is `read`, or a grant when the knowledge
base is guarded.
'''),
])
print("edited")
EOF
grep -n "atlas-mount\|stage" skills/wiki-ingest/SKILL.md | head && grep -n "atlas" skills/wiki/SKILL.md | head
```

Expected: `edited`, then the new lines.

- [ ] **Step 7: Read every new skill once for the writing guide, then commit**

Check each new `SKILL.md` for the words the guide bans (`flag`, `genuine`, `honest`, `shape`, `load bearing`, `judgement call`):

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && grep -niE "\bflag|genuine|honest|\bshape|load bearing|judgement" skills/atlas*/SKILL.md; echo "exit $?"
```

Expected: no matches, `exit 1`.

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com add skills/ && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -q -m "skills: atlas, atlas-project, atlas-knowledge, atlas-mount, atlas-repo; wiki and wiki-ingest hand off" && git log --oneline -1
```

---

### Task 11: Docs and 1.3.0

**Files:**
- Modify: `CLAUDE.md`, `docs/usage.md`, `docs/core-design.md`, `README.md`, `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`

- [ ] **Step 1: Apply every edit**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && python3 - <<'EOF'
import io
def edit(p, pairs):
    s=io.open(p,encoding='utf-8').read()
    for old,new in pairs:
        assert old in s, (p, old[:50])
        s=s.replace(old,new,1)
    io.open(p,'w',encoding='utf-8').write(s)

edit('CLAUDE.md', [
('''| Tasks: pages, ledger, skills, repos reaching the vault | `docs/tasks-design.md` |''',
'''| Tasks: pages, ledger, skills, repos reaching the vault | `docs/tasks-design.md` |
| The atlas tools and the `atlas` skills | `docs/superpowers/specs/2026-09-16-atlas-tools-design.md` |'''),
('''## Two layers, one backend

`view` is the whole atlas as one screen; every command is one thing from it.
The rule: an action lives once, as a function in `internal/vaults`,
`internal/refresh`, `internal/vault`, or `internal/txn`. The CLI exposes it as
one subcommand. The TUI reaches it through `tui.Hooks`, which `cli.hooks`
builds in one place. The TUI is a subset of the CLI, never the reverse: a new
key gets a command in the same change, and `docs/usage.md` carries the table
that maps them.''',
'''## Three layers, one backend

`view` is the whole atlas as one screen; every command is one thing from it;
the atlas tools (`atlas`, `vault`, `mount`, `cluster`, `repo`, `settings`,
`stage`) are the same things from a Claude Code session. The rule: an action
lives once, as a function in `internal/vaults`, `internal/refresh`,
`internal/vault`, `internal/txn`, or `internal/capture`. The CLI exposes it
as one subcommand. The TUI and the tools reach it through `actions.Atlas`,
which `actions.Bind` builds in one place. The TUI and the tools are subsets
of the CLI, never the reverse: a new key or a new tool gets a command in the
same change, and `docs/usage.md` carries the table that maps them.'''),
('''internal/cli/           argument parsing and one method per subcommand''',
'''internal/cli/           argument parsing and one method per subcommand
internal/actions/       every atlas action as one struct of functions, and Bind, the one place it is built'''),
('''- `git` is a runtime requirement. `python3` is not. macOS and Linux only.''',
'''- The atlas tools call `actions.Bind` once per call over a config loaded for
  that call, and never use `plan`/`apply`: nothing they do deletes user data,
  and the skill's one-line statement is the preview. `atlas` without
  `refresh` writes nothing; every write tool scans afresh to resolve names.
- `git` is a runtime requirement. `python3` is not. macOS and Linux only.'''),
('''1.0.0 is the first v2 release; 1.1.0 is the view with tabs; 1.2.0 is
clusters.''',
'''1.0.0 is the first v2 release; 1.1.0 is the view with tabs; 1.2.0 is
clusters; 1.3.0 is the atlas tools and skills.'''),
])

s=io.open('docs/usage.md',encoding='utf-8').read()
start=s.index('| In `view` | Command |')
end_marker='| — (no `view` key; run from a `lint` finding) | `stub VAULT [TITLE...] [--type T]` |\n'
end=s.index(end_marker)+len(end_marker)
table='''| In `view` | Command | Tool, from a Claude Code session |
|---|---|---|
| `n` new project, `N` new knowledge base | `new-project NAME [--in REPO]`, `new-knowledge NAME` | `vault` create |
| `a` adopt a vault | `adopt PATH` | `vault` adopt |
| Enter on a vault | `show NAME` | `atlas` |
| `o` open in Obsidian | `open-vault NAME` | — |
| `c` start Claude Code | `open-claude NAME` | — |
| `i` ingest sources, on a project | `ingest NAME [PATH...]` | `stage`, then the `wiki-ingest` skill |
| `t` tasks on a project, `p` plant, `c` continue | `tasks NAME`, `plant NAME TEXT`, `open-claude NAME --task ID` | `tasks`, `plant` |
| `T` every project's tasks | `tasks` | — |
| `l` repositories on a project: `n` new, `a` link, `e` edit, `u` unlink | `new-repo NAME REPO`, `link NAME PATH`, `edit-repo NAME REPO`, `unlink NAME REPO`, `repos [NAME]` | `repo` |
| `e` edit a vault, `s` save | `edit NAME --…` | `vault` edit |
| `e` then `r` forget | `remove NAME` | `vault` forget |
| `m` mounts: on a project `a` mount, `w` `r` ask, `u` unmount; on a knowledge base `w` `r` grant, `x` revoke, `a` grant | `mount PROJECT KB [--read] [--as NAME]`, `unmount PROJECT KB\\|NAME`, `grant KB PROJECT --write\\|--read`, `revoke KB PROJECT\\|ID` | `mount` |
| `C` new cluster; `M` members, on a knowledge base: `a` add, `x` drop | `new-cluster NAME`, `cluster NAME`, `cluster add NAME KB`, `cluster remove NAME KB\\|ID` | `vault` create with `members`; `cluster` |
| `R` refresh | `refresh` | `atlas` with `refresh` |
| — | `config KEY VALUE` | `settings` |
| `←` `→` switch tabs; `h` shows every key | — | — |
| — (no `view` key; run from a `lint` finding) | `stub VAULT [TITLE...] [--type T]` | `stub` |
'''
s=s[:start]+table+s[end:]
old='''(`claude-atlas view` says the same explicitly). Everything it does is also
one command, so scripts and muscle memory both work:'''
new='''(`claude-atlas view` says the same explicitly). Everything it does is also
one command, and one tool from a Claude Code session, so scripts, muscle
memory, and the `atlas` skills all work:'''
assert old in s
s=s.replace(old,new,1)
io.open('docs/usage.md','w',encoding='utf-8').write(s)

edit('docs/core-design.md', [
('''The model reads pages with its own Read, Grep, and Glob tools. A `search`
tool with BM25 ranking is a later addition, not part of this design.''',
'''The model reads pages with its own Read, Grep, and Glob tools. A `search`
tool with BM25 ranking is a later addition, not part of this design.

Added in 1.3.0, the atlas tools, bound to the same functions as the view
(`docs/superpowers/specs/2026-09-16-atlas-tools-design.md`):

| Tool | Reads or writes | Returns |
|---|---|---|
| `atlas` | reads; with `refresh`, rewrites the registry | every vault with its state, the problems, the settings |
| `vault` | writes: create, adopt, edit, forget | the vault as the atlas sees it |
| `mount` | writes: mount, unmount, access, grant, revoke | the mount with its effective access, or the grants |
| `cluster` | writes: add, remove | the members |
| `repo` | writes: link, new, clone, unlink, edit | the repository |
| `settings` | writes when given a value | the settings |
| `stage` | writes into `inbox/` unless `dry_run` | the plan, and what was copied |'''),
])

edit('README.md', [
('''gathers others; a project mounts the cluster once and reaches every member.''',
'''gathers others; a project mounts the cluster once and reaches every member.
Every change here is also possible from a Claude Code session: the `atlas`,
`atlas-project`, `atlas-knowledge`, `atlas-mount`, and `atlas-repo` skills
create vaults, mount, grant, and link repositories through tools that call
the same functions the view calls.'''),
])
print("edited")
EOF
perl -pi -e 's/"version": "1\.2\.0"/"version": "1.3.0"/g' .claude-plugin/plugin.json .claude-plugin/marketplace.json
grep -n '"version"' .claude-plugin/plugin.json .claude-plugin/marketplace.json
```

Expected: `edited`, then three lines each showing `1.3.0`.

- [ ] **Step 2: Check the banned words in the changed docs, then commit**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git diff -U0 CLAUDE.md docs/usage.md docs/core-design.md README.md | grep '^+' | grep -niE "\bflag|genuine|honest|\bshape|load bearing|judgement"; echo "exit $?"
```

Expected: no matches, `exit 1`.

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com add CLAUDE.md docs/usage.md docs/core-design.md README.md .claude-plugin/ && git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -q -m "docs: three layers, one backend; 1.3.0" && git log --oneline -1
```

---

### Task 12: Verify end to end

The tests prove the tools; this proves the plugin: the tools appear in a session, the skills trigger, and a guided create lands on disk. Everything runs against a scratch atlas home, never `~/.claude-atlas`.

**Files:** none changed. If a step fails, fix it in the task that owns the code and re-run from Step 1.

- [ ] **Step 1: The whole suite, vet, format, build**

```bash
cd /Users/nathanaday/SoftwareProjects/claude-atlas && make test && go vet ./... && gofmt -l internal/ cmd/ && make install && claude-atlas version
```

Expected: every package `ok`; nothing from vet or gofmt; the version the build stamps.

- [ ] **Step 2: A scratch atlas**

```bash
S=/private/tmp/claude-501/-Users-nathanaday-SoftwareProjects-claude-atlas/1f02bef0-7321-4284-a48a-6d700030bf78/scratchpad/atlas-tools && rm -rf "$S" && mkdir -p "$S" && claude-atlas --home "$S/home" -y setup --no-plugin --vaults-dir "$S/Vaults" --first-vault welcome && claude-atlas --home "$S/home" -y new-knowledge ai-ml --scope "Machine learning: models, training, evaluation." && claude-atlas --home "$S/home" list
```

Expected: `welcome` (project) and `ai-ml` (knowledge base) listed.

- [ ] **Step 3: The tools are served and the `atlas` skill reads them**

```bash
S=/private/tmp/claude-501/-Users-nathanaday-SoftwareProjects-claude-atlas/1f02bef0-7321-4284-a48a-6d700030bf78/scratchpad/atlas-tools && cd "$S" && CLAUDE_ATLAS_HOME="$S/home" claude --plugin-dir /Users/nathanaday/SoftwareProjects/claude-atlas -p "Use the atlas skill. What vaults exist, and what does each mount?" --allowedTools "mcp__plugin_claude-atlas_atlas__*,Skill,Read,Grep,Glob"
```

Expected: an answer that names `welcome` and `ai-ml`, with the knowledge base's scope, and no mention of running a terminal command. The session started in a folder that is not a vault, and the tool still answered.

- [ ] **Step 4: A guided create lands on disk**

```bash
S=/private/tmp/claude-501/-Users-nathanaday-SoftwareProjects-claude-atlas/1f02bef0-7321-4284-a48a-6d700030bf78/scratchpad/atlas-tools && cd "$S" && CLAUDE_ATLAS_HOME="$S/home" claude --plugin-dir /Users/nathanaday/SoftwareProjects/claude-atlas -p "Use the atlas-project skill to create a project named triage, tags usc, mounting ai-ml, no repository. Take the defaults for everything else. I have read your one-line summary and my answer is yes; do not wait for another answer." --allowedTools "mcp__plugin_claude-atlas_atlas__*,Skill,Read,Grep,Glob" && claude-atlas --home "$S/home" show triage
```

Expected: `show triage` prints the project at `$S/Vaults/projects/triage` with tag `usc` and the mount `ai-ml` (effective write).

- [ ] **Step 5: The in-vault skills still hand off, not to the terminal**

```bash
S=/private/tmp/claude-501/-Users-nathanaday-SoftwareProjects-claude-atlas/1f02bef0-7321-4284-a48a-6d700030bf78/scratchpad/atlas-tools && cd "$S/Vaults/projects/triage" && CLAUDE_ATLAS_HOME="$S/home" claude --plugin-dir /Users/nathanaday/SoftwareProjects/claude-atlas -p "Use the wiki skill. I want to make ai-ml read-only for this project. Which skill handles that, and what would it call? Do not change anything." --allowedTools "mcp__plugin_claude-atlas_atlas__*,Skill,Read,Grep,Glob"
```

Expected: the answer names `atlas-mount` and the `mount` tool with `action: access`, `access: read`; it does not name `claude-atlas mount`.

- [ ] **Step 6: Report**

State what passed and what did not, with the output. Then stop: merging into `main` and releasing (`claude plugin marketplace update`, `claude plugin update`) are the user's call; ask before either.
