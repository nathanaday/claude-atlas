# v2 Phase 4: Mounts and Grants in `view`, `doctor` Checks Grants — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The `view` screen mounts, unmounts, grants, and revokes through one key, `m`; the registry, `show`, `doctor`, and the signals name a grant whose project the scan does not find; `revoke` can drop such a grant.

**Architecture:** One new TUI screen, `mountsScreen`, opens on `m` and is kind-aware: on a project it lists the mounts (`a` mount a knowledge base, `u` unmount); on a knowledge base it lists the projects that mount it and the grants (`w` and `r` grant write or read, `x` revoke, `a` grant to a project by name). It reaches `vaults.Mount`, `Unmount`, `Grant`, and `Revoke` through four new `tui.Hooks` fields that `cli.hooks` builds, the way the repositories screen reaches the repository functions. `registry.Grant` replaces `vault.Grant` in `registry.Entry.Grants` and carries an `Error` when the scan holds no project with the grant's id; `Signals`, `show`, the detail view, and `doctor` read it.

**Tech Stack:** Go 1.24, Bubble Tea, Bubbles `textinput`, the packages `registry`, `refresh`, `vaults`, `cli`, `tui`.

**Spec:** `docs/v2-design.md`, sections "The atlas" (the `view` key table), "Access", "Phases" (item 4).

## Global Constraints

- The TUI is a subset of the CLI: every screen action has a command. This phase's commands exist (`mount`, `unmount`, `grant`, `revoke`); Task 1 extends `revoke` so a stale grant can be dropped from the CLI too.
- An action lives once, in `internal/vaults`; the CLI calls it; the TUI reaches it through `tui.Hooks`, built in `cli.hooks`.
- TUI models keep all logic in `Update`; tests drive them with `tea.KeyMsg` (`keyV`, `pressV`, `typeV` in `internal/tui/view_test.go`).
- Every identity-file change goes through `vault.UpdateConfig`. Doctor is read-only.
- Every command that acts on a vault scans afresh (`registry.Scan`); `registry.json` is for display.
- Dependencies: `gopkg.in/yaml.v3`, the MCP `go-sdk` v1.4.0, Bubble Tea, Bubbles, Lip Gloss. Nothing else.
- Tests never touch a real `~/.claude-atlas`; they skip when git is missing (`gitx.Available`).
- Commits: `git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit`; no `Co-Authored-By`; subject `area: what changed`; stage by name; never stage `.superpowers/`.
- Prose (comments, messages, docs): short plain sentences, active voice, no metaphors; never "flag" (except a CLI flag), "genuine", "honest", "shape", "load bearing", "judgement call", "earned its keep", "worth flagging". Comments are sparse.
- Key names: `m` opens the mounts screen on either kind. Inside it: `a`, `u` (project); `w`, `r`, `x`, `a` (knowledge base); `Esc` closes. The old table rows naming `M`, `g`, `G` go.

---

### Task 1: A grant whose project is gone is a finding; `revoke` drops it by id

**Files:**
- Modify: `internal/registry/registry.go`, `internal/registry/registry_test.go`
- Modify: `internal/refresh/derive.go`, `internal/refresh/refresh_test.go`
- Modify: `internal/vaults/mounts.go`, `internal/vaults/mounts_test.go`
- Modify: `internal/cli/cli.go`, `internal/cli/cli_test.go`
- Modify: `internal/tui/view.go` (only the type of `e.Grants` in the detail loop, so it compiles)

**Interfaces:**
- Produces in `registry`:

```go
// Grant is what a knowledge base grants one project, resolved against the scan: Name is
// the project's current name when the scan holds it, and Error says when it does not.
type Grant struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Access string `json:"access"`
	Error  string `json:"error,omitempty"`
}
```

`Entry.Grants` becomes `[]Grant`. `buildEntry` copies each `vault.Grant`; `resolve` sets `Name` from `ix.ByID(g.ID)` when found, else `Error = "no project with id " + g.ID`. `GrantedAccess` keeps reading `g.ID` and `g.Access`.

- Produces in `vaults`:

```go
// RevokeID removes kb's grant for projectID, whether or not the scan still holds that
// project. It refuses a project entry as kb and an id kb grants nothing to.
func RevokeID(kb registry.Entry, projectID string, now time.Time) error
```

`Revoke(kb, project, now)` becomes `RevokeID(kb, project.ID, now)` after its own kind checks (add one: `project.Kind != vault.Project` → "<name> is not a project").

- Produces in `refresh.Signals`: for a knowledge base, one note per grant with `Error`: `grant <name or id>: no project with id <id>; run \`claude-atlas revoke <kb> <id>\``.
- Produces in `cli`: `revoke KB PROJECT|ID` — when `e.entry(cfg, args[1])` fails and `args[1]` equals the `ID` of one of `kb.Grants`, call `vaults.RevokeID`; print `revoked <id> on <kb>`. `doctor`: for each knowledge base, a grant with `Error` is `console.Fail` (`<kb> · grant <name or id>`, the error plus "; run `claude-atlas revoke <kb> <id>`"); a grant on an open knowledge base (`en.Access == vault.AccessOpen`) is `console.Skip` (`<kb> · grant <name>`, "<kb> is open; the grant applies when it is guarded"). `show` prints a grant row's `Error` after the access when set.

- [ ] **Step 1: Write the failing tests**

`registry_test.go` `TestAGrantForAnUnknownProjectCarriesAnError`: a knowledge base whose identity file has two grants (through `vault.UpdateConfig` on its root: one for a scanned project's id, one for `"gone-0000"`); after `Scan`, the first grant's `Name` equals the project's name from the scan (set the identity's grant name to "old name" to prove it was refreshed) and `Error == ""`; the second has `Error == "no project with id gone-0000"`.

`refresh_test.go` `TestSignalsNameAStaleGrant`: an entry with `Kind: vault.Knowledge`, `Grants: []registry.Grant{{ID: "gone-0000", Name: "x", Access: "write", Error: "no project with id gone-0000"}}`, a non-nil `State{VaultOK: true}` → `Signals` contains "no project with id gone-0000" and "revoke".

`mounts_test.go` `TestRevokeIDDropsAGrantWhoseProjectIsGone`: a guarded knowledge base with a grant for `"gone-0000"` written through `vault.UpdateConfig`; `RevokeID(kbEntry, "gone-0000", now)` → the identity file has no grants; `RevokeID(kbEntry, "gone-0000", now)` again → error containing "no grant"; `RevokeID(projectEntry, …)` → error containing "not a knowledge base".

`cli_test.go` `TestDoctorAndRevokeSeeAStaleGrant`: `setup(t)`; `new-knowledge ai-ml`; `edit ai-ml --access guarded`; write a grant `{ID: "gone-0000", Name: "gone", Access: "write"}` through `vault.UpdateConfig`; `doctor` → output contains "no project with id gone-0000" and "revoke ai-ml gone-0000"; `show ai-ml` → output contains "gone-0000" and "no project"; `revoke ai-ml gone-0000` → exit 0, output contains "revoked"; `doctor` → output no longer contains "gone-0000". Then `edit ai-ml --access open`; `grant ai-ml welcome --write` → exit 0; `doctor` → output contains "is open; the grant applies when it is guarded".

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/registry/ ./internal/refresh/ ./internal/vaults/ ./internal/cli/ 2>&1 | head -20`
Expected: compile errors (`registry.Grant`, `RevokeID` undefined) or assertion failures.

- [ ] **Step 3: Write the code**

`registry.go`: the `Grant` type; `buildEntry` converts; in `resolve`, after mounts, loop each knowledge base's grants and set `Name`/`Error`. `derive.go`: the signal. `mounts.go`: `RevokeID`, and `Revoke` over it. `cli.go`: `revoke` (usage `claude-atlas revoke KB PROJECT|ID`), `doctor`, `show`. `view.go`: the detail loop over `e.Grants` reads `g.Name`, `g.Access` as before (the type changes; the fields keep their names).

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/registry/registry.go internal/registry/registry_test.go internal/refresh/derive.go internal/refresh/refresh_test.go internal/vaults/mounts.go internal/vaults/mounts_test.go internal/cli/cli.go internal/cli/cli_test.go internal/tui/view.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "registry, cli: a grant for a project the scan does not find is a finding; revoke drops it by id"
```

---

### Task 2: The mounts screen for a project; the hooks; `m` in the view

**Files:**
- Create: `internal/tui/mountscreen.go`, `internal/tui/mountscreen_test.go`
- Modify: `internal/tui/editor.go` (the `Hooks` fields), `internal/tui/editor_test.go` (the fixture's hooks)
- Modify: `internal/tui/view.go` (`m` key, `openMounts`, `updateMounts`, the hint line), `internal/tui/view_test.go` (the hint assertion)
- Modify: `internal/cli/cli.go` (`hooks`)

**Interfaces:**
- Consumes: `vaults.Mount(project, kb registry.Entry, access, name string, now) (vault.Mount, error)`, `vaults.Unmount(project registry.Entry, target string, now) error`, `vaults.MountState(project registry.Entry, m registry.Mount) string` with `vaults.MountOK`, `MountMissing`, `MountWrong`; `vaults.Grant(kb, project registry.Entry, access string, now) error`; `vaults.RevokeID(kb registry.Entry, projectID string, now) error` (Task 1).
- Produces in `tui.Hooks`:

```go
	// The mount calls: mount a knowledge base on a project with an access and a mount name
	// (empty means write and the knowledge base's name); unmount by knowledge base id, name,
	// or mount name; grant a project write or read on a knowledge base; revoke by project id.
	Mount   func(project, kb registry.Entry, access, name string) (vault.Mount, error)
	Unmount func(project registry.Entry, target string) error
	Grant   func(kb, project registry.Entry, access string) error
	Revoke  func(kb registry.Entry, projectID string) error
```

`cli.hooks` fills them with `vaults.Mount(project, kb, access, name, time.Now())`, `vaults.Unmount(...)`, `vaults.Grant(...)`, `vaults.RevokeID(...)`. The test fixture `atlasFixture` fills them the same way with `testNow`.

- Produces in `tui`:

```go
type mountsMode int

const (
	mountsList mountsMode = iota
	mountsPickKB     // typing a knowledge base's name
	mountsPickAccess // w or r for the new mount (project) or the new grant (knowledge base)
	mountsConfirm    // y or n before an unmount or a revoke
)

// mountRow is one line of the screen: on a project, a mount; on a knowledge base, a
// project that mounts it or holds a grant (or both).
type mountRow struct {
	mount   *registry.Mount // project screen
	ref     *registry.Ref   // knowledge base screen: a project that mounts it
	grant   *registry.Grant // knowledge base screen: its grant, when it has one
	name    string          // what the row is called: the mount name, or the project's name or id
	state   string          // project screen: the symlink state or the mount's error
}

type mountsScreen struct {
	hooks   Hooks
	entry   registry.Entry
	items   []Item
	rows    []mountRow
	cursor  int
	mode    mountsMode
	name    textinput.Model // the knowledge base (project screen) or project (knowledge base screen) to add
	picked  *registry.Entry // the vault the typed name resolved to
	status  string
	err     string
	changed bool
	closed  bool
	width   int
}

func newMounts(hooks Hooks, e registry.Entry, items []Item, width int) mountsScreen
func (s *mountsScreen) reload(items []Item)   // finds the entry again by path and rebuilds the rows
func (s mountsScreen) update(msg tea.Msg) (mountsScreen, tea.Cmd)
func (s mountsScreen) view() string
```

This task builds the project half; Task 3 adds the knowledge base half in the same file. In this task, `openMounts` on a knowledge base sets `v.errMsg = "a knowledge base is mounted by projects; press m on a project"` — Task 3 replaces that line.

Rules for the project screen:
- Rows come from `entry.Mounts` in order. `name` is `m.Name`; the line shows the mount name, the knowledge base's name from `items` (by `m.ID`; the mount name when not found), `requested → effective` (`m.Access` and `m.Effective`), and `state`: `m.Error` when set, else `vaults.MountState(entry, m)` shown as `ok`, `symlink missing`, or `symlink points elsewhere`.
- `a`: `mountsPickKB`, the input empty and focused, prompt "Knowledge base name". Enter resolves the typed name against `items` (`Kind == vault.Knowledge`, `strings.EqualFold` on `Name`); unknown → `err = "no knowledge base named X"` and stay; found → `picked`, `mountsPickAccess`, prompt "Access: w write (default), r read". In `mountsPickAccess`, `w` or Enter → write, `r` → read, Esc → back to the list; then `hooks.Mount(entry, *picked, access, "")`; on error `err` and back to the list; on success `changed = true`, `status = "mounted X as kb/NAME"`.
- `u`: with a row under the cursor, `mountsConfirm` with the question "Unmount NAME? y/n"; `y` → `hooks.Unmount(entry, row.mount.ID)`; `n`/Esc → list.
- Esc in the list closes (`closed = true`); `q` is not handled here (the view quits on `q` only outside screens, as for the repositories screen; check `updateLinks` for the convention and follow it).
- With no mounts, the list says: "no mounts yet: a mount is a knowledge base this project reads or writes through kb/. Press a to mount one."
- The header line: `<name>   mounts   <path>` like the repositories screen's `<name>   repositories   <path>`.
- The hint line under the list: `a mount · u unmount · esc back`.
- The view: `m` joins the key group `o c e i l t`; `openMounts(item)` needs `v.hooks.Load` and `v.hooks.Mount`; `updateMounts` mirrors `updateLinks` (close → reload keeping the path; changed → reload, `s.reload(v.items)`, refresh in the background). The hint line at `view.go:1087` gains ` · m mounts` for both kinds.

- [ ] **Step 1: Write the failing tests**

`mountscreen_test.go` `TestMountsScreenMountsAndUnmounts`: `cfg, _, v := atlasView(t)` (cursor on the project "reading"; the fixture holds the knowledge base "ai-ml"); `v = keyV(v, "m")` → `v.mounts != nil`, the view contains "no mounts yet" and "reading   mounts"; `keyV(v, "a")` → `mode == mountsPickKB`; `v.mounts.name.SetValue("nope")`, Enter → `err` contains "no knowledge base named nope", mode unchanged; `SetValue("AI-ML")`, Enter → `mode == mountsPickAccess`; `keyV(v, "r")` → `mode == mountsList`, one row, `v.changed`, status contains "mounted ai-ml as kb/ai-ml", the view contains "read" and "ok"; `entryNamed(t, cfg, "reading").Mounts[0].Access == "read"` and `os.Readlink(<reading>/kb/ai-ml)` is the knowledge base's `wiki/`; `keyV(v, "u")` → `mode == mountsConfirm`, the view contains "Unmount ai-ml?"; `keyV(v, "n")` → list, still one row; `keyV(v, "u")`, `keyV(v, "y")` → no rows, the symlink is gone, `entryNamed(...).Mounts` empty; Esc → `v.mounts == nil`.

`TestMountsScreenNeedsHooksAndAProject`: a view with `Hooks{}` → `m` sets `errMsg` containing "not available"; with hooks but the cursor on the knowledge base "ai-ml" → `errMsg` contains "press m on a project" (this assertion changes in Task 3).

`view_test.go` `TestHintsFollowTheCursor`: the project hint contains "m mounts".

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestMountsScreen|TestHintsFollowTheCursor'`
Expected: compile errors (`v.mounts`, `mountsPickKB` undefined).

- [ ] **Step 3: Write the code**

Follow `linkscreen.go` for the list, the box rendering (`boxSt`, `boxSelSt`, `selSt`, `dim`, `catSt`, `title`), the `textinput` use, and the confirm mode; follow `openLinks`/`updateLinks` in `view.go` for the wiring.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/mountscreen.go internal/tui/mountscreen_test.go internal/tui/editor.go internal/tui/editor_test.go internal/tui/view.go internal/tui/view_test.go internal/cli/cli.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "tui: m opens a project's mounts; a mounts and u unmounts"
```

---

### Task 3: The mounts screen for a knowledge base: who mounts it, grants, revokes

**Files:**
- Modify: `internal/tui/mountscreen.go`, `internal/tui/mountscreen_test.go`
- Modify: `internal/tui/view.go` (`openMounts` opens the screen for a knowledge base too)

**Interfaces:**
- Consumes: `Hooks.Grant`, `Hooks.Revoke` (Task 2), `registry.Entry.MountedBy []registry.Ref{ID, Name, Access}` (`Access` is the effective access), `registry.Entry.Grants []registry.Grant` (Task 1), `registry.Entry.Access` (`open` or `guarded`).

Rules for the knowledge base screen:
- Rows: one per project id in the union of `entry.MountedBy` and `entry.Grants`, in this order: mounters in `MountedBy` order, then grants for projects that do not mount. `name` is the project's name (from the ref or the grant; the id when the grant has an `Error`). The line shows: the project's name; `mounts it (effective X)` or `does not mount it`; `grant: write|read` or `no grant`; and the grant's `Error` when set.
- The header shows the access after the name: `<name>   mounts   open|guarded   <path>`.
- `w` / `r`: with a row whose project the scan holds (a ref, or a grant without `Error`), `hooks.Grant(entry, projectEntry, "write"|"read")` where `projectEntry` comes from `items` by id; on an open knowledge base, do it and set `status` to "granted X write; <kb> is open, so the grant applies when it is guarded"; on success `changed = true`.
- `x`: `mountsConfirm`, "Revoke NAME's grant? y/n"; `y` → `hooks.Revoke(entry, id)`; a row with no grant → `err = "NAME has no grant"`.
- `a`: `mountsPickKB` reused with the prompt "Project name"; Enter resolves against `items` with `Kind == vault.Project`; unknown → `err = "no project named X"`; found → `mountsPickAccess`, then `hooks.Grant`.
- With no rows: "nothing mounts this knowledge base yet, and it grants nothing. Press a to grant a project access."
- The hint line: `w grant write · r grant read · x revoke · a grant by name · esc back`.
- `openMounts` on a knowledge base opens this screen (needs `v.hooks.Grant`).

- [ ] **Step 1: Write the failing tests**

`mountscreen_test.go` `TestMountsScreenOnAKnowledgeBaseGrantsAndRevokes`: `cfg, _, hooks := atlasFixture(t)`; mount `ai-ml` on `reading` through `vaults.Mount` (entries from `registry.Scan`); make `ai-ml` guarded through `vaults.EditIdentity`; `v := openView(t, hooks)`; move the cursor to `ai-ml` (`pressV` with `tea.KeyDown` until `v.current().item.Entry.Name == "ai-ml"`); `keyV(v, "m")` → `v.mounts != nil`, the view contains "ai-ml   mounts   guarded", "reading", "mounts it (effective read)", "no grant"; `keyV(v, "w")` → `v.changed`, the view contains "grant: write" and "effective write", `entryNamed(t, cfg, "ai-ml").Grants[0].Access == "write"`; `keyV(v, "x")` → `mode == mountsConfirm`, the view contains "Revoke reading's grant?"; `keyV(v, "y")` → the view contains "no grant" and "effective read", `Grants` empty; `keyV(v, "a")`, `SetValue("welcome")`, Enter, `keyV(v, "r")` → a second row "welcome" with "does not mount it" and "grant: read"; `entryNamed(t, cfg, "ai-ml").Grants[0].Name == "welcome"`.

`TestMountsScreenNamesAStaleGrant`: as above, but write a grant for `"gone-0000"` through `vault.UpdateConfig` before opening; the row shows "gone-0000" and "no project with id"; `w` on it → `err` contains "no project"; `x`, `y` → the row is gone.

Update `TestMountsScreenNeedsHooksAndAProject` from Task 2: `m` on the knowledge base now opens the screen; rename the test `TestMountsScreenNeedsHooks`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run TestMountsScreen`
Expected: FAIL: the knowledge base case sets `errMsg`.

- [ ] **Step 3: Write the code**

In `mountscreen.go`, branch on `s.entry.Kind` in `build`, `update`'s key switch, `view`, and the hint. Keep one type.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/mountscreen.go internal/tui/mountscreen_test.go internal/tui/view.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "tui: m on a knowledge base shows who mounts it; w, r, and x grant and revoke"
```

---

### Task 4: The detail view shows the symlink state and stale grants; the docs

**Files:**
- Modify: `internal/tui/view.go` (the detail renderer near line 1152), `internal/tui/view_test.go` (`TestDetailShowsEverything`)
- Modify: `docs/usage.md`, `docs/v2-design.md`, `CLAUDE.md`

**Interfaces:** consumes `vaults.MountState`, `registry.Grant.Error`.

- [ ] **Step 1: Write the failing test**

`view_test.go` `TestDetailShowsEverything`: the sample's `p3` mounts `ai-ml` with `Path: "/v/ai-ml/wiki"` and no symlink on disk, so the detail's mount row contains "symlink missing"; add to the sample a knowledge base grant with `Error: "no project with id gone-0000"` on `papers` and assert the detail on `papers` contains "gone-0000" and "no project with id".

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestDetailShowsEverything`
Expected: FAIL.

- [ ] **Step 3: Write the code and the docs**

`view.go`: the mount row appends the state from `vaults.MountState` (`ok` is not shown; `symlink missing` and `symlink points elsewhere` are), or the mount's error; the grant row appends the grant's `Error` when set.

`docs/usage.md`: in the top key table, replace the two phase 4 rows with one: `| \`m\` mounts: on a project \`a\` mount, \`u\` unmount; on a knowledge base \`w\` \`r\` grant, \`x\` revoke, \`a\` grant by name | \`mount PROJECT KB [--read] [--as NAME]\`, \`unmount PROJECT KB\|NAME\`, \`grant KB PROJECT --write\|--read\`, \`revoke KB PROJECT\|ID\` |`; in the `view` key table, replace the phase 4 row with `| \`m\` | mounts: mount and unmount on a project; grants on a knowledge base |`; in "Mount a knowledge base", one sentence on the screen and one on `revoke KB ID` for a grant whose project is gone; `doctor`'s paragraph adds the two grant checks.

`docs/v2-design.md`: the status line "Phases 1–4 are built (kinds; the registry; mounts and access; the mount and grant keys in `view`). Phases 5–7 are not."; the key table's two phase 4 rows become the one row above (no "(phase 4)"); the "Phases" list item 4 stays as history.

`CLAUDE.md`: the sources-of-truth v2 row says "(phases 1–4 built: kinds, the registry, mounts, the view's mount keys)".

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/view.go internal/tui/view_test.go docs/usage.md docs/v2-design.md CLAUDE.md
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "tui, docs: the detail names a missing symlink and a stale grant; the m key in the tables"
```

---

## Self-review

**Spec coverage (phase 4):** "The mount and grant keys in `view`" — Tasks 2 and 3 (one key, `m`, with the actions inside the screen, the way `l` works for repositories; the spec's table rows for `m`/`u`/`g`/`G` become one row in Task 4). "CLI parity" — every screen action maps to `mount`, `unmount`, `grant`, `revoke`; the one action the CLI lacked (revoking a grant whose project is gone) lands in Task 1 as `revoke KB ID`. "`doctor` checks mounts and grants" — mounts landed in phase 3; grants in Task 1.

**Placeholders:** Tasks 2 and 3 describe the screen by rules and tests rather than every line; the implementers follow `linkscreen.go`.

**Type consistency:** `registry.Grant{ID, Name, Access, Error}` (Task 1) is what Tasks 3 and 4 read; `Hooks.Mount/Unmount/Grant/Revoke` (Task 2) are what Task 3 calls; `mountsMode` constants and `mountRow` fields (Task 2) are shared with Task 3; `vaults.RevokeID` (Task 1) is what `Hooks.Revoke` wraps (Task 2).
