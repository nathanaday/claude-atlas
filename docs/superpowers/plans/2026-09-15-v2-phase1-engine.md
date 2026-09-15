# v2 Phase 1: The Engine With Two Kinds — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A vault has a kind, `knowledge` or `project`, recorded in a v2 identity file with an id and a name; the layout, the operation kinds, the tools, lint, and the session hook follow the kind.

**Architecture:** `internal/vault` owns the v2 identity file (`Config` with `id`, `kind`, `name`), `Options` for `Init` and `Adopt`, and templates split into `common/`, `knowledge/`, and `project/`. `internal/txn` bounds writes by the vault's kind and refuses task operations in a knowledge base. `internal/lint` reports files and identity fields that do not belong to the kind. `internal/mcpserver` and `internal/hooks` gate the project-only tools and lines. The CLI gains `new-vault --kind` and `adopt --as`. The atlas tree, the TUI, mounts, and access come in later phases.

**Tech Stack:** Go 1.24.2, the MCP go-sdk v1.4.0, git.

**Spec:** `docs/v2-design.md`, sections "One engine, two kinds", "The identity file", "Operations", "Lint", and "Sessions". Phase 1 in "Phases".

## Global Constraints

- No new dependencies. Allowed: `gopkg.in/yaml.v3`, MCP `go-sdk` v1.4.0, Bubble Tea, Bubbles, Lip Gloss. UUIDs come from `crypto/rand`.
- The identity file never holds a path (spec: "Ids travel; paths stay").
- A knowledge base has no `inbox/`, `ideas/`, `wiki/tasks/`, task ledger, `wiki/questions/`, or `wiki/sessions/`. A project has all of them.
- Every write path goes through `txn.Prepare` and `txn.Apply`. `vault.Init`, `vault.Adopt`, and `vault.Upgrade` are the only code that writes vault files directly.
- Lint and refresh stay read-only toward every vault, offline, and idempotent. A new vault of either kind in either mode lints clean (`lint.TestNewVaultHasNoFindings`).
- Tests never touch a real `~/.claude-atlas`, never install a plugin, and skip when `git` is missing.
- No backward compatibility with v1 vaults: `vault.Open` refuses schema v1 and points at `adopt`. Existing vaults on the author's machine migrate by hand in phase 7.
- Every task ends with `go build ./... && go vet ./... && go test ./...` passing.
- Commits: author `nathanaday <nraday1221@gmail.com>` (pass `-c user.name=nathanaday -c user.email=nraday1221@gmail.com` to `git commit`); no `Co-Authored-By` line. Subject style: `area: what changed`, lower case, no period.
- Stage files by name.
- Code comments are one-line doc comments in the style of the surrounding code. Prose follows the user's writing guide in `~/.claude/CLAUDE.md`: short sentences, plain words, no metaphors.

## File map

| File | Change |
|---|---|
| `internal/vault/vault.go` | `Kind`, `Config` v2, `NewID`, `Options`, `ErrV1`, `ReadConfig`, `Init` and `Adopt` by kind, templates by kind, `Upgrade` by kind |
| `internal/vault/route.go` | `RoutableTypes(kind, mode)`, `folderFor` by kind, `Kind.Noun` |
| `internal/vault/templates/{common,knowledge,project}/` | the template split |
| `internal/vault/vault_test.go` | identity, layout, adopt, routing tests |
| `internal/txn/txn.go`, `txn_test.go` | `allowed` by vault kind; `Task` refused in a knowledge base |
| `internal/lint/lint.go`, `lint_test.go` | `kind_errors`, report version 3, new-vault test over kinds |
| `internal/mcpserver/server.go`, `server_test.go` | `status` kind and id; project-only tools refuse in a knowledge base; `plan` refuses `ingest` and `save` there |
| `internal/hooks/hooks.go`, `hooks_test.go` | session-start line by kind; task lines for projects only |
| `internal/vaults/vaults.go`, `vaults_test.go` | `Create(path, vault.Options, ...)` |
| `internal/cli/cli.go`, `cli_test.go` | `new-vault --kind`, `adopt --as` |
| `internal/wizard/wizard.go` | passes `vault.Options{Kind: vault.Project}` |
| test files that call `vault.Init` or write an identity file by hand | updated to the v2 forms |
| `CLAUDE.md` | one constraint line for kinds |

---

### Task 1: The v2 identity file: id, kind, name

**Files:**
- Modify: `internal/vault/vault.go`
- Modify: `internal/vault/vault_test.go`
- Modify (signature follow-through): `internal/vaults/vaults.go:55-77`, `internal/vaults/vaults_test.go:37-44`, `internal/wizard/wizard.go:181`, `internal/cli/cli.go:376,386,530,579,627`
- Modify (test call sites): `internal/lint/lint_test.go:286`, `internal/capture/capture_test.go:23`, `internal/tasks/tasks_test.go:108`, `internal/txn/txn_test.go:33`, `internal/refresh/refresh_test.go:184`, `internal/vaults/manage_test.go:298,555`, `internal/hooks/hooks_test.go:26`, `internal/mcpserver/server_test.go:75`
- Modify (hand-written identity files in tests): `internal/discover/discover_test.go:27`, `internal/tui/editor_test.go:25`, `internal/refresh/refresh_test.go:25`

**Interfaces:**
- Produces, in package `vault`:
  - `const Schema = "claude-atlas.vault.v2"`, `const SchemaV1 = "claude-atlas.vault.v1"`
  - `type Kind string`; `const Knowledge Kind = "knowledge"`, `Project Kind = "project"`; `var Kinds = []Kind{Knowledge, Project}`; `func ParseKind(s string) (Kind, error)`
  - `const AccessOpen = "open"`, `AccessGuarded = "guarded"`, `AccessRead = "read"`, `AccessWrite = "write"`
  - `type Grant struct{ ID, Name, Access string }`, `type Mount struct{ ID, Name, Access string }`, `type Repo struct{ Name, Remote, Changes string }`
  - `type Config struct{ Schema string; ID string; Kind Kind; Name string; Mode Mode; Created string; Scope string; Access string; Grants []Grant; Tags []string; Mounts []Mount; Repos []Repo }`
  - `func NewID() string`
  - `type Options struct{ Kind Kind; Mode Mode; Name string }`
  - `func Init(root string, opts Options, now time.Time) (*InitResult, error)`
  - `func Adopt(root string, opts Options, now time.Time) (*AdoptResult, error)`; `AdoptResult` gains `FromV1 bool` and `Kind Kind`
  - `var ErrV1 error`
  - `func ReadConfig(root string) (Config, bool)`
  - `func (v *Vault) Name() string` now returns `v.Config.Name`
- Produces, in package `vaults`: `func Create(path string, opts vault.Options, c *console.Console, confirm bool) (*vault.InitResult, error)`

- [ ] **Step 1: Write the failing tests**

In `internal/vault/vault_test.go`, add `"errors"` and `"regexp"` to the imports, and add after `needGit`:

```go
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
```

In `TestInitCreatesACompleteVaultWithOneCommit`, replace

```go
	res, err := Init(root, Generic, now)
```

with

```go
	res, err := Init(root, Options{Kind: Project, Mode: Generic}, now)
```

and replace

```go
	v, err := Open(root)
	if err != nil || v.Config.Mode != Generic || v.Config.Created != "2026-09-12" {
		t.Fatalf("open %+v %v", v, err)
	}
```

with

```go
	v, err := Open(root)
	if err != nil || v.Config.Schema != Schema || v.Config.Kind != Project || v.Config.Mode != Generic || v.Config.Created != "2026-09-12" {
		t.Fatalf("open %+v %v", v, err)
	}
	if v.Name() != "fresh" || !uuidPattern.MatchString(v.Config.ID) {
		t.Fatalf("name %q id %q", v.Name(), v.Config.ID)
	}
```

and replace `!strings.HasPrefix(commits[0].Subject, "setup: initialize vault")` with `!strings.HasPrefix(commits[0].Subject, "setup: initialize project fresh")`. Replace the second `Init(root, Generic, now)` in that test with `Init(root, Options{Kind: Project}, now)`.

In `TestInitRefusesInsideAnotherRepo`, `TestResolveOrder`, `TestRouteAndSkeleton`, and `TestUpgradeAndAdoptMoveTheTaskIndexFromItsOldPath`, replace every `Init(<root>, Generic, now)` with `Init(<root>, Options{Kind: Project, Mode: Generic}, now)`. In `TestAdoptKeepsExistingFilesAndFillsGaps` and `TestUpgradeAndAdoptMoveTheTaskIndexFromItsOldPath`, replace every `Adopt(<root>, "", now)` or `Adopt(<root>, Generic, now)` with `Adopt(<root>, Options{}, now)`, and every `Adopt(<root>, LYT, now)` with `Adopt(<root>, Options{Mode: LYT}, now)`. Keep each test's other assertions.

Add this test after `TestInitRefusesInsideAnotherRepo`:

```go
func TestIdentityFile(t *testing.T) {
	needGit(t)
	kb := filepath.Join(t.TempDir(), "ai-ml")
	if _, err := Init(kb, Options{Kind: Knowledge, Name: "AI and ML"}, now); err != nil {
		t.Fatal(err)
	}
	v, err := Open(kb)
	if err != nil {
		t.Fatal(err)
	}
	if v.Config.Kind != Knowledge || v.Config.Mode != Generic || v.Name() != "AI and ML" || v.Config.Access != AccessOpen {
		t.Fatalf("knowledge base config %+v", v.Config)
	}
	proot := filepath.Join(t.TempDir(), "p")
	if _, err := Init(proot, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	p, _ := Open(proot)
	if p.Config.ID == v.Config.ID || !uuidPattern.MatchString(p.Config.ID) {
		t.Fatalf("ids %q %q", p.Config.ID, v.Config.ID)
	}
	if p.Config.Access != "" || p.Name() != "p" {
		t.Fatalf("project config %+v", p.Config)
	}
	raw, _ := os.ReadFile(filepath.Join(proot, Marker))
	for _, absent := range []string{"scope", "access", "grants", "tags", "mounts", "repos"} {
		if strings.Contains(string(raw), `"`+absent+`"`) {
			t.Errorf("a fresh identity file carries %q:\n%s", absent, raw)
		}
	}
	if _, err := Init(filepath.Join(t.TempDir(), "x"), Options{}, now); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("kind is required: %v", err)
	}
	old := t.TempDir()
	os.WriteFile(filepath.Join(old, Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic","created":"2026-09-12"}`), 0o644)
	if _, err := Open(old); !errors.Is(err, ErrV1) || !strings.Contains(err.Error(), "adopt") {
		t.Fatalf("v1 open: %v", err)
	}
	if cfg, ok := ReadConfig(old); !ok || cfg.Schema != SchemaV1 || cfg.Mode != Generic {
		t.Fatalf("ReadConfig %+v %v", cfg, ok)
	}
	if _, ok := ReadConfig(t.TempDir()); ok {
		t.Fatal("no marker, no config")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vault/ 2>&1 | head -20`
Expected: compile errors: `undefined: Options`, `undefined: Knowledge`, `undefined: ErrV1`, `undefined: ReadConfig`.

- [ ] **Step 3: Write the identity file code**

In `internal/vault/vault.go`, change the schema constants:

```go
	Schema       = "claude-atlas.vault.v2"
	// SchemaV1 is the identity file older versions wrote; adopt rewrites it.
	SchemaV1 = "claude-atlas.vault.v1"
```

Replace the `Config` type and `Encode` with:

```go
// Kind says what a vault is for. A knowledge base holds sources, entities, and concepts.
// A project holds tasks, questions, and notes, and mounts knowledge bases.
type Kind string

const (
	Knowledge Kind = "knowledge"
	Project   Kind = "project"
)

var Kinds = []Kind{Knowledge, Project}

// ParseKind validates a kind name.
func ParseKind(s string) (Kind, error) {
	for _, k := range Kinds {
		if string(k) == s {
			return k, nil
		}
	}
	return "", fmt.Errorf("kind must be knowledge or project, not %q", s)
}

// Access levels. A knowledge base is open or guarded; a mount and a grant are read or write.
const (
	AccessOpen    = "open"
	AccessGuarded = "guarded"
	AccessRead    = "read"
	AccessWrite   = "write"
)

// Grant is a knowledge base's word on one project.
type Grant struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Access string `json:"access"`
}

// Mount is a project's use of one knowledge base.
type Mount struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Access string `json:"access"`
}

// Repo is a repository a project works in.
type Repo struct {
	Name    string `json:"name"`
	Remote  string `json:"remote,omitempty"`
	Changes string `json:"changes,omitempty"`
}

// Config is the content of the identity file: the facts that travel with the vault. It
// never holds a path; paths are facts about one machine and live in the atlas config.
type Config struct {
	Schema  string `json:"schema"`
	ID      string `json:"id"`
	Kind    Kind   `json:"kind"`
	Name    string `json:"name"`
	Mode    Mode   `json:"mode"`
	Created string `json:"created"`
	// A knowledge base's fields.
	Scope  string  `json:"scope,omitempty"`
	Access string  `json:"access,omitempty"`
	Grants []Grant `json:"grants,omitempty"`
	// A project's fields.
	Tags   []string `json:"tags,omitempty"`
	Mounts []Mount  `json:"mounts,omitempty"`
	Repos  []Repo   `json:"repos,omitempty"`
}

// Encode renders the identity file.
func (c Config) Encode() []byte {
	data, _ := json.MarshalIndent(c, "", "  ")
	return append(data, '\n')
}

// NewID mints a random UUID (version 4) for a vault.
func NewID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
```

Replace `Name`:

```go
// Name is the vault's name from its identity file.
func (v *Vault) Name() string { return v.Config.Name }
```

After `var ErrNotVault`, add:

```go
// ErrV1 means the identity file is from before v2; adopt rewrites it.
var ErrV1 = errors.New("v1 vault")

// ReadConfig parses the identity file without validating it. ok is false when there is
// none or it is not JSON. Lint and adopt read a vault this way; everything else opens it.
func ReadConfig(root string) (Config, bool) {
	data, err := os.ReadFile(filepath.Join(root, Marker))
	if err != nil {
		return Config{}, false
	}
	var cfg Config
	if json.Unmarshal(data, &cfg) != nil {
		return Config{}, false
	}
	return cfg, true
}
```

In `Open`, replace everything from `if cfg.Schema != Schema {` through the mode check with:

```go
	marker := filepath.Join(abs, Marker)
	switch cfg.Schema {
	case Schema:
	case SchemaV1:
		return nil, fmt.Errorf("%w: %s was made by claude-atlas v1; run `claude-atlas adopt %s --as knowledge` or `--as project`", ErrV1, abs, abs)
	default:
		return nil, fmt.Errorf("%s: unsupported schema %q", marker, cfg.Schema)
	}
	if _, err := ParseKind(string(cfg.Kind)); err != nil {
		return nil, fmt.Errorf("%s: %w", marker, err)
	}
	if cfg.ID == "" {
		return nil, fmt.Errorf("%s has no id; run `claude-atlas adopt %s`", marker, abs)
	}
	if cfg.Name == "" {
		cfg.Name = filepath.Base(abs)
	}
	if cfg.Mode == "" {
		cfg.Mode = Generic
	}
	if _, err := ParseMode(string(cfg.Mode)); err != nil {
		return nil, fmt.Errorf("%s: %w", marker, err)
	}
	return &Vault{Root: abs, Config: cfg}, nil
```

Before `Init`, add:

```go
// Options say what to make: the kind (required), the mode (generic by default), and the
// name (the directory's by default).
type Options struct {
	Kind Kind
	Mode Mode
	Name string
}

// newConfig is the identity file of a vault made now.
func newConfig(root string, opts Options, now time.Time) (Config, error) {
	kind, err := ParseKind(string(opts.Kind))
	if err != nil {
		return Config{}, fmt.Errorf("kind is required: %w", err)
	}
	mode := opts.Mode
	if mode == "" {
		mode = Generic
	}
	if _, err := ParseMode(string(mode)); err != nil {
		return Config{}, err
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = filepath.Base(root)
	}
	cfg := Config{Schema: Schema, ID: NewID(), Kind: kind, Name: name, Mode: mode, Created: now.Format("2006-01-02")}
	if kind == Knowledge {
		cfg.Access = AccessOpen
	}
	return cfg, nil
}
```

Change `Init` to `func Init(root string, opts Options, now time.Time) (*InitResult, error)`. After the `filepath.Abs` call, add:

```go
	cfg, err := newConfig(abs, opts, now)
	if err != nil {
		return nil, err
	}
```

Replace `writeMissing(abs, mode, now, true)` with `writeMissing(abs, cfg, now, true)`, and the commit line with:

```go
	sha, err := repo.Commit(CommitMessage("setup", fmt.Sprintf("initialize %s %s (%s mode)", cfg.Kind, cfg.Name, cfg.Mode), id))
```

Change `writeMissing` to `func writeMissing(root string, cfg Config, now time.Time, overwrite bool) ([]string, error)` and replace the marker line with:

```go
	if err := put(Marker, cfg.Encode()); err != nil {
		return nil, err
	}
```

In `Upgrade`, replace `writeMissing(abs, v.Config.Mode, now, false)` with `writeMissing(abs, v.Config, now, false)`.

Replace `AdoptResult` and `Adopt` with:

```go
// AdoptResult reports what Adopt changed.
type AdoptResult struct {
	Root           string
	Kind           Kind
	OperationID    string
	Commit         string
	Added          []string
	Moved          []Move
	GitInitialized bool
	WasLegacy      bool
	AlreadyAdopted bool
	// FromV1 is set when a v1 identity file was rewritten.
	FromV1 bool
}

// adoptConfig decides the identity file adopt writes: a current one is kept, a v1 one is
// rewritten with its mode and creation date, and a vault without one gets a new one.
// Without a kind, adopt keeps the vault's kind or makes it a project.
func adoptConfig(root string, existing Config, hasMarker bool, opts Options, now time.Time) (Config, error) {
	if hasMarker && existing.Schema == Schema {
		if opts.Kind != "" && opts.Kind != existing.Kind {
			return Config{}, fmt.Errorf("%s is already a %s; a vault's kind does not change", root, existing.Kind)
		}
		return existing, nil
	}
	if opts.Kind == "" {
		opts.Kind = Project
	}
	if opts.Mode == "" {
		opts.Mode = existing.Mode
	}
	if opts.Mode == "" && IsLegacy(root) {
		opts.Mode = legacyMode(root)
	}
	cfg, err := newConfig(root, opts, now)
	if err != nil {
		return Config{}, err
	}
	if existing.Created != "" {
		cfg.Created = existing.Created
	}
	return cfg, nil
}

// Adopt turns an existing directory, an Obsidian vault, a claude-obsidian vault, or a v1
// vault into a claude-atlas vault of a kind. It adds only what is missing and commits a
// baseline that includes every file already there. It never replaces or removes a file,
// except that a v1 identity file is rewritten and files an older version put at other
// paths are moved.
func Adopt(root string, opts Options, now time.Time) (*AdoptResult, error) {
	if err := requireGit(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	if !IsAdoptable(abs) {
		return nil, fmt.Errorf("%s is not a vault: it has no .obsidian/, wiki/, or vault identity file; create one with `claude-atlas new-vault`", abs)
	}
	existing, hasMarker := ReadConfig(abs)
	res := &AdoptResult{Root: abs, WasLegacy: IsLegacy(abs), AlreadyAdopted: hasMarker && existing.Schema == Schema, FromV1: hasMarker && existing.Schema != Schema}
	cfg, err := adoptConfig(abs, existing, hasMarker, opts, now)
	if err != nil {
		return nil, err
	}
	res.Kind = cfg.Kind
	repo := gitx.Repo{Dir: abs}
	if repo.InsideOtherRepo() {
		return nil, fmt.Errorf("%s is inside another git repository; a vault keeps its own history", abs)
	}
	if res.AlreadyAdopted {
		if res.Moved, err = moveLegacy(abs); err != nil {
			return nil, err
		}
	}
	if res.FromV1 {
		if err := writeFile(abs, Marker, cfg.Encode()); err != nil {
			return nil, err
		}
		res.Added = append(res.Added, Marker)
	}
	added, err := writeMissing(abs, cfg, now, false)
	if err != nil {
		return nil, err
	}
	res.Added = append(res.Added, added...)
	sort.Strings(res.Added)
	if !repo.IsRepo() {
		if err := repo.Init(); err != nil {
			return nil, err
		}
		res.GitInitialized = true
	}
	dirty, err := repo.Dirty()
	if err != nil {
		return nil, err
	}
	if !dirty && repo.HasHead() {
		return res, nil
	}
	if err := repo.AddAll(); err != nil {
		return nil, err
	}
	res.OperationID = NewOperationID("setup", now)
	what := fmt.Sprintf("adopt %s %s", cfg.Kind, cfg.Name)
	switch {
	case res.WasLegacy:
		what = fmt.Sprintf("adopt claude-obsidian vault as %s %s", cfg.Kind, cfg.Name)
	case res.FromV1:
		what = fmt.Sprintf("adopt v1 vault as %s %s", cfg.Kind, cfg.Name)
	}
	sha, err := repo.Commit(CommitMessage("setup", what, res.OperationID))
	if err != nil {
		return nil, err
	}
	res.Commit = sha
	return res, nil
}
```

- [ ] **Step 4: Follow the signature through the callers**

`internal/vaults/vaults.go`: change `Create` to

```go
// Create makes a new vault at path after showing what it will contain.
func Create(path string, opts vault.Options, c *console.Console, confirm bool) (*vault.InitResult, error) {
	if err := CheckNewPath(path); err != nil {
		return nil, err
	}
	if opts.Mode == "" {
		opts.Mode = vault.Generic
	}
	if confirm {
		files := append(vault.TemplateFiles(), vault.Marker, vault.LedgerPath)
		c.Say("claude-atlas will create the %s %s (%s mode) with %d files and a git repository:", opts.Kind, home.Display(path), opts.Mode, len(files))
		for _, item := range files {
			c.Say("    %s", item)
		}
		c.Say("")
		ok, err := c.Confirm("Create this vault?", true)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrCancelled
		}
	}
	return vault.Init(path, opts, time.Now())
}
```

`internal/vaults/vaults_test.go:37-44`: replace each `Create(<path>, vault.Generic, nil, false)` with `Create(<path>, vault.Options{Kind: vault.Project}, nil, false)`.

`internal/wizard/wizard.go:181`: `vaults.Create(firstPath, vault.Options{Kind: vault.Project}, c, false)`.

`internal/cli/cli.go`:
- line 376: `vault.Adopt(choice.Path, vault.Options{Mode: mode, Name: choice.Name}, time.Now())`
- line 386: `vaults.Create(choice.Path, vault.Options{Kind: vault.Project, Mode: mode, Name: choice.Name}, e.console, false)`
- line 530: `vault.Adopt(abs, vault.Options{Mode: mode, Name: opts.Name}, time.Now())`
- line 579: `vaults.Create(path, vault.Options{Kind: vault.Project, Mode: mode, Name: opts.Name}, e.console, true)`
- line 627: `vaults.Create(choice.Path, vault.Options{Kind: vault.Project, Mode: mode, Name: choice.Name}, e.console, false)`

Test call sites: in each file listed under **Files**, replace `vault.Init(<root>, vault.Generic, <time>)` with `vault.Init(<root>, vault.Options{Kind: vault.Project, Mode: vault.Generic}, <time>)`, and in `internal/lint/lint_test.go:286` with `vault.Init(root, vault.Options{Kind: vault.Project, Mode: mode}, asOf)`.

Hand-written identity files: in `internal/discover/discover_test.go:27`, `internal/tui/editor_test.go:25`, and `internal/refresh/refresh_test.go:25`, replace the JSON string with:

```
{"schema":"claude-atlas.vault.v2","id":"00000000-0000-4000-8000-000000000001","kind":"project","name":"v","mode":"generic","created":"2026-09-12"}
```

(keep whatever `created` value the test had if it asserts on it). `internal/vaults/manage_test.go:167` writes `{}` to mark a folder as a vault for `CheckMove`; leave it.

Run `grep -rn "vault.Init(\|vault.Adopt(\|vaults.Create(" --include='*.go' internal/ cmd/` and confirm every call uses the new forms.

- [ ] **Step 5: Run the build and the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass. `TestInitCreatesACompleteVaultWithOneCommit` and `TestIdentityFile` pass. If `hooks_test` or `server_test` compare a name, the default name is still the directory name, so nothing changes there.

- [ ] **Step 6: Commit**

```bash
git add internal/vault/vault.go internal/vault/vault_test.go internal/vaults/vaults.go internal/vaults/vaults_test.go internal/wizard/wizard.go internal/cli/cli.go internal/lint/lint_test.go internal/capture/capture_test.go internal/tasks/tasks_test.go internal/txn/txn_test.go internal/refresh/refresh_test.go internal/vaults/manage_test.go internal/hooks/hooks_test.go internal/mcpserver/server_test.go internal/discover/discover_test.go internal/tui/editor_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "vault: the v2 identity file carries an id, a kind, and a name"
```

---

### Task 2: Templates by kind

**Files:**
- Move: `internal/vault/templates/*` into `internal/vault/templates/common/`, `internal/vault/templates/knowledge/`, `internal/vault/templates/project/`
- Modify: `internal/vault/vault.go` (`TemplateFiles`, `renderTemplate`, `writeMissing`)
- Modify: `internal/vault/vault_test.go`
- Modify: `internal/lint/lint_test.go` (`TestNewVaultHasNoFindings`)
- Modify: `internal/vaults/vaults.go` (`TemplateFiles(opts.Kind)`)

**Interfaces:**
- Consumes: `vault.Options`, `vault.Config`, `vault.Kinds` from Task 1.
- Produces: `func TemplateFiles(kind Kind) []string`. A knowledge base's template has no `inbox/`, `ideas/`, `wiki/tasks/`; its `.gitignore` is the common one; a project's `.gitignore` ignores `/kb/`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/vault/vault_test.go`:

```go
func TestInitLayoutByKind(t *testing.T) {
	needGit(t)
	kb := filepath.Join(t.TempDir(), "kb")
	if _, err := Init(kb, Options{Kind: Knowledge}, now); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{InboxDir, InboxTasksDir, IdeasDir, TasksDir, TaskLedgerPath} {
		if _, err := os.Stat(filepath.Join(kb, filepath.FromSlash(rel))); err == nil {
			t.Errorf("a knowledge base has %s", rel)
		}
	}
	for _, rel := range []string{Marker, ".gitignore", AppFile, AppearanceFile, LogPage, HotPage, IndexPage, OverviewPage, LedgerPath} {
		if _, err := os.Stat(filepath.Join(kb, filepath.FromSlash(rel))); err != nil {
			t.Errorf("a knowledge base lacks %s", rel)
		}
	}
	index, _ := os.ReadFile(filepath.Join(kb, "wiki", "index.md"))
	if strings.Contains(string(index), "Questions") || !strings.Contains(string(index), "## Concepts") {
		t.Fatalf("knowledge base index:\n%s", index)
	}
	hot, _ := os.ReadFile(filepath.Join(kb, "wiki", "hot.md"))
	if strings.Contains(string(hot), "inbox/") {
		t.Fatalf("a knowledge base's hot cache must not point at an inbox:\n%s", hot)
	}
	for _, f := range TemplateFiles(Knowledge) {
		if strings.HasPrefix(f, "inbox/") || strings.HasPrefix(f, "ideas/") || strings.HasPrefix(f, "wiki/tasks/") {
			t.Errorf("knowledge template lists %s", f)
		}
	}
	p := filepath.Join(t.TempDir(), "p")
	if _, err := Init(p, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"inbox/.gitkeep", "inbox/tasks/.gitkeep", "ideas/.gitkeep", TasksIndex, TaskLedgerPath, LedgerPath, ".obsidian/snippets/claude-atlas.css"} {
		if _, err := os.Stat(filepath.Join(p, filepath.FromSlash(rel))); err != nil {
			t.Errorf("a project lacks %s", rel)
		}
	}
	ignore, _ := os.ReadFile(filepath.Join(p, ".gitignore"))
	if !strings.Contains(string(ignore), "/kb/") {
		t.Fatalf("a project ignores its mounts:\n%s", ignore)
	}
	kbIgnore, _ := os.ReadFile(filepath.Join(kb, ".gitignore"))
	if strings.Contains(string(kbIgnore), "/kb/") {
		t.Fatalf("a knowledge base has no mounts to ignore:\n%s", kbIgnore)
	}
}
```

In `internal/lint/lint_test.go`, replace `TestNewVaultHasNoFindings` with:

```go
func TestNewVaultHasNoFindings(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	asOf := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	for _, kind := range vault.Kinds {
		for _, mode := range vault.Modes {
			root := filepath.Join(t.TempDir(), string(kind)+"-"+string(mode))
			if _, err := vault.Init(root, vault.Options{Kind: kind, Mode: mode}, asOf); err != nil {
				t.Fatal(err)
			}
			r, err := Run(root, Options{AsOf: asOf})
			if err != nil {
				t.Fatal(err)
			}
			if r.Summary.IssuesFound != 0 || r.Summary.WantedPages != 0 || r.Summary.Stubs != 0 {
				t.Errorf("%s vault in %s mode:\n%s", kind, mode, r.Markdown())
			}
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vault/ ./internal/lint/ 2>&1 | head`
Expected: compile error `too many arguments in call to TemplateFiles` in `vault_test.go`; once past that, "a knowledge base has inbox".

- [ ] **Step 3: Split the templates**

```bash
cd internal/vault/templates
mkdir -p common/.obsidian common/wiki knowledge/wiki project/wiki
git mv .obsidian/app.json .obsidian/appearance.json .obsidian/graph.json common/.obsidian/
git mv .obsidian/snippets common/.obsidian/snippets
git mv wiki/log.md wiki/overview.md common/wiki/
git mv .gitignore knowledge/.gitignore
cp knowledge/.gitignore project/.gitignore
git mv wiki/index.md project/wiki/index.md
git mv wiki/hot.md project/wiki/hot.md
git mv wiki/tasks project/wiki/tasks
git mv inbox project/inbox
git mv ideas project/ideas
rmdir .obsidian wiki 2>/dev/null; true
```

Append to `project/.gitignore`:

```
# a project's mounted knowledge bases: symlinks the atlas recreates
/kb/
```

Create `knowledge/wiki/index.md`:

```markdown
---
type: meta
title: Wiki Index
status: evergreen
created: {{generated_date}}
updated: {{generated_date}}
tags:
  - meta
  - index
---

# Wiki Index

Every page in this knowledge base is listed here. Completed operations keep it current.

## Sources

- No sources yet.

## Concepts

- No concepts yet.

## Entities

- No entities yet.
```

Create `knowledge/wiki/hot.md`:

```markdown
---
type: meta
title: Hot Cache
status: developing
created: {{generated_date}}
updated: {{generated_date}}
tags:
  - meta
  - hot-cache
---

# Recent Context

## Last Updated

Knowledge base initialized. No knowledge operations have completed yet.

## Key Recent Facts

- No facts recorded.

## Recent Changes

- Created the knowledge base.

## Active Threads

- Mount this knowledge base in a project, then ingest a source there.
```

- [ ] **Step 4: Read the templates by kind**

In `internal/vault/vault.go`, replace `TemplateFiles` and `renderTemplate` with:

```go
// TemplateFiles lists the vault-relative paths the template provides for a kind: the
// files every vault has, then the kind's own. A kind's file wins over a common one.
func TemplateFiles(kind Kind) []string {
	seen := map[string]bool{}
	var out []string
	for _, dir := range templateDirs(kind) {
		fs.WalkDir(templates, dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				rel := strings.TrimPrefix(path, dir+"/")
				if !seen[rel] {
					seen[rel] = true
					out = append(out, rel)
				}
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// templateDirs are the embedded folders a kind draws on, the kind's own first.
func templateDirs(kind Kind) []string {
	return []string{"templates/" + string(kind), "templates/common"}
}

func renderTemplate(kind Kind, rel string, now time.Time) ([]byte, error) {
	var data []byte
	var err error
	for _, dir := range templateDirs(kind) {
		if data, err = templates.ReadFile(dir + "/" + rel); err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	return bytes.ReplaceAll(data, []byte("{{generated_date}}"), []byte(now.Format("2006-01-02"))), nil
}
```

In `writeMissing`, replace `for _, rel := range TemplateFiles() {` with `for _, rel := range TemplateFiles(cfg.Kind) {`, `renderTemplate(rel, now)` with `renderTemplate(cfg.Kind, rel, now)`, and the task ledger line with:

```go
	if cfg.Kind == Project {
		if err := put(TaskLedgerPath, []byte(EmptyTaskLedger)); err != nil {
			return nil, err
		}
	}
```

In `internal/vaults/vaults.go`, `Create`: `files := append(vault.TemplateFiles(opts.Kind), vault.Marker, vault.LedgerPath)`.

- [ ] **Step 5: Run the build and the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass, including `TestInitLayoutByKind`, `TestNewVaultHasNoFindings` over four vaults, and `TestNewNotesGoUnderTheWikiUnlessTheUserChose` (which reads `AppFile` through the common folder).

- [ ] **Step 6: Commit**

```bash
git add -A internal/vault/templates internal/vault/vault.go internal/vault/vault_test.go internal/lint/lint_test.go internal/vaults/vaults.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "vault: templates by kind; a knowledge base has no inbox, ideas, or tasks"
```

---

### Task 3: Adopt as a knowledge base removes the task scaffolding; Upgrade by kind

**Files:**
- Modify: `internal/vault/vault.go` (`Adopt`, `Upgrade`, `removeProjectFiles`, `inboxHoldsSources`)
- Modify: `internal/vault/vault_test.go`

**Interfaces:**
- Consumes: `Adopt(root, Options, now)` and `AdoptResult` from Task 1.
- Produces: `AdoptResult.Removed []string`; `Adopt` with `Kind: Knowledge` on a vault without a v2 identity file removes `ideas/`, `inbox/`, `wiki/meta/ledgers/task-ledger.json`, and `wiki/tasks/`, and refuses when `inbox/` holds files other than task notes. `Upgrade` moves the task index only in a project.

- [ ] **Step 1: Write the failing test**

Add to `internal/vault/vault_test.go`:

```go
func TestAdoptAsKnowledgeRemovesTaskScaffolding(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "old")
	if _, err := Init(root, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"lyt","created":"2026-09-01"}`), 0o644)
	os.WriteFile(filepath.Join(root, "wiki", "tasks", "Do it.md"), []byte("---\ntype: task\ntitle: Do it\n---\n"), 0o644)
	os.WriteFile(filepath.Join(root, "inbox", "tasks", "note.md"), []byte("later\n"), 0o644)
	repo := gitx.Repo{Dir: root}
	repo.AddAll()
	repo.Commit("old state")
	res, err := Adopt(root, Options{Kind: Knowledge}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res.FromV1 || res.Kind != Knowledge || strings.Join(res.Removed, ",") != "ideas,inbox,wiki/meta/ledgers/task-ledger.json,wiki/tasks" {
		t.Fatalf("result %+v", res)
	}
	v, err := Open(root)
	if err != nil || v.Config.Kind != Knowledge || v.Config.Mode != LYT || v.Config.Created != "2026-09-01" || v.Config.ID == "" {
		t.Fatalf("open %+v %v", v, err)
	}
	for _, rel := range res.Removed {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s still exists", rel)
		}
	}
	if dirty, _ := repo.Dirty(); dirty {
		t.Fatal("adopt must leave the tree clean")
	}
	commits, _ := repo.Log(1)
	if !strings.HasPrefix(commits[0].Subject, "setup: adopt v1 vault as knowledge old") {
		t.Fatalf("commit %+v", commits[0])
	}
	if again, err := Adopt(root, Options{}, now); err != nil || again.Commit != "" || !again.AlreadyAdopted {
		t.Fatalf("second adopt %+v %v", again, err)
	}
	if _, err := Adopt(root, Options{Kind: Project}, now); err == nil || !strings.Contains(err.Error(), "does not change") {
		t.Fatalf("kind is fixed: %v", err)
	}

	withSources := filepath.Join(t.TempDir(), "busy")
	if _, err := Init(withSources, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(withSources, Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic","created":"2026-09-01"}`), 0o644)
	os.WriteFile(filepath.Join(withSources, "inbox", "paper.pdf"), []byte("%PDF"), 0o644)
	if _, err := Adopt(withSources, Options{Kind: Knowledge}, now); err == nil || !strings.Contains(err.Error(), "inbox/ holds 1 file") {
		t.Fatalf("an inbox with sources stops the removal: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vault/ -run TestAdoptAsKnowledgeRemovesTaskScaffolding`
Expected: FAIL: `res.Removed undefined` (compile), then "result ... Removed: []".

- [ ] **Step 3: Remove what a knowledge base does not have**

In `internal/vault/vault.go`, add `Removed []string` to `AdoptResult` after `Moved`, and add after `moveLegacy`:

```go
// projectOnly are the paths a knowledge base does not have. Adopting as a knowledge base
// removes them; the user agreed that tasks and ideas start fresh.
var projectOnly = []string{IdeasDir, InboxDir, TaskLedgerPath, TasksDir}

// inboxSources counts the files in inbox/ other than task notes and dotfiles: sources
// nobody has ingested, which adopt must not delete.
func inboxSources(root string) int {
	n := 0
	filepath.WalkDir(filepath.Join(root, InboxDir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p == filepath.Join(root, filepath.FromSlash(InboxTasksDir)) || strings.HasPrefix(d.Name(), ".") && p != filepath.Join(root, InboxDir) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasPrefix(d.Name(), ".") {
			n++
		}
		return nil
	})
	return n
}

// removeProjectFiles deletes the project-only paths that exist and reports them.
func removeProjectFiles(root string) ([]string, error) {
	if n := inboxSources(root); n > 0 {
		return nil, fmt.Errorf("inbox/ holds %d file%s; ingest them through a project or move them out before adopting as a knowledge base", n, map[bool]string{true: "", false: "s"}[n == 1])
	}
	var removed []string
	for _, rel := range projectOnly {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return removed, err
		}
		removed = append(removed, rel)
	}
	return removed, nil
}
```

In `Adopt`, after the `repo.InsideOtherRepo()` check, add:

```go
	if cfg.Kind == Knowledge && !res.AlreadyAdopted {
		if res.Removed, err = removeProjectFiles(abs); err != nil {
			return nil, err
		}
	}
```

and after computing `what`, add:

```go
	if len(res.Removed) > 0 {
		what += "; removed " + strings.Join(res.Removed, ", ")
	}
```

In `Upgrade`, replace `if res.Moved, err = moveLegacy(abs); err != nil {` with:

```go
	if v.Config.Kind == Project {
		if res.Moved, err = moveLegacy(abs); err != nil {
			return res, err
		}
	}
```

(Keep the existing `return res, err` body; the diff adds the `if` around it.)

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/vault/ && go build ./... && go vet ./... && go test ./...`
Expected: all pass. `TestAdoptKeepsExistingFilesAndFillsGaps` still adopts as a project and keeps every file.

- [ ] **Step 5: Commit**

```bash
git add internal/vault/vault.go internal/vault/vault_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "vault: adopt --as knowledge rewrites a v1 identity file and removes the task scaffolding"
```

---

### Task 4: Routing and write scope follow the kind

**Files:**
- Modify: `internal/vault/route.go`
- Modify: `internal/vault/vault_test.go`
- Modify: `internal/txn/txn.go` (`allowed`, `Prepare`)
- Modify: `internal/txn/txn_test.go`
- Modify: `internal/mcpserver/server.go:581,600` (`RoutableTypes` calls; the rest of the server changes in Task 6)

**Interfaces:**
- Consumes: `vault.Kind`, `vault.Knowledge`, `vault.Project`.
- Produces: `func RoutableTypes(kind Kind, mode Mode) []string`; `func (k Kind) Noun() string` ("knowledge base" or "project"); in `txn`, `allowed(vk vault.Kind, kind Kind, p string, mode WriteMode) error`; `Prepare` refuses `Task` in a knowledge base.

- [ ] **Step 1: Write the failing tests**

Add to `internal/vault/vault_test.go`:

```go
func TestRoutableTypesByKind(t *testing.T) {
	needGit(t)
	if got := strings.Join(RoutableTypes(Knowledge, Generic), ","); got != "source,entity,concept" {
		t.Fatalf("knowledge generic: %s", got)
	}
	if got := strings.Join(RoutableTypes(Project, LYT), ","); got != "note,moc,source,entity,concept,question,session" {
		t.Fatalf("project lyt: %s", got)
	}
	kb := filepath.Join(t.TempDir(), "kb")
	Init(kb, Options{Kind: Knowledge}, now)
	v, _ := Open(kb)
	if _, err := v.RouteFor("question", "Why", now); err == nil || !strings.Contains(err.Error(), "knowledge base") {
		t.Fatalf("questions belong to a project: %v", err)
	}
	r, err := v.RouteFor("concept", "Backpropagation", now)
	if err != nil || r.Path != "wiki/concepts/Backpropagation.md" {
		t.Fatalf("route %+v %v", r, err)
	}
	if Knowledge.Noun() != "knowledge base" || Project.Noun() != "project" {
		t.Fatal("nouns")
	}
}
```

In `internal/txn/txn_test.go`, add after `newVault`:

```go
func newKnowledge(t *testing.T) *vault.Vault {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := filepath.Join(t.TempDir(), "kb")
	if _, err := vault.Init(root, vault.Options{Kind: vault.Knowledge}, now); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
```

and add this test at the end of the file:

```go
func TestKnowledgeBaseBoundsWrites(t *testing.T) {
	v := newKnowledge(t)
	cases := []struct {
		name string
		req  Request
		want string
	}{
		{"task kind", Request{Kind: Task, Summary: "x", Writes: []Write{{Path: "wiki/tasks/A.md", Mode: Create, Content: mkpage("A", "")}}}, "no tasks"},
		{"question", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: "wiki/questions/Q.md", Mode: Create, Content: mkpage("Q", "")}}}, "belongs to a project"},
		{"session", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: "wiki/sessions/S.md", Mode: Create, Content: mkpage("S", "")}}}, "belongs to a project"},
		{"inbox", Request{Kind: Ingest, Summary: "x", Writes: []Write{{Path: "inbox/a.md", Mode: Delete}}}, "belongs to a project"},
		{"ideas", Request{Kind: Repair, Summary: "x", Writes: []Write{{Path: "ideas/a.md", Mode: Create, Content: []byte("x")}}}, "belongs to a project"},
	}
	for _, c := range cases {
		if _, err := Prepare(v, c.req, now); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
	req, _, err := PlantRequest(v, tasks.Plant{Title: "T", Text: "t"}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(v, req, now); err == nil || !strings.Contains(err.Error(), "no tasks") {
		t.Fatalf("plant in a knowledge base: %v", err)
	}
	plan, err := Prepare(v, Request{Kind: Save, Summary: "add A", Writes: []Write{{Path: "wiki/concepts/A.md", Mode: Create, Content: mkpage("A", "text\n")}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(v, plan, now); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vault/ ./internal/txn/ 2>&1 | head`
Expected: compile errors: `not enough arguments in call to RoutableTypes`, `Knowledge.Noun undefined`.

- [ ] **Step 3: Route by kind**

In `internal/vault/route.go`, replace `RoutableTypes` and `folderFor` with:

```go
// RoutableTypes lists the page types a vault of a kind files in a mode. A knowledge base
// holds sources, entities, and concepts; a project adds questions and sessions.
func RoutableTypes(kind Kind, mode Mode) []string {
	types := []string{"source", "entity", "concept"}
	if kind == Project {
		types = append(types, "question", "session")
	}
	if mode == LYT {
		types = append([]string{"note", "moc"}, types...)
	}
	return types
}

// Noun is the kind as a person says it.
func (k Kind) Noun() string {
	if k == Knowledge {
		return "knowledge base"
	}
	return string(k)
}

func folderFor(kind Kind, mode Mode, pageType string) (string, error) {
	types := RoutableTypes(kind, mode)
	found := false
	for _, t := range types {
		if t == pageType {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("type %q is not filed in a %s in %s mode; use one of %s", pageType, kind.Noun(), mode, strings.Join(types, ", "))
	}
	if mode == LYT {
		if pageType == "moc" {
			return "wiki/mocs", nil
		}
		return "wiki/notes", nil
	}
	return genericFolders[pageType], nil
}
```

In `RouteFor`, replace `folder, err := folderFor(mode, pageType)` with `folder, err := folderFor(v.Config.Kind, mode, pageType)`.

In `internal/mcpserver/server.go`, replace `vault.RoutableTypes(v.Config.Mode)` (line 581) with `vault.RoutableTypes(v.Config.Kind, v.Config.Mode)` and `vault.RoutableTypes(mode)` (line 600) with `vault.RoutableTypes(v.Config.Kind, mode)`.

- [ ] **Step 4: Bound writes by kind**

In `internal/txn/txn.go`, change `allowed` to take the vault's kind and check it first:

```go
// allowed enforces each kind's write scope, and the vault kind's: a knowledge base has
// no inbox, ideas, tasks, questions, or sessions.
func allowed(vk vault.Kind, kind Kind, p string, mode WriteMode) error {
	under := func(dir string) bool { return strings.HasPrefix(p, dir+"/") }
	if vk == vault.Knowledge {
		for _, dir := range []string{vault.InboxDir, vault.IdeasDir, vault.TasksDir, "wiki/questions", "wiki/sessions"} {
			if under(dir) {
				return fmt.Errorf("%s/ belongs to a project, not a knowledge base: %s", dir, p)
			}
		}
		if p == vault.TaskLedgerPath {
			return fmt.Errorf("%s belongs to a project, not a knowledge base", p)
		}
	}
	switch {
```

Delete the later line `under := func(dir string) bool { return strings.HasPrefix(p, dir+"/") }` (it now sits at the top). In `Prepare`, replace `if err := allowed(req.Kind, p, w.Mode); err != nil {` with `if err := allowed(v.Config.Kind, req.Kind, p, w.Mode); err != nil {`, and after the `validKind` check at the top of `Prepare`, add:

```go
	if v.Config.Kind == vault.Knowledge && req.Kind == Task {
		return nil, errors.New("a knowledge base has no tasks; plant the task in a project that mounts it")
	}
```

- [ ] **Step 5: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass, including `TestRoutableTypesByKind`, `TestKnowledgeBaseBoundsWrites`, and the unchanged `TestRouteAndSkeleton`, `TestPrepareValidates`, and `TestTaskKindBoundsWrites`.

- [ ] **Step 6: Commit**

```bash
git add internal/vault/route.go internal/vault/vault_test.go internal/txn/txn.go internal/txn/txn_test.go internal/mcpserver/server.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "txn, vault: a knowledge base files no questions, sessions, or tasks, and no plan writes them there"
```

---

### Task 5: Lint reports what does not belong to the kind

**Files:**
- Modify: `internal/lint/lint.go`
- Modify: `internal/lint/lint_test.go`

**Interfaces:**
- Consumes: `vault.ReadConfig`, `vault.Schema`, `vault.Config` fields.
- Produces: `Report.KindErrors []PathFinding` (`kind_errors`), counted in `Summary.CategoryCounts["kind_errors"]` and `IssuesFound`; `ReportVersion = 3`; a "Kind" section in the Markdown report.

- [ ] **Step 1: Write the failing test**

Add to `internal/lint/lint_test.go`:

```go
func TestKindErrors(t *testing.T) {
	asOf := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	kb := fixture(t, map[string]string{
		".claude-atlas.json":                 `{"schema":"claude-atlas.vault.v2","id":"1","kind":"knowledge","name":"kb","mode":"generic","created":"2026-09-14","mounts":[{"id":"2","name":"p","access":"write"}]}`,
		"wiki/index.md":                      mkpage("Index", "# Index\n"),
		"inbox/paper.md":                     "x",
		"wiki/tasks/tasks.md":                mkpage("Tasks", "# Tasks\n"),
		"wiki/meta/ledgers/task-ledger.json": `{"schema":"claude-atlas.task-ledger.v1","tasks":[]}`,
	})
	r, err := Run(kb, Options{AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range r.KindErrors {
		got = append(got, f.Path)
	}
	if strings.Join(got, ",") != ".claude-atlas.json,inbox,wiki/meta/ledgers/task-ledger.json,wiki/tasks" {
		t.Fatalf("kind errors %v", got)
	}
	if r.Summary.CategoryCounts["kind_errors"] != 4 || r.Version != 3 || !strings.Contains(r.Markdown(), "## Kind (4)") {
		t.Fatalf("summary %+v\n%s", r.Summary, r.Markdown())
	}
	project := fixture(t, map[string]string{
		".claude-atlas.json": `{"schema":"claude-atlas.vault.v2","id":"2","kind":"project","name":"p","mode":"generic","created":"2026-09-14","scope":"x"}`,
		"wiki/index.md":      mkpage("Index", "# Index\n"),
	})
	r, _ = Run(project, Options{AsOf: asOf})
	if len(r.KindErrors) != 1 || r.KindErrors[0].Path != ".claude-atlas.json" || !strings.Contains(r.KindErrors[0].Message, "scope") {
		t.Fatalf("project kind errors %+v", r.KindErrors)
	}
	plain := fixture(t, map[string]string{
		"wiki/index.md": mkpage("Index", "# Index\n"),
		"inbox/x.md":    "x",
	})
	r, _ = Run(plain, Options{AsOf: asOf})
	if len(r.KindErrors) != 0 {
		t.Fatalf("no identity file, no kind checks: %+v", r.KindErrors)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/lint/ -run TestKindErrors`
Expected: compile error `r.KindErrors undefined`.

- [ ] **Step 3: Add the check**

In `internal/lint/lint.go`:

- `const ReportVersion = 3`.
- In `Report`, after `TaskErrors`, add:

```go
	// KindErrors are files and identity fields that do not belong to the vault's kind.
	KindErrors []PathFinding `json:"kind_errors"`
```

- Add after `taskErrors`:

```go
// knowledgeHasNo says why each project-only folder is out of place in a knowledge base.
var knowledgeHasNo = map[string]string{
	vault.InboxDir: "sources enter through a project that mounts it",
	vault.IdeasDir: "ideas live in a project",
	vault.TasksDir: "tasks live in a project",
}

// kindErrors checks the vault against its kind. A knowledge base has no inbox, ideas, or
// tasks and carries no project fields; a project carries no knowledge base fields. A
// tree without a current identity file is not checked.
func kindErrors(root string, present map[string]bool) []PathFinding {
	cfg, ok := vault.ReadConfig(root)
	if !ok || cfg.Schema != vault.Schema {
		return nil
	}
	var out []PathFinding
	switch cfg.Kind {
	case vault.Knowledge:
		for _, dir := range []string{vault.InboxDir, vault.IdeasDir, vault.TasksDir} {
			if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(dir))); err == nil && info.IsDir() {
				out = append(out, PathFinding{Path: dir, Message: "a knowledge base has no " + dir + "/; " + knowledgeHasNo[dir]})
			}
		}
		if present[vault.TaskLedgerPath] {
			out = append(out, PathFinding{Path: vault.TaskLedgerPath, Message: "a knowledge base has no task ledger"})
		}
		if len(cfg.Tags)+len(cfg.Mounts)+len(cfg.Repos) > 0 {
			out = append(out, PathFinding{Path: vault.Marker, Message: "a knowledge base carries no tags, mounts, or repos; those are a project's fields"})
		}
	case vault.Project:
		if cfg.Scope != "" || cfg.Access != "" || len(cfg.Grants) > 0 {
			out = append(out, PathFinding{Path: vault.Marker, Message: "a project carries no scope, access, or grants; those are a knowledge base's fields"})
		}
	}
	return out
}
```

- In `Run`, after `report.LedgerErrors = ledgerErrors(...)`, add `report.KindErrors = kindErrors(root, present)`. In the `CategoryCounts` map, add `"kind_errors": len(report.KindErrors),`.
- In `sortFindings`, add `sort.SliceStable(r.KindErrors, func(i, j int) bool { return pathLess(r.KindErrors[i].Path, r.KindErrors[j].Path) })`.
- In `fillEmpty`, add:

```go
	if r.KindErrors == nil {
		r.KindErrors = []PathFinding{}
	}
```

(Also check `TaskErrors` and `LedgerErrors` are filled there; if `TaskErrors` is not, add it the same way.)

- In `Markdown`, after the "Ledger" section, add:

```go
	section("Kind", len(r.KindErrors))
	for _, f := range r.KindErrors {
		fmt.Fprintf(&b, "- `%s`: %s\n", f.Path, f.Message)
	}
```

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass. If a test elsewhere asserts `Version == 2` or an exact `IssuesFound` on a fixture with an identity file, the test output names it; `TestNewVaultHasNoFindings` must still pass for all four vaults, which it does because a fresh vault of either kind has only its own files.

- [ ] **Step 5: Commit**

```bash
git add internal/lint/lint.go internal/lint/lint_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "lint: report files and identity fields that do not belong to the vault's kind"
```

---

### Task 6: The tools follow the kind

**Files:**
- Modify: `internal/mcpserver/server.go`
- Modify: `internal/mcpserver/server_test.go`

**Interfaces:**
- Consumes: `vault.Kind`, `vault.Knowledge`, `vault.Project`, `Kind.Noun`.
- Produces: `Status.Kind string` (`kind`), `Status.ID string` (`id`); `inbox`, `capture`, `plant`, `tasks`, `repos`, and `route` with `type: task` return an error in a knowledge base; `plan` with `ingest` or `save` returns an error in a knowledge base; `status` computes tasks and the inbox only in a project.

- [ ] **Step 1: Write the failing test**

In `internal/mcpserver/server_test.go`, add after `newVault`:

```go
func newKnowledge(t *testing.T) *vault.Vault {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := filepath.Join(t.TempDir(), "kb")
	if _, err := vault.Init(root, vault.Options{Kind: vault.Knowledge}, now); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.Open(root)
	return v
}
```

In `TestToolsListAndStatus`, replace `st.Mode != "generic" || st.Pages != 5` with `st.Kind != "project" || st.ID == "" || st.Mode != "generic" || st.Pages != 5`.

Add at the end of the file:

```go
func TestKnowledgeBaseTools(t *testing.T) {
	v := newKnowledge(t)
	c := connect(t, v.Root)
	var st Status
	if msg := c.call("status", nil, &st); msg != "" || st.Kind != "knowledge" || st.ID == "" || st.Name != "kb" || st.Tasks.Open != 0 {
		t.Fatalf("%s %+v", msg, st)
	}
	refused := map[string]map[string]any{
		"inbox":   {},
		"capture": {"paths": []string{"x.md"}},
		"plant":   {"title": "T", "text": "t"},
		"tasks":   {},
		"repos":   {},
		"route":   {"type": "task", "title": "T"},
	}
	for name, args := range refused {
		if msg := c.call(name, args, nil); !strings.Contains(msg, "knowledge base") {
			t.Errorf("%s in a knowledge base: %q", name, msg)
		}
	}
	if msg := c.call("route", map[string]any{"type": "question", "title": "Q"}, nil); !strings.Contains(msg, "knowledge base") {
		t.Errorf("route question: %q", msg)
	}
	page := "---\ntype: concept\ntitle: A\nstatus: seed\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n\n# A\n\ntext\n"
	write := []map[string]any{{"path": "wiki/concepts/A.md", "mode": "create", "content": page}}
	for _, kind := range []string{"ingest", "save"} {
		if msg := c.call("plan", map[string]any{"kind": kind, "summary": "x", "writes": write}, nil); !strings.Contains(msg, "through a project") {
			t.Errorf("%s in a knowledge base: %q", kind, msg)
		}
	}
	var po PlanOut
	if msg := c.call("plan", map[string]any{"kind": "repair", "summary": "add A", "writes": write}, &po); msg != "" {
		t.Fatal(msg)
	}
	var res txn.Result
	if msg := c.call("apply", map[string]any{"plan_id": po.PlanID}, &res); msg != "" || res.Commit == "" {
		t.Fatalf("apply: %s %+v", msg, res)
	}
	var mo ModeOut
	if msg := c.call("mode", nil, &mo); msg != "" || strings.Join(mo.Types, ",") != "source,entity,concept" {
		t.Fatalf("mode: %s %+v", msg, mo)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/mcpserver/ -run 'TestKnowledgeBaseTools|TestToolsListAndStatus'`
Expected: compile error `st.Kind undefined`.

- [ ] **Step 3: Gate the tools**

In `internal/mcpserver/server.go`:

Add to `Status` after `Name`:

```go
	Kind          string         `json:"kind"`
	ID            string         `json:"id"`
```

In `status`, build the output with `Kind: string(v.Config.Kind), ID: v.Config.ID` added to the literal, and wrap the inbox and task blocks:

```go
	if v.Config.Kind == vault.Project {
		if files, err := capture.ListInbox(v, s.opts.Now()); err == nil {
			for _, f := range files {
				if !f.Captured {
					out.InboxWaiting++
				}
			}
		}
	}
```

and the same `if v.Config.Kind == vault.Project {` around the whole `if led, err := tasks.Current(v, s.opts.Now()); err == nil { ... }` block.

Add after `pluginVersion`:

```go
// requireProject refuses what only a project has.
func requireProject(v *vault.Vault, what string) error {
	if v.Config.Kind == vault.Project {
		return nil
	}
	return fmt.Errorf("%s is a knowledge base and has no %s; sources, tasks, and repositories belong to a project that mounts it", v.Name(), what)
}
```

Then, right after each `v, err := s.resolve(a.Vault)` block's error return:
- in `inbox`: `if err := requireProject(v, "inbox"); err != nil { return nil, InboxOut{}, err }`
- in `capture`: `if err := requireProject(v, "inbox"); err != nil { return nil, capture.Result{}, err }`
- in `plant`: `if err := requireProject(v, "tasks"); err != nil { return nil, PlantOut{}, err }`
- in `tasks`: `if err := requireProject(v, "tasks"); err != nil { return nil, TasksOut{}, err }`
- in `repos`: `if err := requireProject(v, "repositories"); err != nil { return nil, ReposOut{}, err }`
- in `route`, inside `if a.Type == "task" {` as its first line: `if err := requireProject(v, "tasks"); err != nil { return nil, vault.Route{}, err }`

In `plan`, after the `allowedKind` check, add:

```go
	if v.Config.Kind == vault.Knowledge && (kind == txn.Ingest || kind == txn.Save) {
		return nil, PlanOut{}, fmt.Errorf("knowledge enters through a project: %s is a knowledge base, so ingest and save run in a project session that mounts it", v.Name())
	}
```

Update the `status` tool description to start with "Describe the current vault: kind (knowledge base or project), path, mode, ...".

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver/server.go internal/mcpserver/server_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "mcpserver: status reports the kind; the project-only tools refuse in a knowledge base"
```

---

### Task 7: The session hook names the kind

**Files:**
- Modify: `internal/hooks/hooks.go`
- Modify: `internal/hooks/hooks_test.go`

**Interfaces:**
- Consumes: `vault.Kind`, `Kind.Noun`.
- Produces: `const KnowledgeSkills`; the first line reads `claude-atlas project: NAME (MODE mode) at PATH` or `claude-atlas knowledge base: NAME (MODE mode) at PATH`; a knowledge base session gets the maintenance line and no task lines.

- [ ] **Step 1: Write the failing tests**

In `internal/hooks/hooks_test.go`, in `TestSessionStart`, replace `"claude-atlas vault: v (generic mode)"` with `"claude-atlas project: v (generic mode)"` and, further down, `!strings.Contains(out.String(), "claude-atlas vault")` with `!strings.Contains(out.String(), "claude-atlas project")`. Search the file for any other `"claude-atlas vault"` and change it the same way.

Add:

```go
func TestSessionStartInAKnowledgeBase(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := filepath.Join(t.TempDir(), "kb")
	if _, err := vault.Init(root, vault.Options{Kind: vault.Knowledge, Name: "ai-ml"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+root+`"}`), &out, env(nil), true, time.Now()); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"claude-atlas knowledge base: ai-ml (generic mode)", "Knowledge enters through a project", "<vault-context>", KnowledgeSkills} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	for _, absent := range []string{"Open tasks", "task-plant", "inbox/tasks"} {
		if strings.Contains(text, absent) {
			t.Errorf("a knowledge base session mentions %q:\n%s", absent, text)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/hooks/`
Expected: FAIL: `missing "claude-atlas project: v (generic mode)"` and `undefined: KnowledgeSkills`.

- [ ] **Step 3: Print by kind**

In `internal/hooks/hooks.go`, after `Skills`, add:

```go
// KnowledgeSkills is the slash-menu line for a knowledge base, where knowledge enters
// through a project and the work here is upkeep.
const KnowledgeSkills = "/claude-atlas:wiki  wiki-query  wiki-lint  wiki-fold  wiki-mode  canvas  obsidian-markdown  obsidian-bases  think"
```

In `SessionStart`, replace the block from `var b strings.Builder` through `b.WriteString("Change wiki pages only through the atlas MCP tools (plan, then apply). Skills: " + Skills + "\n")` with:

```go
	var b strings.Builder
	noun := v.Config.Kind.Noun()
	if via != "" {
		fmt.Fprintf(&b, "claude-atlas: this folder is linked to the project %s, whose vault is the %s %s (%s mode) at %s. The atlas tools use that vault. Search the wiki (the wiki-query skill, or Grep under its wiki/) before answering from the code alone; keep a decision with the save skill.\n", via, noun, v.Name(), v.Config.Mode, v.Root)
	} else {
		fmt.Fprintf(&b, "claude-atlas %s: %s (%s mode) at %s\n", noun, v.Name(), v.Config.Mode, v.Root)
	}
	if policy != "" {
		b.WriteString(policy + "\n")
	}
	if v.Config.Kind == vault.Knowledge {
		b.WriteString("Knowledge enters through a project that mounts this knowledge base. Here: lint, repair, fold, stub. Change wiki pages only through the atlas MCP tools (plan, then apply). Skills: " + KnowledgeSkills + "\n")
	} else {
		b.WriteString("Change wiki pages only through the atlas MCP tools (plan, then apply). Skills: " + Skills + "\n")
	}
```

and replace `b.WriteString(taskLines(v, in.Cwd, via != "", now))` with:

```go
	if v.Config.Kind == vault.Project {
		b.WriteString(taskLines(v, in.Cwd, via != "", now))
	}
```

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass. `TestSessionStartListsTasksAndFindsAVaultThroughTheAtlas` still finds "linked to the project"; if it asserts the exact old sentence, update it to the new one with "the project" before the name.

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/hooks.go internal/hooks/hooks_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "hooks: the session start names the vault's kind and lists tasks only in a project"
```

---

### Task 8: `new-vault --kind` and `adopt --as`

**Files:**
- Modify: `internal/cli/cli.go` (`newVault`, `adopt`, `createVault`, `adoptPath`, `createOrAdopt`, `newVaultInteractive`)
- Modify: `internal/cli/cli_test.go`
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: `vault.Options`, `vault.ParseKind`, `vaults.Create(path, vault.Options, ...)`, `AdoptResult.Kind`, `AdoptResult.Removed`.
- Produces: `claude-atlas new-vault NAME --kind knowledge|project` (default `project`); `claude-atlas adopt PATH --as knowledge|project` (default: the vault's kind, else `project`). The interactive screens create projects until phase 4.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/cli_test.go` (add `"github.com/nathanaday/claude-atlas/internal/vault"` to the imports):

```go
func TestNewVaultKindAndAdoptAs(t *testing.T) {
	h, vaults := setup(t)
	if code := h.run("new-vault", "ai-ml", "--kind", "knowledge", "--category", "kb"); code != 0 {
		t.Fatalf("new-vault exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	v, err := vault.Open(filepath.Join(vaults, "kb", "ai-ml"))
	if err != nil || v.Config.Kind != vault.Knowledge {
		t.Fatalf("open %+v %v", v, err)
	}
	if _, err := os.Stat(v.Path("inbox")); err == nil {
		t.Fatal("a knowledge base has no inbox")
	}
	if code := h.run("new-vault", "x", "--kind", "bogus"); code != 2 || !strings.Contains(h.err.String(), "kind must be") {
		t.Fatalf("bad kind: exit %d %s", code, h.err.String())
	}
	old := filepath.Join(t.TempDir(), "old")
	os.MkdirAll(filepath.Join(old, "wiki"), 0o755)
	os.WriteFile(filepath.Join(old, "wiki", "index.md"), []byte("---\ntitle: I\n---\n# I\n"), 0o644)
	if code := h.run("adopt", old, "--as", "knowledge", "--category", "kb"); code != 0 {
		t.Fatalf("adopt exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if !strings.Contains(h.out.String(), "as knowledge base") {
		t.Fatalf("adopt output:\n%s", h.out.String())
	}
	if adopted, err := vault.Open(old); err != nil || adopted.Config.Kind != vault.Knowledge {
		t.Fatalf("adopted %+v %v", adopted, err)
	}
	if code := h.run("adopt", old, "--as", "project"); code != 1 || !strings.Contains(h.err.String(), "does not change") {
		t.Fatalf("kind is fixed: exit %d %s", code, h.err.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run TestNewVaultKindAndAdoptAs`
Expected: FAIL: `flag provided but not defined: -kind` (exit 2 on the first run).

- [ ] **Step 3: Add the flags**

In `internal/cli/cli.go`:

Add after `modeFlag`:

```go
func kindFlag(fs *flag.FlagSet, name, usage string) *string {
	return fs.String(name, "", usage)
}

// parseKind reads a --kind or --as value; empty means the caller's default.
func parseKind(s string, fallback vault.Kind) (vault.Kind, error) {
	if s == "" {
		return fallback, nil
	}
	return vault.ParseKind(s)
}
```

In `newVault`, after `mode := modeFlag(fs)`, add `kind := kindFlag(fs, "kind", "what the vault is for: project (tasks, questions, notes; mounts knowledge bases) or knowledge (a knowledge base of sources, entities, and concepts); default project")`. After the mode parse, add:

```go
	k, err := parseKind(*kind, vault.Project)
	if err != nil {
		return 2, err
	}
```

Change the two calls: `return e.adoptPath(*from, *opts, vault.Options{Kind: k, Mode: m, Name: opts.Name})` and `return e.createVault(positional[0], *opts, vault.Options{Kind: k, Mode: m, Name: opts.Name})`. Update the usage string to `usage: claude-atlas new-vault [NAME | --from PATH] [--kind project|knowledge] [--name N] [--category DIR] [--purpose TEXT] [--priority P] [--mode generic|lyt]`.

In `adopt`, after `mode := ...`, add `as := kindFlag(fs, "as", "adopt as a project or a knowledge base; a vault that already has a kind keeps it; default project")`. After the mode parse, add:

```go
	k, err := parseKind(*as, "")
	if err != nil {
		return 2, err
	}
```

Change the interactive call to `e.adoptPath(choice.Path, vaults.RegisterOptions{...same...}, vault.Options{Kind: vault.Project, Mode: vault.Mode(choice.Mode), Name: choice.Name})` and the positional call to `e.adoptPath(positional[0], *opts, vault.Options{Kind: k, Mode: m, Name: opts.Name})`. Update the usage string to include `[--as project|knowledge]`.

Change `adoptPath` to `func (e *env) adoptPath(path string, opts vaults.RegisterOptions, vopts vault.Options) (int, error)`, call `vault.Adopt(abs, vopts, time.Now())`, and replace the `switch` that prints the adopt step with:

```go
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
	if len(res.Removed) > 0 {
		c.Step(console.OK, "removed", strings.Join(res.Removed, ", ")+" (a knowledge base has none)")
	}
```

Change `createVault` to `func (e *env) createVault(arg string, opts vaults.RegisterOptions, vopts vault.Options) (int, error)` and call `vaults.Create(path, vopts, e.console, true)`. In `createOrAdopt`, pass `vault.Options{Mode: mode, Name: choice.Name}` to `Adopt` and `vault.Options{Kind: vault.Project, Mode: mode, Name: choice.Name}` to `Create` (Task 1 did this; confirm). In `newVaultInteractive`, the same `Create` call.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass. If `TestVaultCommands` asserts the old "adopted" wording ("already a claude-atlas vault"), update it to "already a claude-atlas project".

- [ ] **Step 5: Record the change in CLAUDE.md**

In `CLAUDE.md`, under "## Constraints", add this bullet after the one that begins "Every write path goes through":

```
- A vault has a kind, `knowledge` or `project` (`vault.Kind`, in the v2
  identity file with an `id` and a `name`). A knowledge base has no inbox,
  ideas, tasks, questions, or sessions; `txn`, the tools, lint, and the hook
  refuse or skip them there. `new-vault --kind` and `adopt --as` choose the
  kind; it does not change afterwards. Templates live under
  `internal/vault/templates/{common,knowledge,project}/`.
```

In the "Sources of truth" table, change the v2 row's parenthetical from "(designed, not built)" to "(phase 1 built: kinds)".

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go CLAUDE.md
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "cli: new-vault --kind and adopt --as choose a vault's kind"
```

---

## Self-review

**Spec coverage (phase 1 in `v2-design.md`):** the v2 identity file with id, kind, and name (Task 1); templates by kind (Task 2); `adopt --as` writing the identity file and dropping task scaffolding from a knowledge base (Task 3); operation kinds by vault kind and `RoutableTypes` taking the kind (Task 4); lint's "a knowledge base with `wiki/tasks/` or `inbox/` is a finding; a project with `grants` or `scope` is a finding" (Task 5); tools gated by kind and `plan` refusing `ingest` and `save` in a knowledge base with no project session (Task 6); the session hook's first line and the knowledge base's maintenance line (Task 7, the mount lines wait for phase 3); `lint.TestNewVaultHasNoFindings` for both kinds (Task 2). `new-knowledge` and `new-project` as commands, and the registry, are phase 2; phase 1 reaches the kinds through `new-vault --kind` and `adopt --as`.

**Placeholders:** none. Every code step shows the code.

**Type consistency:** `vault.Options{Kind, Mode, Name}` is used the same way in Tasks 1, 2, 3, 4, 6, 7, 8. `AdoptResult.Kind` and `.Removed` are added in Tasks 1 and 3 and read in Task 8. `RoutableTypes(kind, mode)` is changed in Task 4 and its two server call sites in the same task. `requireProject` is defined and used in Task 6 only. `KnowledgeSkills` is defined and tested in Task 7.
