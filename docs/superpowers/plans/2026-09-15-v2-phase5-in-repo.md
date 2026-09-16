# v2 Phase 5: A Project Inside a Repository — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `new-project NAME --in REPO` creates a project at `REPO/atlas/` that the repository's own git tracks; every git command the engine runs there takes the pathspec `atlas/`, so an operation never commits code outside the vault; the repository is the project's repository without an entry in the identity file.

**Architecture:** `gitx.Repo` gains a `Prefix`: the vault's path inside the working tree (`""` for a vault that is its own repository, `"atlas/"` for one inside a repository). Every method scopes its command to the prefix and translates paths in both directions, so `txn`, `vault`, and the rest keep speaking vault-relative paths. A vault learns its layout from the filesystem when it is opened (`vault.HostRepo`): the root is named `atlas`, has no `.git` of its own, and its parent has one. Nothing about the layout is stored; a clone on another machine opens the same way. The registry uses the same rule to list the host repository as the project's first repository, so discovery, the hooks, the `repos` tool, and `show` see it without new code.

**Tech Stack:** Go 1.24, git, the packages `gitx`, `vault`, `txn`, `registry`, `vaults`, `cli`, `hooks`, `lint`.

**Spec:** `docs/v2-design.md`, section "A project inside a repository", and "Phases" item 5.

## Global Constraints

- Every write path goes through `txn.Prepare` and `txn.Apply`. `vault.Init`, `vault.InitIn` (new), `vault.Adopt`, `vault.Upgrade`, and `vault.UpdateConfig` are the only code that writes vault files directly, and only before or outside an operation.
- Ids travel; paths stay. The identity file never holds a path or a layout. `~/.claude-atlas/config.json` holds the paths the atlas cannot compute: an in-repository project sits outside the vaults directory, so `new-project --in` registers its path there.
- Every git command the engine runs inside a repository takes the pathspec `atlas/`. The manual-edits commit and the operation commit touch only paths under `atlas/`. An operation never commits code outside `atlas/`, staged or not.
- The scan is the truth; every command that acts on a vault scans afresh.
- Dependencies: `gopkg.in/yaml.v3`, the MCP `go-sdk` v1.4.0, Bubble Tea, Bubbles, Lip Gloss. Nothing else.
- Tests never touch a real `~/.claude-atlas`; they skip when git is missing (`gitx.Available`); MCP tools are tested in-process.
- Commits: `git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit`; no `Co-Authored-By`; subject `area: what changed`; stage by name; never stage `.superpowers/`.
- Prose (comments, messages, docs): short plain sentences, active voice, no metaphors; never "flag" (except a CLI flag), "genuine", "honest", "shape", "load bearing", "judgement call", "earned its keep", "worth flagging". Comments are sparse.
- The folder name is fixed: `atlas`. `vault.InRepoDir = "atlas"`.

---

### Task 1: `gitx.Repo` scopes every command to a prefix

**Files:**
- Modify: `internal/gitx/gitx.go`, `internal/gitx/gitx_test.go`

**Interfaces:**
- Produces:

```go
// Repo is a git working tree rooted at Dir. Prefix is the path inside it that the caller
// owns, with a trailing slash ("atlas/"), or "" for the whole tree: every command is
// scoped to it, and every path a method takes or returns is relative to it.
type Repo struct {
	Dir    string
	Prefix string
}
```

Rules, method by method:
- `Status()`: `git status --porcelain=v1 -z -uall -- <Prefix>` when `Prefix != ""` (no pathspec otherwise); returned paths have the prefix removed; a path outside the prefix (git does not return one with the pathspec, but a rename's source may lie outside) is dropped.
- `Dirty()`: over `Status`, so scoped.
- `AddAll()`: `git add -A -- <Prefix or .>`.
- `Add(paths...)`: each path prefixed.
- `Commit(message)`: with a prefix, `git commit -q --no-verify -m <message> -- <Prefix>` (git's pathspec commit records the working tree under the prefix, and leaves the index outside it alone); without one, as today.
- `RestoreFromHead(paths...)`, `Tracked(path)`, `ShowFile(rev, path)`, `LogFollow(path)`: the path prefixed.
- `Log(n)`: `-- <Prefix>` appended when `Prefix != ""`, so only commits that touched the vault are listed.
- `ChangedPaths(sha)`: paths outside the prefix dropped, the rest stripped.
- `RevertNoCommit(sha)`: unchanged; the commits it reverts touched only the prefix.
- `Init`, `Clone`, `RemoteURL`, `IsRepo`, `InsideOtherRepo`, `HasHead`, `Head`: unchanged.
- Two private helpers: `func (r Repo) in(p string) string` (prefix a path) and `func (r Repo) out(p string) (string, bool)` (strip it; false when outside).

- [ ] **Step 1: Write the failing test**

Add to `gitx_test.go` (follow the file's existing fixture that creates a temp repository and commits; read it first):

```go
func TestPrefixScopesEveryCommand(t *testing.T) {
	if !Available() {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	whole := Repo{Dir: dir}
	if err := whole.Init(); err != nil {
		t.Fatal(err)
	}
	write := func(rel, text string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(text), 0o644)
	}
	write("src/main.go", "package main\n")
	write("atlas/wiki/index.md", "# index\n")
	if err := whole.AddAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := whole.Commit("code and vault"); err != nil {
		t.Fatal(err)
	}
	r := Repo{Dir: dir, Prefix: "atlas/"}
	write("src/main.go", "package main // changed\n")
	write("src/new.go", "package main\n")
	write("atlas/wiki/index.md", "# index\n\nchanged\n")
	write("atlas/wiki/concepts/A.md", "# A\n")
	// Status sees only the vault, with vault-relative paths.
	entries, err := r.Status()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	sort.Strings(paths)
	if strings.Join(paths, ",") != "wiki/concepts/A.md,wiki/index.md" {
		t.Fatalf("status %v", paths)
	}
	// A change outside the vault that the user staged stays staged and uncommitted.
	if _, err := whole.run("add", "src/main.go"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	sha, err := r.Commit("vault only")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := r.ChangedPaths(sha)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(changed)
	if strings.Join(changed, ",") != "wiki/concepts/A.md,wiki/index.md" {
		t.Fatalf("changed %v", changed)
	}
	outside, _ := whole.Status()
	var codes []string
	for _, e := range outside {
		codes = append(codes, e.Code+" "+e.Path)
	}
	sort.Strings(codes)
	if strings.Join(codes, ",") != "?? src/new.go,M  src/main.go" {
		t.Fatalf("outside the vault after the commit: %v", codes)
	}
	// Log lists only commits that touched the vault.
	if _, err := whole.run("commit", "-q", "-m", "code only"); err != nil {
		t.Fatal(err)
	}
	log, err := r.Log(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].Subject != "vault only" || log[1].Subject != "code and vault" {
		t.Fatalf("log %+v", log)
	}
	// Paths a method takes are vault-relative.
	if !r.Tracked("wiki/index.md") || r.Tracked("src/main.go") {
		t.Fatal("Tracked must prefix its path")
	}
	if data, err := r.ShowFile("HEAD", "wiki/concepts/A.md"); err != nil || string(data) != "# A\n" {
		t.Fatalf("ShowFile %q %v", data, err)
	}
	if follow, err := r.LogFollow("wiki/index.md"); err != nil || len(follow) != 2 {
		t.Fatalf("LogFollow %+v %v", follow, err)
	}
	write("atlas/wiki/index.md", "scribble\n")
	if err := r.RestoreFromHead("wiki/index.md"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "atlas/wiki/index.md")); string(data) != "# index\n\nchanged\n" {
		t.Fatalf("restore: %q", data)
	}
	if err := r.Add("wiki/concepts/A.md"); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := r.Dirty(); dirty {
		t.Fatal("the vault is clean")
	}
}
```

(`run` is the package's private helper; the test lives in package `gitx`, so it may call it. Add `sort` to the imports if missing.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/gitx/ -run TestPrefixScopesEveryCommand`
Expected: compile error `unknown field Prefix`.

- [ ] **Step 3: Write the code**

Add the field, the two helpers, and the scoping in each method listed above. Keep the zero value's behavior identical to today; the existing tests must not change.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/gitx/gitx.go internal/gitx/gitx_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "gitx: a repository scopes every command to a prefix"
```

---

### Task 2: A vault inside a repository: `HostRepo`, `InitIn`, `Adopt`, and the engine over the prefixed repository

**Files:**
- Modify: `internal/vault/vault.go`, `internal/vault/vault_test.go`
- Modify: `internal/lint/lint_test.go` (`TestNewVaultHasNoFindings`)
- Modify: `internal/txn/txn_test.go`

**Interfaces:**
- Consumes: `gitx.Repo{Dir, Prefix}` (Task 1).
- Produces in `vault`:

```go
// InRepoDir is the folder a project inside a repository lives in: REPO/atlas/.
const InRepoDir = "atlas"

// HostRepo is the working tree that holds a vault inside a repository: root is named
// InRepoDir, has no .git of its own, and its parent has one. It is "" for a vault that
// is its own repository. Nothing stores this; a clone answers the same way.
func HostRepo(root string) string

// Repo is the vault's git repository: the vault itself, or the host repository scoped to
// the vault's folder.
func (v *Vault) Repo() gitx.Repo

// InitIn creates a project at REPO/atlas/, tracked by the repository's git, and commits
// it with the pathspec atlas/. REPO must be the top level of a git working tree; the
// folder must not exist or must be empty; the kind must be project.
func InitIn(repoRoot string, opts Options, now time.Time) (*InitResult, error)
```

Rules:
- `Repo()`: `if host := HostRepo(v.Root); host != "" { return gitx.Repo{Dir: host, Prefix: InRepoDir + "/"} }`; else `gitx.Repo{Dir: v.Root}`. `HostRepo` uses `os.Lstat` only (a `.git` file counts, as in a worktree). Cache nothing; the three stats are cheap.
- `InitIn`: `requireGit`; `repoRoot` absolute; `gitx.Repo{Dir: repoRoot}.IsRepo()` else "<repoRoot> is not the top level of a git repository"; `opts.Kind != Project` → "only a project lives inside a repository"; root `filepath.Join(repoRoot, InRepoDir)` must not exist or be empty (same message as `Init`); write the template with `writeMissing(root, cfg, now, true)`; `repo := gitx.Repo{Dir: repoRoot, Prefix: InRepoDir + "/"}`; `repo.AddAll()`; commit `CommitMessage("setup", "initialize project <name> (<mode> mode) in <basename(repoRoot)>", id)`; return the same `InitResult` shape `Init` returns.
- `Init` keeps refusing a vault inside another repository (its message stays; `new-project --in` is the way).
- `Adopt(root, …)` on a root whose `HostRepo` is not "": skip `git init` and the "inside another git repository" refusal, and run its baseline and setup commits over `v.Repo()` (the prefixed repository). Everything else unchanged. Read `Adopt` and its helpers (`baseline`, `committed`) — they take a `gitx.Repo`; pass the prefixed one.
- `txn` needs no change: it calls `v.Repo()` everywhere. Verify with `grep -n "gitx.Repo{" internal/txn/` — any direct construction must go through `v.Repo()`.

- [ ] **Step 1: Write the failing tests**

`vault_test.go`:

```go
func TestInitInCreatesAProjectInsideARepository(t *testing.T) {
	needGit(t)
	repoRoot := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(repoRoot, 0o755)
	host := gitx.Repo{Dir: repoRoot}
	if err := host.Init(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n"), 0o644)
	res, err := InitIn(repoRoot, Options{Kind: Project, Name: "Notes"}, now)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repoRoot, InRepoDir)
	if HostRepo(root) != repoRoot {
		t.Fatalf("HostRepo %q", HostRepo(root))
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		t.Fatal("the vault must not have its own .git")
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if v.Repo().Dir != repoRoot || v.Repo().Prefix != "atlas/" || v.Name() != "Notes" || v.Config.Kind != Project {
		t.Fatalf("%+v %+v", v.Repo(), v.Config)
	}
	changed, err := host.ChangedPaths(res.Commit)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range changed {
		if !strings.HasPrefix(p, "atlas/") {
			t.Fatalf("the setup commit touched %s", p)
		}
	}
	if st, _ := host.Status(); len(st) != 1 || st[0].Path != "main.go" {
		t.Fatalf("main.go must stay uncommitted: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); err != nil {
		t.Fatal("atlas/.gitignore must exist")
	}
}

func TestInitInRefusals(t *testing.T) {
	needGit(t)
	plain := filepath.Join(t.TempDir(), "plain")
	os.MkdirAll(plain, 0o755)
	if _, err := InitIn(plain, Options{Kind: Project}, now); err == nil || !strings.Contains(err.Error(), "not the top level of a git repository") {
		t.Fatalf("plain folder: %v", err)
	}
	repoRoot := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(repoRoot, 0o755)
	gitx.Repo{Dir: repoRoot}.Init()
	if _, err := InitIn(repoRoot, Options{Kind: Knowledge}, now); err == nil || !strings.Contains(err.Error(), "only a project") {
		t.Fatalf("knowledge base: %v", err)
	}
	if _, err := InitIn(repoRoot, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := InitIn(repoRoot, Options{Kind: Project}, now); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("twice: %v", err)
	}
	if HostRepo(filepath.Join(t.TempDir(), "atlas")) != "" {
		t.Fatal("a folder named atlas outside a repository is not inside one")
	}
}

func TestAdoptAcceptsAClonedProjectInsideARepository(t *testing.T) {
	needGit(t)
	repoRoot := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(repoRoot, 0o755)
	gitx.Repo{Dir: repoRoot}.Init()
	if _, err := InitIn(repoRoot, Options{Kind: Project, Name: "Notes"}, now); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(t.TempDir(), "clone")
	if err := (gitx.Repo{Dir: clone}).Clone(repoRoot); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(clone, InRepoDir)
	res, err := Adopt(root, Options{Kind: Project}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != Project || res.FromV1 {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		t.Fatal("adopt must not git init inside the clone")
	}
	v, _ := Open(root)
	if v.Repo().Dir != clone || v.Name() != "Notes" {
		t.Fatalf("%+v", v.Repo())
	}
}
```

`lint_test.go` `TestNewVaultHasNoFindings`: after the kinds loop, an in-repository project for each mode (`InitIn` under a fresh `git init` folder) lints clean too.

`txn_test.go`:

```go
func TestAnOperationInsideARepositoryCommitsOnlyTheVault(t *testing.T) {
	needGit(t)
	repoRoot := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(repoRoot, 0o755)
	host := gitx.Repo{Dir: repoRoot}
	host.Init()
	os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n"), 0o644)
	host.AddAll()
	host.Commit("code")
	if _, err := vault.InitIn(repoRoot, vault.Options{Kind: vault.Project, Name: "Notes"}, now); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(filepath.Join(repoRoot, vault.InRepoDir))
	if err != nil {
		t.Fatal(err)
	}
	// Half-written code outside the vault, and a hand edit inside it.
	os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main // half\n"), 0o644)
	os.WriteFile(v.Path("wiki/hot.md"), []byte("# hot\n\nby hand\n"), 0o644)
	page := "---\ntype: concept\ntitle: A\nstatus: seed\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n\n# A\n\ntext\n"
	plan, err := Prepare(v, Request{Kind: Save, Summary: "add A", Writes: []Write{{Path: "wiki/concepts/A.md", Mode: Create, Content: []byte(page)}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Apply(v, plan, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, sha := range []string{res.ManualCommit, res.Commit} {
		if sha == "" {
			continue
		}
		paths, _ := host.ChangedPaths(sha)
		for _, p := range paths {
			if !strings.HasPrefix(p, "atlas/") {
				t.Fatalf("commit %s touched %s", sha, p)
			}
		}
	}
	if res.ManualCommit == "" {
		t.Fatal("the hand edit must be committed first")
	}
	if st, _ := host.Status(); len(st) != 1 || st[0].Path != "main.go" {
		t.Fatalf("main.go must stay as it was: %+v", st)
	}
	ops, err := History(v, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) < 2 || ops[0].Kind != "save" {
		t.Fatalf("history %+v", ops)
	}
	if _, err := UndoOperation(v, ops[0].ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v.Path("wiki/concepts/A.md")); err == nil {
		t.Fatal("undo must remove A")
	}
	if data, _ := os.ReadFile(filepath.Join(repoRoot, "main.go")); string(data) != "package main // half\n" {
		t.Fatalf("undo touched the code: %q", data)
	}
}
```

(Use the file's helpers: `needGit`, `now`; read `txn_test.go`'s first test for the request and plan types.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vault/ ./internal/lint/ ./internal/txn/ 2>&1 | head`
Expected: compile errors (`InitIn`, `HostRepo`, `InRepoDir` undefined).

- [ ] **Step 3: Write the code**

`vault.go`: the constant, `HostRepo`, `Repo()`, `InitIn` (share `writeMissing` and the commit message helper with `Init`; extract the "exists and is not empty" check into a helper both use), and the `Adopt` branch.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/vault.go internal/vault/vault_test.go internal/lint/lint_test.go internal/txn/txn_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "vault: a project inside a repository commits with the pathspec atlas/"
```

---

### Task 3: `new-project --in`; the host repository is the project's repository

**Files:**
- Modify: `internal/vaults/vaults.go`, `internal/vaults/vaults_test.go` (or `identity_test.go`, where `Create` is tested)
- Modify: `internal/vaults/identity.go`, `internal/vaults/identity_test.go`
- Modify: `internal/registry/registry.go`, `internal/registry/registry_test.go`
- Modify: `internal/cli/cli.go`, `internal/cli/cli_test.go`
- Modify: `internal/discover/discover_test.go`
- Modify: `internal/hooks/hooks_test.go`

**Interfaces:**
- Consumes: `vault.InitIn`, `vault.HostRepo`, `vault.InRepoDir` (Task 2); `vaults.Create(path, opts, c, confirm)`; `vaults.Register`; `cli.finishVault`; `registry.resolve`; `vaults.AddRepo`/`CreateRepo`/`CloneRepo`/`RemoveRepo`/`EditRepo`.
- Produces in `vaults`:

```go
// CreateIn makes a project inside the repository at repoRoot, at REPO/atlas/, and
// returns the vault's path. It refuses a folder that is not a repository's top level.
func CreateIn(repoRoot string, opts vault.Options, c *console.Console) (string, error)
```

- Produces in `registry`: for a project whose root has a host repository, `resolve` prepends a `Repo{Name: filepath.Base(host), Path: host, Changes: <from an identity entry with that name, else "commit">, Remote: <from that entry, else "">}` to `e.Repos`, and drops the identity entry with that name from the rest of the list (the host entry carries its fields). `Entry.Host string` (`json:"host,omitempty"`) holds the host path so callers can tell the host repository from a linked one.
- Produces in `vaults`: `AddRepo`, `CreateRepo`, `CloneRepo` refuse the host repository's name ("<name> is the repository this project lives in"); `RemoveRepo` refuses it the same way; `EditRepo` on the host's name creates the identity entry when it is missing (so `edit-repo NAME REPO --changes pr` works) and edits it otherwise; a `--path` edit on it is refused.
- Produces in `cli`: `new-project NAME --in REPO`: `NAME` is the display name; the folder is `REPO/atlas`; `--in` and a path-like `NAME` together are refused ("with --in, NAME is the project's name; the folder is REPO/atlas"); after `CreateIn`, `EditIdentity` for `--tags`, then `finishVault(cfg, path)` (which registers the path, since it sits outside the vaults directory). `usage` gains `--in REPO`. `show` marks the host repository row "(this project lives in it)". `unlink` on it fails with the `RemoveRepo` message.

- [ ] **Step 1: Write the failing tests**

`registry_test.go` `TestAProjectInsideARepositoryListsItsHost`: a git repository `code` with `main.go`; `vault.InitIn(code, …, "Notes")`; a config whose `Vaults` lists `code/atlas` (outside `VaultsDir`); `Scan` → the entry has `Host == code`, `Repos[0].Name == "code"`, `Repos[0].Path == code`, `Repos[0].Changes == "commit"`; after `vault.UpdateConfig` adds `Repos: [{Name: "code", Changes: "pr"}]`, a rescan gives `Repos[0].Changes == "pr"` and still one repo.

`discover_test.go`: extend the existing test (or add one): a folder `code/src` inside the host repository matches the in-repository project with `Repo.Name == "code"`.

`identity_test.go` `TestTheHostRepositoryCannotBeLinkedOrRemoved`: with the entry above, `AddRepo(h, cfg, e, code, false, now)` → error containing "lives in"; `CreateRepo(…, "code", …)` → the same; `RemoveRepo(…, "code", …)` → the same; `EditRepo(…, "code", RepoEdit{Changes: "pr"}, now)` → the identity file has `Repos[0] == {Name: "code", Changes: "pr"}`; `EditRepo(…, "code", RepoEdit{Path: "/x"}, now)` → error.

`vaults_test.go` `TestCreateInMakesTheProjectAtRepoAtlas`: `CreateIn(code, Options{Kind: Project, Name: "Notes"}, console)` → the path is `code/atlas`, `vault.Open` works; `CreateIn` on a plain folder → error.

`cli_test.go` `TestNewProjectInARepository`: `setup(t)`; a git repository `code` under the temp root with one commit; `new-project Notes --in <code>` → exit 0, output contains "created" and "registered"; `show Notes` → contains "code" and "lives in"; `repos Notes` → contains `code` and `changes: commit`; `unlink Notes code` → exit 1 containing "lives in"; `edit-repo Notes code --changes pr` → exit 0; `repos Notes` → `changes: pr`; `new-project Other --in <code>` → exit 1 containing "already exists"; `new-project sub/dir --in <code>` → exit 2 (usage).

`hooks_test.go` `TestSessionStartInsideAHostRepository`: a temp home whose config lists `code/atlas`; `SessionStart` with `cwd = code/src` prints "this folder is the repository code of the project Notes" and the search sentence.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/registry/ ./internal/vaults/ ./internal/discover/ ./internal/cli/ ./internal/hooks/ 2>&1 | head -20`
Expected: compile errors or assertion failures (`CreateIn` undefined; `Host` undefined; `--in` unknown flag).

- [ ] **Step 3: Write the code**

`registry.go`: in `buildEntry` set `Host = vault.HostRepo(root)` for a project; in `resolve`, the host repository first. `identity.go`: a helper `hostName(e registry.Entry) string` (`filepath.Base(e.Host)` or ""), used by the four refusals and by `EditRepo`. `vaults.go`: `CreateIn` beside `Create`. `cli.go`: the flag, the refusal, the `show` mark, the `usage` line.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/vaults/vaults.go internal/vaults/vaults_test.go internal/vaults/identity.go internal/vaults/identity_test.go internal/registry/registry.go internal/registry/registry_test.go internal/cli/cli.go internal/cli/cli_test.go internal/discover/discover_test.go internal/hooks/hooks_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "vaults, registry, cli: new-project --in puts a project inside its repository"
```

---

### Task 4: Docs

**Files:**
- Modify: `CLAUDE.md`, `docs/usage.md`, `docs/v2-design.md`

- [ ] **Step 1: CLAUDE.md**

The write-path bullet adds `vault.InitIn`. A new bullet under Constraints: "A project may live inside a repository at `REPO/atlas/` (`new-project NAME --in REPO`). `vault.HostRepo` detects the layout from the filesystem; nothing stores it. Every git command the engine runs there takes the pathspec `atlas/` (`gitx.Repo.Prefix`), so an operation never commits code outside the vault. The host repository is the project's first repository in the registry (`Entry.Host`), without an entry in the identity file; `link`, `new-repo`, and `unlink` refuse its name, and `edit-repo` sets its change policy." The sources-of-truth v2 row: "(phases 1–5 built: …, projects inside repositories)".

- [ ] **Step 2: usage.md**

A section "A project inside a repository" after "Mount a knowledge base" (H3 under "## The atlas"): what `new-project NAME --in REPO` creates, that the repository's git tracks `atlas/` and every wiki commit lands on the current branch with the pathspec, that under `changes: pr` the wiki rides the pull request (set it with `edit-repo NAME REPO --changes pr`), that a clone is registered with `adopt REPO/atlas --as project`, and that `link`/`unlink` do not apply to the host repository. Four to six sentences. The command table row for `new-project` mentions `--in REPO`.

- [ ] **Step 3: v2-design.md**

The status line: "Phases 1–5 are built (…; projects inside repositories). Phases 6 and 7 are not." In "A project inside a repository", add one sentence: "The layout is detected, not stored: the root is named `atlas`, has no `.git`, and its parent has one (`vault.HostRepo`)." In "Left for later": the add screen has no "in repository" option (the CLI has it); `adopt` on a moved in-repository project keeps working because nothing is stored.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/usage.md docs/v2-design.md
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "docs: a project inside a repository"
```

---

## Self-review

**Spec coverage (phase 5):** the vault root `REPO/atlas/` with the repository's git (Task 2); every git command with the pathspec `atlas/`, the manual-edits commit and the operation commit only under `atlas/` (Tasks 1, 2); `atlas/.gitignore` from the project template (Task 2, the template already ignores `.vault-meta/`, `kb/`, `/repos/`, and Obsidian's workspace files); `undo` reverts the operation commit (Task 2's txn test); wiki commits on the current branch, `changes: pr` rides the pull request (Task 3's `EditRepo` rule, Task 4's docs); the repository is the project's repository without an identity entry (Task 3); the project sits outside the vaults directory and `new-project --in` registers it (Task 3 through `finishVault`).

**Placeholders:** none; the tests are written out. Task 3's `EditRepo` rule is stated as behavior with a test.

**Type consistency:** `gitx.Repo{Dir, Prefix}` (Task 1) is what `vault.Repo()` returns (Task 2); `vault.HostRepo`/`InRepoDir`/`InitIn` (Task 2) are what `registry`, `vaults.CreateIn`, and the CLI use (Task 3); `registry.Entry.Host` (Task 3) is what `vaults.hostName` reads (Task 3).
