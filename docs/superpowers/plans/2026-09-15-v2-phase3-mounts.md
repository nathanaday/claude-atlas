# v2 Phase 3: Mounts and Access — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A project mounts knowledge bases as symlinks under `kb/`, a knowledge base grants access, the tools enforce the effective access, knowledge enters a knowledge base through a project session with provenance, `route` and lint see through mounts, and the session hook names the mounts.

**Architecture:** `vault` gains the advisory lock (shared by `txn` and `UpdateConfig`). `vaults` gains `Mount`, `Unmount`, `Grant`, `Revoke`, and `EnsureMounts` (the symlinks), all through `vault.UpdateConfig`. `txn` reserves `kb/` and `repos/`; the guard hook denies `kb/`. `ledger` records `via` (the project a source came through); `capture` can capture a project's inbox file into a knowledge base. The MCP server learns the session's project and the target's effective access, and applies it in `plan`, `capture`, and `stub`; `route` answers across mounts; a `mounts` tool lists them. Lint treats the symlinks under `kb/` as link targets. The CLI gains `mount`, `unmount`, `grant`, `revoke`; `doctor` and `refresh` recreate missing symlinks. The hook prints one line per mount. The TUI's `m`/`u`/`g`/`G` keys are phase 4.

**Tech Stack:** Go 1.24.2, the MCP go-sdk v1.4.0, git, symlinks (macOS and Linux).

**Spec:** `docs/v2-design.md`: "Mounts", "Access", "Knowledge enters through a project", "Sessions", "Tools", "Operations", "Lint", and phase 3 in "Phases". Phases 1 and 2 are on `main`.

## Global Constraints

- No new dependencies.
- The identity file never holds a path. A mount is `{id, name, access}` in the project's identity file; the symlink `<project>/kb/<name> -> <knowledge base>/wiki` is local state, ignored by git, recreated from the identity file and the registry.
- A knowledge base never learns who mounts it and never links a project.
- A vault's own facts change only through `vault.UpdateConfig`; `mount`, `unmount`, `grant`, `revoke` use it. `vault.Init`, `Adopt`, `Upgrade`, `UpdateConfig`, `txn.Apply`, and the symlink creation under `kb/` are the only code that writes into a vault. Refresh writes nothing a vault's git tracks; it may recreate the ignored `kb/` symlinks.
- Effective access is the lesser of the mount's request and the knowledge base's grant (`registry.Effective`, `registry.GrantedAccess`): `open` grants `write`; `guarded` grants what the grant says, else `read`.
- `kb/` and `repos/` are reserved everywhere in `txn.allowed`: no plan writes under them. The guard hook denies Write and Edit under `kb/`; it allows them under `repos/` (a repository is where Claude works).
- One vault per operation. A knowledge base has no inbox; a source captured into a knowledge base comes from a project's inbox and its ledger record carries `via`.
- `ingest` and `save` into a knowledge base need a project session whose mount of that knowledge base is effectively `write`; a read-only mount refuses them; a knowledge base session (no project) refuses them and names the projects that mount it. Maintenance kinds (`repair`, `fold`, `markdown`, `canvas`, `base`, `config`, `stub`) run in a knowledge base session.
- Lint stays read-only, offline, and idempotent. In a project it treats each symlink under `kb/` as a set of link targets prefixed `kb/<name>/`; findings are about the project's own pages only. A bare link resolves in the project's own `wiki/` first, then in the mounts; a name in two mounts is ambiguous. The duplicate-basename check spans the project's wiki and its mounts, except the root pages (`index`, `log`, `hot`, `overview`) and folder index pages. A link that resolves in a mount is not a wanted page; the near-match check covers mount names and aliases.
- The atlas tools return real paths: a mount's `path` is the knowledge base's `wiki/`, never `kb/<name>`.
- Tests never touch a real `~/.claude-atlas`, never install a plugin, and skip when `git` is missing.
- Every task ends with `go build ./... && go vet ./... && go test ./...` passing.
- Commits: author `nathanaday <nraday1221@gmail.com>` (`-c user.name=nathanaday -c user.email=nraday1221@gmail.com`); no `Co-Authored-By`; subject `area: what changed`. Stage files by name; never stage `.superpowers/`.
- Comments are one-line doc comments in the style of the surrounding code. Prose follows the user's writing guide: short sentences, plain words, no metaphors.

## File map

| File | Change |
|---|---|
| `internal/vault/lock.go` (moved from `internal/txn/lock.go`), `vault.go` | `vault.Lock`; `UpdateConfig` takes it |
| `internal/vaults/mounts.go`, `mounts_test.go` | `Mount`, `Unmount`, `Grant`, `Revoke`, `EnsureMounts`, `MountLink` |
| `internal/txn/txn.go`, `txn_test.go`, `internal/hooks/hooks.go`, `hooks_test.go` | `kb/` and `repos/` reserved; the guard denies `kb/` |
| `internal/ledger/ledger.go`, `ledger_test.go`, `internal/capture/capture.go`, `capture_test.go` | `Via`; `CaptureFrom` |
| `internal/mcpserver/server.go`, `server_test.go`, `internal/txn/stub.go`, `stub_test.go` | the session's project, the target's access, `plan`/`capture`/`stub` gates, `StubInto`, the `mounts` tool, `status` mounts |
| `internal/vault/route.go`, `vault_test.go`, `internal/mcpserver/server.go` | `FindPage`; `route` across mounts |
| `internal/lint/lint.go`, `lint_test.go` | mounts as targets |
| `internal/cli/cli.go`, `cli_test.go` | `mount`, `unmount`, `grant`, `revoke`; `doctor` and `refresh` recreate symlinks |
| `internal/hooks/hooks.go`, `hooks_test.go` | mount lines; mounted-by line |
| `CLAUDE.md`, `docs/usage.md`, `docs/v2-design.md` | the new commands, the refresh exception, the Obsidian check |

## Symlink facts, checked on this machine on 2026-09-15

`~/Documents` is not an iCloud zone on the author's machine (`brctl status` reports no client zone; only `Desktop` is linked into CloudDocs), and a symlink under `~/Documents/Vaults` survived unchanged. The Obsidian follow-symlink check needs a live Obsidian and is Task 10's manual step.

---

### Task 1: The vault lock moves into `vault`; `UpdateConfig` takes it

**Files:**
- Create: `internal/vault/lock.go` (from `internal/txn/lock.go`)
- Delete: `internal/txn/lock.go`
- Modify: `internal/vault/vault.go`, `internal/txn/txn.go`, `internal/vault/vault_test.go`

**Interfaces:**
- Produces: `func Lock(root string) (func(), error)` in `vault` — the advisory lock on `<root>/.vault-meta/lock`, exactly the old `txn.lock` with a root instead of a `*Vault`. `txn` calls `vault.Lock(v.Root)` where it called `lock(v)`. `UpdateConfig` takes the lock around read, validate, write, commit.

- [ ] **Step 1: Write the failing test**

Add to `internal/vault/vault_test.go`:

```go
func TestLockIsExclusiveAndUpdateConfigTakesIt(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "p")
	if _, err := Init(root, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	unlock, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- UpdateConfig(root, "tag", now, func(c *Config) error { c.Tags = []string{"x"}; return nil })
	}()
	select {
	case err := <-done:
		t.Fatalf("UpdateConfig ran while the vault was locked: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	v, _ := Open(root)
	if len(v.Config.Tags) != 1 {
		t.Fatalf("tags %+v", v.Config.Tags)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vault/ -run TestLockIsExclusiveAndUpdateConfigTakesIt`
Expected: compile error `undefined: Lock`.

- [ ] **Step 3: Move the lock**

`git mv internal/txn/lock.go internal/vault/lock.go`; change its package to `vault`, its signature to `func Lock(root string) (func(), error)`, `v.Path(MetaDir)` to `filepath.Join(root, MetaDir)`, and the error text to name `root`. In `txn.go`, replace every `lock(v)` with `vault.Lock(v.Root)`. In `UpdateConfig`, take the lock first: `unlock, err := Lock(root); if err != nil { return err }; defer unlock()`, before `Open`.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass; the txn tests that exercised the lock (`TestApplyCommitsOneOperation` and friends) still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/lock.go internal/txn/lock.go internal/vault/vault.go internal/vault/vault_test.go internal/txn/txn.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "vault: the advisory lock lives in vault and UpdateConfig takes it"
```

---

### Task 2: Mounts and grants

**Files:**
- Create: `internal/vaults/mounts.go`, `internal/vaults/mounts_test.go`

**Interfaces:**
- Consumes: `registry.Entry` (`ID`, `Kind`, `Name`, `Path`, `Mounts`, `Access`, `Grants`, `KbDir`, `Wiki`), `registry.Scan`, `vault.UpdateConfig`, `vault.Mount`, `vault.Grant`, `vault.ValidAccess`, `links.CleanName`.
- Produces:

```go
// MountLink is the symlink a mount makes: <project>/kb/<name> -> <knowledge base>/wiki.
func MountLink(project registry.Entry, name string) string // = project.KbDir(name)

// Mount records kb in project's identity file with the requested access (read or
// write; write by default) under name (kb's name by default, cleaned), and creates the
// symlink. It refuses a knowledge base entry as project, a project entry as kb, a name
// already in use, and a knowledge base already mounted.
func Mount(project, kb registry.Entry, access, name string, now time.Time) (vault.Mount, error)

// Unmount removes the mount named by kb's id or the mount name from project's identity
// file and removes the symlink. The knowledge base is untouched.
func Unmount(project registry.Entry, target string, now time.Time) error

// Grant records project's access on kb: read or write. A knowledge base that is open
// stays open; the grant applies when it is guarded. It refuses a project entry as kb.
func Grant(kb, project registry.Entry, access string, now time.Time) error

// Revoke removes project's grant from kb.
func Revoke(kb, project registry.Entry, now time.Time) error

// EnsureMounts creates every symlink project's mounts need and removes symlinks under
// kb/ that no mount names. It reports what it created and removed. A mount whose
// knowledge base the index does not hold is left alone and returned in missing.
func EnsureMounts(project registry.Entry, ix *registry.Index) (created, removed, missing []string, err error)
```

Rules: the symlink target is the knowledge base's absolute `wiki/` path (`kb.Wiki()`); `kb/` is created with `0o755` when absent; an existing correct symlink is left; an existing wrong symlink is replaced; a real folder at `kb/<name>` is an error ("… is a folder, not a mount; move it away"). `Unmount` removes only a symlink. `EnsureMounts` uses `ix.ByID` to find each mount's knowledge base.

- [ ] **Step 1: Write the failing tests**

`mounts_test.go`, with a helper that makes a project and two knowledge bases under a temp vaults dir (`vault.Init`), builds `cfg := &home.Config{VaultsDir: dir}`, scans, and returns the entries:

- `TestMountCreatesTheSymlinkAndRecordsTheMount`: `Mount(p, kb, "", "", now)` → identity has `{kb.ID, "ai-ml", "write"}`; `os.Readlink(p.KbDir("ai-ml"))` is `kb.Wiki()`; a page written into the knowledge base's wiki (`os.WriteFile`) is readable through `p.KbDir("ai-ml")/...`; the project's git is clean (`kb/` is ignored); mounting again → error "already"; `Mount(p, kb2, "read", "robots", now)` → second mount with name `robots` and access `read`; `Mount(kb, p, ...)` → error "knowledge base" (as project); `Mount(p, p, ...)` → error "not a knowledge base"; `Mount(p, kb2, "sometimes", "", now)` → error.
- `TestUnmountRemovesTheSymlinkAndTheMount`: by name and by id; the target folder still holds its pages; unmounting an unknown name → error.
- `TestGrantAndRevoke`: `Grant(kb, p, "read", now)` → `kb` identity `grants` has `{p.ID, p.Name, "read"}`; `Grant` again with `write` replaces it; `Grant(kb, p, "open", now)` → error; `Revoke(kb, p, now)` → gone; `Revoke` again → error "no grant"; after `Grant` on a guarded kb, `registry.Scan` shows the project's mount `Effective` follows the grant.
- `TestEnsureMountsRecreatesAndPrunes`: mount two; delete one symlink; add a stray symlink `kb/stray`; `EnsureMounts` → created `[ai-ml]`, removed `[stray]`; a mount to an id not in the index → `missing` names it and nothing else changes; a real folder at `kb/<name>` → error.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vaults/ -run 'TestMount|TestUnmount|TestGrant|TestEnsureMounts'`
Expected: compile errors for the new names.

- [ ] **Step 3: Write `mounts.go`**

Implement the interfaces above. `Mount` and `Unmount` run `vault.UpdateConfig` on the project (summaries `mount <name>`, `unmount <name>`), then the symlink work; `Grant` and `Revoke` run it on the knowledge base (`grant <project name> <access>`, `revoke <project name>`). Uniqueness of mount ids and names is also checked by `UpdateConfig` (phase 2); `Mount`'s own check gives the better message.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/vaults/mounts.go internal/vaults/mounts_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "vaults: a project mounts a knowledge base as a symlink; a knowledge base grants access"
```

---

### Task 3: `kb/` and `repos/` are reserved; the guard denies `kb/`

**Files:**
- Modify: `internal/txn/txn.go`, `internal/txn/txn_test.go`
- Modify: `internal/hooks/hooks.go`, `internal/hooks/hooks_test.go`
- Modify: `internal/vault/vault.go` (two constants)

**Interfaces:**
- Produces: `vault.KbDir = "kb"`, `vault.ReposDir = "repos"` (and `registry` uses them instead of its private constants). `txn.allowed` refuses any path under `kb/` or `repos/` for every kind ("kb/ holds mounted knowledge bases; change their pages in the knowledge base's own operation" / "repos/ holds repositories; they are not the vault's files"). `hooks.Guard` denies a Write or Edit whose path, relative to the vault it sits in, starts with `kb/`, with the reason "pages of a mounted knowledge base change only through the atlas MCP tools, in the knowledge base's own operation"; a path under `repos/` is allowed (the guard returns nothing).

- [ ] **Step 1: Write the failing tests**

In `txn_test.go`, add two cases to `TestPrepareValidates`'s table: `{"kb", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: "kb/ai-ml/concepts/A.md", Mode: Create, Content: mkpage("A", "")}}}, "mounted knowledge base"}` and `{"repos", Request{Kind: Repair, Summary: "x", Writes: []Write{{Path: "repos/code/README.md", Mode: Create, Content: []byte("x")}}}, "not the vault's files"}`.

In `hooks_test.go` `TestGuard`, add: a write to `<vault>/kb/ai-ml/concepts/A.md` is denied with a reason containing "mounted knowledge base"; a write to `<vault>/repos/code/main.go` produces no output. (Look at how the existing cases build the JSON input and assert on the decision.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/txn/ -run TestPrepareValidates; go test ./internal/hooks/ -run TestGuard`
Expected: FAIL: the kb and repos writes are accepted; the guard allows `kb/`.

- [ ] **Step 3: Reserve the paths**

In `vault.go`, add `KbDir = "kb"` and `ReposDir = "repos"` next to `InboxDir`; in `registry.go`, replace `reposDir` and `kbDir` with them. In `txn.allowed`, after the knowledge-base block and before the reserved-path `switch`, add:

```go
	if under(vault.KbDir) {
		return fmt.Errorf("%s/ holds mounted knowledge bases; change their pages in the knowledge base's own operation: %s", vault.KbDir, p)
	}
	if under(vault.ReposDir) {
		return fmt.Errorf("%s/ holds repositories; they are not the vault's files: %s", vault.ReposDir, p)
	}
```

In `hooks.Guard`'s `switch`, add before the `.git/` case: `case strings.HasPrefix(rel, vault.KbDir+"/"): reason = "pages of a mounted knowledge base change only through the atlas MCP tools, in the knowledge base's own operation"`.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/vault.go internal/registry/registry.go internal/txn/txn.go internal/txn/txn_test.go internal/hooks/hooks.go internal/hooks/hooks_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "txn, hooks: kb/ and repos/ are reserved; the guard denies writes into a mount"
```

---

### Task 4: Provenance: `via` in the ledger; capture into a knowledge base from a project's inbox

**Files:**
- Modify: `internal/ledger/ledger.go`, `internal/ledger/ledger_test.go`
- Modify: `internal/capture/capture.go`, `internal/capture/capture_test.go`

**Interfaces:**
- Produces in `ledger`:

```go
// Via names the project a source came through into a knowledge base. Provenance, not a link.
type Via struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
```

`Source` gains `Via *Via` (`via,omitempty`); `Update` gains `Via *Via` (`via,omitempty`); `Apply` copies a non-nil `Update.Via` onto the record. `Parse` keeps unknown fields as today.

- Produces in `capture`:

```go
// CaptureFrom copies files from source's inbox into target's raw store and records them
// in target's ledger with via, as one commit in target. source's inbox is untouched.
func CaptureFrom(target, source *vault.Vault, paths []string, via ledger.Via, now time.Time) (*Result, error)
```

`Capture(v, paths, now)` stays as the same-vault case (a project capturing its own inbox). `CaptureFrom` resolves `paths` against `source` (`resolveInbox(source, arg)`), reads the bytes from `source`, and writes into `target` with `req.Sources[i].Via = &via`. It refuses when `target.Config.Kind != vault.Knowledge` ("capture into a project from its own inbox with capture") and when `source.Config.Kind != vault.Project`.

- [ ] **Step 1: Write the failing tests**

`ledger_test.go`: a round trip: `Apply` with `Via{ID: "p1", Name: "cs566"}` on a new record; `Encode` contains `"via": {`; `Parse` of that output gives the record back with `Via` set; an `Update` without `Via` on an existing record keeps the old `Via`.

`capture_test.go`: `TestCaptureFromAProjectIntoAKnowledgeBase`: a project with `inbox/paper.md`, a knowledge base; `CaptureFrom(kb, project, []string{"paper.md"}, ledger.Via{ID: project.Config.ID, Name: "cs566"}, now)` → the file is under `kb/.raw/captured/<sha>.md`, `kb`'s ledger has the record with `Via.Name == "cs566"`, the kb has one new commit whose subject starts with `capture: capture paper.md`, the project's inbox still holds `paper.md` and the project has no new commit; a second call reports `AlreadyCaptured`; `CaptureFrom(project, project, …)` → error "capture"; `CaptureFrom(kb, kb, …)` → error "project".

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ledger/ ./internal/capture/ 2>&1 | head`
Expected: compile errors `undefined: Via`, `undefined: CaptureFrom`.

- [ ] **Step 3: Write the code**

As described. Factor the loop body of `Capture` so both functions share it: a private `captureInto(target *vault.Vault, read func(rel string) ([]byte, error), resolve func(arg string) (string, error), via *ledger.Via, paths, now)`; `Capture` passes `target`'s own readers; `CaptureFrom` passes `source`'s.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/ledger/ledger.go internal/ledger/ledger_test.go internal/capture/capture.go internal/capture/capture_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "ledger, capture: a source captured into a knowledge base records the project it came through"
```

---

### Task 5: The server knows the session's project and the target's access

**Files:**
- Modify: `internal/mcpserver/server.go`, `internal/mcpserver/server_test.go`
- Modify: `internal/txn/stub.go`, `internal/txn/stub_test.go`

**Interfaces:**
- Consumes: `registry.Scan`, `registry.Index.ByID`, `registry.Index.ByPath`, `registry.Effective`, `registry.GrantedAccess`, `registry.Entry.Mounts`/`MountedBy`, `capture.CaptureFrom`, `ledger.Via`, `vault.Lock`.
- Produces in `mcpserver`:

```go
// session is what the server knows about one tool call's vaults: the vault the call
// acts on, the session's own project (nil in a knowledge base session or with no atlas),
// and, when the target is a knowledge base mounted by that project, the mount's
// effective access.
type session struct {
	target  *vault.Vault
	project *registry.Entry // the session's project, from the working directory or CLAUDE_ATLAS_VAULT
	mount   *registry.Mount // the project's mount of target, when target is a knowledge base
}

// open resolves the target (explicit, env, nearest identity file, then discovery) and
// the session's project (the same resolution with no explicit vault), then the mount.
func (s *Server) open(explicit string) (*session, error)

// writable says whether a page-writing kind may run: in a project, always; in a
// knowledge base, only through a project session whose mount is effectively write.
// It returns the refusal to show the model.
func (sess *session) writable(kind txn.Kind) error
```

Rules:
- `plan`: `ingest` and `save` on a knowledge base need `sess.project != nil`, `sess.mount != nil`, and `sess.mount.Effective == write`; the refusals: no project → "knowledge enters through a project: <kb> is a knowledge base; run this in a project that mounts it (mounted by: a, b)" (names from `MountedBy`, or "nothing mounts it yet"); a project without a mount → "<project> does not mount <kb>; run `claude-atlas mount <project> <kb>`"; a read mount → "<project> mounts <kb> read-only". `repair`, `fold`, `markdown`, `canvas`, `base`, `stub` run in a knowledge base session or through a write mount; through a read mount they are refused too (a read mount writes nothing).
- `capture`: on a project, as today. On a knowledge base: `sess.project` must exist and the mount must be write; then `capture.CaptureFrom(target, projectVault, paths, ledger.Via{ID, Name}, now)` where `projectVault` is `vault.Open(sess.project.Path)`. The description says the paths are the project's inbox files.
- `stub`: `StubArgs.Titles[i].Target string` (a mount name; empty means the project's own wiki). Titles with a target are grouped per target and go through `txn.StubInto(project, kb, titles, defaultType, via, now)`; the mount must be write; the result lists each stub with its vault path. With no titles, `stub` stubs into the project only.
- A `mounts` tool (read-only): for a project, each mount with `id`, `name`, `path` (the knowledge base's `wiki/`), `link` (`kb/<name>`), `access` (requested), `effective`, `scope`, `pages` (from the knowledge base's lint summary, or omitted when the knowledge base cannot be read), and `error`; for a knowledge base it errors "a knowledge base has no mounts; it is mounted by: …".
- `status`: a project's `mounts` (the same shape without `pages`); a knowledge base's `access` and `mounted_by` (name and effective access).
- `route` is Task 6; leave it.

- Produces in `txn`:

```go
// StubInto creates, in kb, seed pages for titles the project's wiki links to but nobody
// has written, so the links resolve through the project's mount of kb. via names the
// project in the operation's summary. One operation, in kb.
func StubInto(project, kb *vault.Vault, titles []StubTitle, defaultType string, via string, now time.Time) (StubResult, error)
```

`StubInto` runs lint on `project` for the wanted set (as `StubRequest` does), routes and writes in `kb` (`kb.RouteFor`, `vault.Skeleton`), and applies with summary `stub <titles> (via <project>)`. `StubTitle` gains `Target string` (`target,omitempty`, "a mount name; the stub lands in that knowledge base").

- [ ] **Step 1: Write the failing tests**

In `server_test.go`, add a fixture `mounted(t)` that builds a temp home (`home.Home{Root: …}` with a config whose `VaultsDir` holds a project `p` and a knowledge base `kb`), mounts `kb` on `p` with `vaults.Mount` (write), refreshes nothing, and returns `(h, cfg, p, kb)`. `connect` must pass `home.EnvHome → h.Root` through `Env` (see `TestReposToolAndStatusInARepository` for the pattern).

- `TestKnowledgeBaseThroughAProjectSession`: connect in `p`; `mounts` lists `kb` with `effective: write` and `path == kb.Wiki()`; `capture` with `vault: kb.Root` and `paths: ["paper.md"]` (put the file in `p`'s inbox first) captures into `kb` and its ledger record has `via.name == p.Name`; `plan` with `vault: kb.Root`, kind `save`, a concept page → succeeds; `apply` commits in `kb`; `status` with `vault: kb.Root` shows `mounted_by` naming `p` with `write`.
- `TestReadMountRefusesWrites`: `vaults.Grant(kb, p, "read")` after making `kb` guarded (`vaults.EditIdentity` with `Access: "guarded"`); `plan` save into `kb` → error containing "read-only"; `capture` into `kb` → the same; `stub` with a target → the same.
- `TestKnowledgeBaseSessionRefusesIngestAndNamesMounts`: connect in `kb`; `plan` save → error containing "mounted by: p"; `plan` repair → ok; `mounts` → error "mounted by".
- `TestStubIntoAMount`: a page in `p` links `[[Backprop]]`; `stub` with `titles: [{title: "Backprop", target: "kb"}]` → the result path is under `kb`'s wiki; `p`'s lint no longer lists `Backprop` as wanted (this half depends on Task 7; assert only the stub's path here and mark the lint assertion for Task 7).

In `stub_test.go`: `TestStubIntoCreatesInTheKnowledgeBase`: a project page links `[[Vanishing Gradient]]`; `StubInto(p, kb, nil-titles-means-all? no: titles []StubTitle{{Title: "Vanishing Gradient"}}, "", "cs566", now)` creates `wiki/concepts/Vanishing Gradient.md` in `kb` with `status: seed`, commits in `kb` with subject `stub: stub Vanishing Gradient (via cs566)`, and leaves `p` unchanged; a title nothing in `p` links → error.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/mcpserver/ ./internal/txn/ 2>&1 | head`
Expected: compile errors (`mounts` unknown tool, `StubInto` undefined) or tool-not-found errors.

- [ ] **Step 3: Write the code**

- `open`: resolve the target as `resolve` does; then the project: `vault.Resolve("", env, ProjectDir)` → if it opens and is a project, that is the session's project; else `discover.Vault` for a repository; else nil. Look the project up in a `registry.Scan(cfg)` (`ix.ByPath`) to get its resolved mounts; when `target` is a knowledge base, find the mount with `ID == target.Config.ID`. Cache the scan per call, not per server.
- `writable(kind)` per the rules above; call it in `plan` (replacing the phase 1 check), `capture`, and `stub`.
- `mounts` tool and the `status` fields; `MountInfo{ID, Name, Path, Link, Access, Effective, Scope, Pages *int, Error}`.
- `StubInto` in `stub.go`: share the candidate-building code with `StubRequest` (extract a helper that returns candidates and the report for a vault).
- Register `mounts` in `MCP()` and add it to `ToolNames`.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass; the phase 1 `TestKnowledgeBaseTools` expectations change from "through a project" to the new refusal text — update that assertion to the new message.

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver/server.go internal/mcpserver/server_test.go internal/txn/stub.go internal/txn/stub_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "mcpserver: a project session writes a mounted knowledge base with its effective access"
```

---

### Task 6: `route` answers across the project and its mounts

**Files:**
- Modify: `internal/vault/route.go`, `internal/vault/vault_test.go`
- Modify: `internal/mcpserver/server.go`, `internal/mcpserver/server_test.go`

**Interfaces:**
- Produces in `vault`:

```go
// Match is an existing page that a title names: by its file stem or by an alias in its
// frontmatter, compared without regard to case.
type Match struct {
	Path    string `json:"path"`
	ByAlias string `json:"by_alias,omitempty"`
}

// FindPage looks for a page named title under wiki/: the file stem first, then the
// aliases in every page's frontmatter. It returns nil when none matches.
func FindPage(root, title string) (*Match, error)
```

- Produces in `mcpserver`: `route`'s output becomes

```go
type RouteOut struct {
	vault.Route                    // where the page goes in the session's vault (the target), as today
	Vault   string   `json:"vault"` // the target's root
	Match   *vault.Match `json:"match,omitempty"` // an existing page in the target
	Mounts  []MountRoute `json:"mounts,omitempty"` // for a project: one per mount
}
type MountRoute struct {
	Name      string       `json:"name"`
	Vault     string       `json:"vault"`
	Effective string       `json:"effective"`
	Path      string       `json:"path,omitempty"`  // where a new page would go, for a writable mount and a type the knowledge base files
	Match     *vault.Match `json:"match,omitempty"` // an existing page with that title or alias
	Error     string       `json:"error,omitempty"`
}
```

Rules: in a project, `route` fills `Mounts` for every mount, resolved or not (an unresolved mount carries its error); a read-only mount reports a `Match` but no `Path`; a type the knowledge base does not file (question, session) reports neither. The response's `next` sentence: "A match anywhere means link to it instead of creating a page."

- [ ] **Step 1: Write the failing tests**

`vault_test.go` `TestFindPageByStemAndAlias`: a vault with `wiki/concepts/Backpropagation.md` whose frontmatter has `aliases: [backprop, "Back Propagation"]`; `FindPage(root, "backpropagation")` → path; `FindPage(root, "Back propagation")` → path with `ByAlias == "Back Propagation"`; `FindPage(root, "nope")` → nil.

`server_test.go` `TestRouteAcrossMounts` (with the `mounted` fixture from Task 5): the kb holds `Backpropagation.md`; `route` in `p` with `type: concept, title: "backprop"` (an alias) → `Mounts[0].Match.Path` set, `Mounts[0].Path` ends in `wiki/concepts/backprop.md`; with `title: "Fresh"` → no match anywhere and `Mounts[0].Path` set; with `type: question` → `Mounts[0].Path == ""` and no error; after `vaults.Grant(kb, p, "read")` on a guarded kb, `Mounts[0].Path == ""` and `Effective == "read"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vault/ -run TestFindPage; go test ./internal/mcpserver/ -run TestRouteAcrossMounts`
Expected: compile errors `undefined: FindPage`, `RouteOut`.

- [ ] **Step 3: Write the code**

`FindPage` walks `wiki/` (skipping dot-dirs), compares stems with `strings.EqualFold`, then reads each page's frontmatter (`vault.Frontmatter` + `vault.StringList(fields, "aliases")`) for an alias match. `route` in the server: after the target's own `RouteFor`, `FindPage(target.Root, title)`; for a project, iterate `sess.project.Mounts`, and for each resolved mount open the knowledge base (`vault.Open(filepath.Dir(m.Path))`), call `FindPage` and, when `m.Effective == write` and the type is in `RoutableTypes(Knowledge, kb.Config.Mode)`, `kb.RouteFor`.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/route.go internal/vault/vault_test.go internal/mcpserver/server.go internal/mcpserver/server_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "route: an existing page in the project or any mount is found by title or alias"
```

---

### Task 7: Lint sees through mounts

**Files:**
- Modify: `internal/lint/lint.go`, `internal/lint/lint_test.go`
- Modify: `internal/txn/stub_test.go` (the deferred assertion from Task 5)

**Interfaces:**
- Produces: `lint.Run` reads every symlink under `<root>/kb/` (`os.Readlink` + `filepath.EvalSymlinks`; a dangling one is skipped and reported in a new `Report.MountErrors []PathFinding` (`mount_errors`, counted in `IssuesFound`)) and walks the target directory (the knowledge base's `wiki/`) into targets with paths `kb/<name>/<rel>`. Those targets are parsed for headings, blocks, and aliases (so fragments and aliases resolve) but are not `pages`: they get no findings, are not counted in `pages_scanned`, and are not orphan or index candidates.
- Resolution: `resolver.resolve` tries the project's own targets (paths under `wiki/`) first; only when none matches does it consider `kb/` targets. A bare name matching in two mounts is ambiguous; in one mount, resolved.
- `duplicate_basenames` spans own pages and mount targets except the root pages (`index`, `log`, `hot`, `overview`) and the folder index pages (`vault.TasksIndex`, `vault.CanvasIndex`, and any `<folder>/<folder>.md`); a mount's root pages never count.
- `wantedTitle`/wanted pages: a link that resolves in a mount is resolved, so it is not wanted. The near index includes mount stems and aliases.
- `lint.Options.Mounts map[string]string` (name → real wiki path) lets a caller pass mounts explicitly (tests, and the server when the symlink is missing); when nil, `Run` reads the symlinks.

- [ ] **Step 1: Write the failing tests**

`lint_test.go` `TestLinksResolveThroughMounts`: fixture: a project with `wiki/index.md` linking `[[Own]]` and `[[Shared]]`, `wiki/concepts/Own.md`, `wiki/concepts/Shared.md`, `wiki/concepts/Notes.md` linking `[[Backpropagation]]`, `[[backprop]]` (alias), `[[Backpropogation]]` (typo), `[[Shared]]`, `[[Twice]]`, `[[Backpropagation#Causes]]`, `[[Nowhere]]`; a knowledge base `wiki/` (a separate temp dir) with `concepts/Backpropagation.md` (aliases `[backprop]`, a `## Causes` heading), `concepts/Shared.md`, `concepts/Twice.md`, `index.md`, `hot.md`; a second knowledge base with `concepts/Twice.md`; symlinks `kb/ai-ml` and `kb/other` in the project. Assert: `Backpropagation`, `backprop`, `#Causes` resolve (no dead link); `Backpropogation` is a dead link with suggestion `Backpropagation`; `Shared` resolves to the project's own page and `duplicate_basenames` lists `Shared` with both paths; `Twice` is ambiguous (two mounts); `Nowhere` is a wanted page; `pages_scanned` counts only the project's pages; no finding names a `kb/` path except the duplicate and the ambiguity; `index` and `hot` are not duplicates. Then run again with `Options{Mounts: map[string]string{"ai-ml": …}}` and no symlinks and expect the same. A dangling symlink `kb/gone` → `mount_errors` names it.

`stub_test.go`: extend `TestStubIntoCreatesInTheKnowledgeBase`: after `StubInto`, `lint.Run(project)` no longer lists the title as wanted (mount the kb on the project first with a symlink).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/lint/ -run TestLinksResolveThroughMounts`
Expected: FAIL: the mount links are dead or wanted.

- [ ] **Step 3: Write the code**

In `Run`: after `walk(root)`, collect mounts (from `opts.Mounts` or the symlinks), walk each target dir, and append `target{path: "kb/<name>/<rel>", page: parsePage(...) for .md}` with a `mount bool` on `target` (or a separate slice) so the page loops skip them. Give `resolver` two tiers: `own` and `mounts`; `resolve` runs the query list against `own` first and returns on a hit. In the duplicate loop, include mount targets and exclude the root and folder-index basenames. In `newNearIndex`, include mount targets. `wantedTitle` needs no change: a resolved link never reaches it. Add `MountErrors` to `Report`, `fillEmpty`, `sortFindings`, `CategoryCounts`, and the Markdown ("Mounts" section after "Kind").

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass; `TestNewVaultHasNoFindings` still clean (a fresh project has an empty `kb/`… it has no `kb/` folder at all).

- [ ] **Step 5: Commit**

```bash
git add internal/lint/lint.go internal/lint/lint_test.go internal/txn/stub_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "lint: a project's links resolve through its mounts; a name in the project and a mount is a duplicate"
```

---

### Task 8: `mount`, `unmount`, `grant`, `revoke`; `doctor` and `refresh` keep the symlinks

**Files:**
- Modify: `internal/cli/cli.go`, `internal/cli/cli_test.go`
- Modify: `internal/refresh/derive.go`, `internal/refresh/refresh_test.go`

**Interfaces:**
- Consumes: `vaults.Mount`, `Unmount`, `Grant`, `Revoke`, `EnsureMounts`, `registry.Scan`.
- Produces, commands:

| Command | Does |
|---|---|
| `mount PROJECT KB [--read] [--as NAME]` | `vaults.Mount`; prints `mounted KB as kb/NAME (effective ACCESS)`; refreshes |
| `unmount PROJECT KB\|NAME` | `vaults.Unmount`; prints `unmounted NAME`; refreshes |
| `grant KB PROJECT --write\|--read` | `vaults.Grant`; prints the grant; refreshes |
| `revoke KB PROJECT` | `vaults.Revoke`; refreshes |
| `show NAME` | a project's mounts now show `kb/<name>`, effective access, and the symlink state (`ok`, `missing`, `wrong target`); a knowledge base shows `mounted by` |
| `doctor` | for each project, `vaults.EnsureMounts` in dry-run form (report only): a missing or wrong symlink is a `Fail` step "run `claude-atlas refresh` to recreate it"; an unresolved mount stays a `Fail` as today |
| `refresh` | `refresh.Registry` now calls `vaults.EnsureMounts` for every readable project and reports `created`/`removed`/`missing` steps |

Every mutating command resolves `PROJECT` and `KB` through `e.entry` and refuses the wrong kind with the function's own error. The `usage` string gains the four commands.

- Produces in `refresh`: `Registry` takes an extra `ensure bool` (the CLI passes `true`; tests may pass `false`); when true it runs `EnsureMounts` on each project before deriving and records `State.Mounts []registry.MountState{Name, Link, Path, OK, Error}` — hmm, keep it simpler: `EnsureMounts` results are returned to the caller as `[]MountChange{Project, Created, Removed, Missing}` and `Signals` reports a mount whose symlink is missing or wrong ("mount X: symlink missing; run claude-atlas refresh") by checking `os.Readlink(e.KbDir(m.Name))` against `m.Path` at derive time.

- [ ] **Step 1: Write the failing tests**

`cli_test.go` `TestMountGrantAndRevokeCommands`: with `setup(t)` plus `new-knowledge ai-ml`; `mount welcome ai-ml` → exit 0, output contains `kb/ai-ml`, `os.Readlink(<welcome>/kb/ai-ml)` is the knowledge base's `wiki/`, `show welcome` lists the mount with `write`; `mount welcome ai-ml` again → exit 1 "already"; `edit ai-ml --access guarded` then `show welcome` shows `read`; `grant ai-ml welcome --write` → `show welcome` shows `write`; `revoke ai-ml welcome` → `read`; `grant ai-ml welcome --sometimes` → exit 2; `mount ai-ml welcome` → exit 1 (a knowledge base cannot mount); remove the symlink by hand, `doctor` exits 1 naming the mount, `refresh` recreates it (readlink ok) and prints `created`; `unmount welcome ai-ml` → symlink gone, `show welcome` lists no mount.

`refresh_test.go` `TestSignalsNameAMissingSymlink`: an entry whose mount has `Path` set but whose `KbDir` has no symlink → `Signals` includes "symlink missing".

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run TestMountGrantAndRevokeCommands`
Expected: FAIL: `mount` is an unknown command.

- [ ] **Step 3: Write the commands**

Follow `link`/`unlink`/`edit-repo` for the shape (flags, `e.entry`, the `refreshAll` at the end, the `console.Step` lines). In `derive.go`, check each resolved mount's symlink when deriving (`os.Readlink(e.KbDir(m.Name))` equals `m.Path`) and let `Signals` report it; in `Registry`, when `ensure` is true, call `vaults.EnsureMounts(e, ix)` for each readable project before `Derive` and return the changes; `cli.refresh` prints them. `doctor` compares the symlinks without creating them.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go internal/refresh/derive.go internal/refresh/refresh_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "cli: mount, unmount, grant, and revoke; refresh recreates a project's mount symlinks"
```

---

### Task 9: The session hook names the mounts

**Files:**
- Modify: `internal/hooks/hooks.go`, `internal/hooks/hooks_test.go`

**Interfaces:**
- Consumes: `registry.Scan`, `registry.Index.ByPath`, `Entry.Mounts` (with `Effective`, `Path`, `Error`), `Entry.MountedBy`, `Entry.Scope`, `lint.Run` (page count).
- Produces: in a project session, after the first line and before the skills line, one line per mount:

```text
Knowledge: ai-ml (write) · Machine learning: models, training, evaluation, deployment, agents · 61 pages · kb/ai-ml
```

(`(read)` for a read mount; an unresolved mount prints `Knowledge: gone (unresolved: no knowledge base with id …)`; a mount whose symlink is missing appends `· symlink missing; run claude-atlas refresh`), then the sentence "Search the project and its knowledge bases (the wiki-query skill) before answering from the code alone." replaces the current "Search the wiki…" clause in the repository sentence and is printed on its own line in every project session. In a knowledge base session, the first line becomes `claude-atlas knowledge base: ai-ml (generic mode, open) at PATH, mounted by cs566 (write), self-study (read).` (or `mounted by nothing yet`; `guarded` in place of `open` when guarded). The page count comes from `lint.Run(kb.Wiki()'s vault root)`'s `PagesScanned`; when the scan cannot read the knowledge base, omit the count.

The hook loads the config through `home.Resolve(env(home.EnvHome))`; with no atlas config (`home.ErrNoAtlas`) it prints no mount lines and no mounted-by clause.

- [ ] **Step 1: Write the failing tests**

`hooks_test.go` `TestSessionStartListsMountsAndMountedBy`: a temp home with a config, a project and a knowledge base (scope set through `vaults.EditIdentity`), mounted with `vaults.Mount`; `SessionStart` in the project prints `Knowledge: ai-ml (write) · <scope> · N pages · kb/ai-ml` and the search sentence; in the knowledge base, `mounted by V (write)`; after `vaults.EditIdentity(kb, Edit{Access: guarded})`, the project's line says `(read)` and the knowledge base's says `guarded`; with the symlink removed, the project's line ends `symlink missing; run claude-atlas refresh`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/hooks/ -run TestSessionStartListsMountsAndMountedBy`
Expected: FAIL: no `Knowledge:` line.

- [ ] **Step 3: Write the code**

In `SessionStart`, after resolving `v`: load the config and scan (skip on `ErrNoAtlas`); `entry := ix.ByPath(v.Root)`; for a project, print the mount lines from `entry.Mounts`; for a knowledge base, build the first line from `entry.Access` and `entry.MountedBy`. Keep the rest.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass; `TestSessionStart` and `TestSessionStartInAKnowledgeBase` (no atlas config) still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/hooks.go internal/hooks/hooks_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "hooks: a project session lists its mounts; a knowledge base session names who mounts it"
```

---

### Task 10: Docs, and the Obsidian check

**Files:**
- Modify: `CLAUDE.md`, `docs/usage.md`, `docs/v2-design.md`

- [ ] **Step 1: CLAUDE.md**

In "Constraints": the write-path bullet adds `vault.UpdateConfig` and "the symlinks under a project's `kb/`, which `mount` and `refresh` create; they are local state git ignores". The reserved-paths bullet adds `kb/` and `repos/`. A new bullet: "A project session may write a mounted knowledge base when its mount is effectively `write` (`registry.Effective` of the request and the grant); a knowledge base session runs maintenance kinds only; a source captured into a knowledge base records `via`, the project it came through." The lint bullet: "In a project, lint resolves links through the symlinks under `kb/`; findings are about the project's own pages." "Lint and refresh are read-only" becomes "Lint is read-only; refresh writes nothing a vault's git tracks (it recreates the ignored `kb/` symlinks)." The sources-of-truth v2 row: "(phases 1–3 built: kinds, the registry, mounts)".

- [ ] **Step 2: usage.md**

A "Mount a knowledge base" section after "Repositories": `mount`, `unmount`, `grant`, `revoke`, what the symlink is, what `refresh` and `doctor` do with it, how access works in three sentences, and that `[[links]]` from a project resolve into the mount in Obsidian and in lint. The in-vault "Work in a vault with Claude Code" section gains one paragraph: in a project, the tools see the mounts; ingest into a knowledge base runs in the project session (`capture` into it, then `plan`/`apply`), and a read mount refuses writes. The command table and the `view` key table note `m`/`u`/`g`/`G` as phase 4.

- [ ] **Step 3: v2-design.md**

Status line: "Phases 1–3 are built (kinds; the registry; mounts and access). Phases 4–7 are not." In "Mounts", replace the "Verify before phase 3 ships" paragraph with the result: the iCloud fact from this plan's "Symlink facts" section, and the Obsidian check as a step for the user with exact instructions: create a project and a knowledge base with `new-project`/`new-knowledge`, `mount` them, `open-vault` the project, write `[[<a knowledge base page>]]` in the project's `wiki/hot.md` in Obsidian, and confirm the link opens the page, the graph shows it, and a bare `[[index]]` opens the project's own index. Record the result in that paragraph when known; until then the paragraph says "not yet run". In "Tools", the `route` row says what Task 6 built (matches by title or alias across mounts). In "Lint", add `mount_errors`.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/usage.md docs/v2-design.md
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "docs: mounts, access, and the Obsidian check"
```

---

## Self-review

**Spec coverage (phase 3):** `mount`, `unmount`, `grant`, `revoke` (Tasks 2, 8); the symlink as local state recreated by `refresh` and reported by `doctor` (Tasks 2, 8); effective access in `plan`, `apply` (through `plan`'s gate; `apply` consumes a plan the gate admitted), and the guard (Tasks 3, 5); `route` across mounts (Task 6); lint across mounts, the near match, the wanted-page rule, the stub `target` (Tasks 5, 7); `via` in the ledger (Task 4); the session hook's mount lines (Task 9); the `mounts` tool (Task 5); `kb/` and `repos/` reserved (Task 3); the lock for `UpdateConfig` (Task 1, from phase 2's deferral). The stubs-status page's remaining items (the session-start counts line) stay with phase 6 alongside the skills. The Obsidian check is a manual step recorded in the spec (Task 10).

**Placeholders:** Tasks 5, 7, and 8 describe interfaces, rules, and tests rather than every line; their implementers run on the most capable model. Task 8's `refresh.Registry` change is stated once, as "return the changes and let `Signals` report a missing symlink" (the first draft sentence with `MountState` is superseded by the "keep it simpler" sentence that follows it).

**Type consistency:** `registry.Mount{ID, Name, Access, Effective, Path, Error}` (phase 2) is what Tasks 5, 6, 8, 9 read. `vaults.Mount(project, kb, access, name, now)` returns `vault.Mount`. `ledger.Via{ID, Name}` is used by `capture.CaptureFrom` and by the server. `txn.StubTitle.Target` and `txn.StubInto(project, kb, titles, defaultType, via, now)` are used by the `stub` tool. `lint.Options.Mounts map[string]string` is name → real path. `vault.KbDir`/`ReposDir` constants (Task 3) replace `registry`'s private ones and are what `txn` and the guard test.
