# The atlas core: a self-contained plugin

Status: implemented, 2026-09-12. v2 (`v2-design.md`) supersedes what this
document says about the atlas and about a single vault kind; the engine
rules below stand. Decisions taken after the proposal: no claim ledger, hand edits are auto-committed, the binary is installed first and the plugin finds it, Go stays at 1.24 with the SDK pinned to v1.4.0.

claude-atlas stops wrapping the claude-obsidian plugin and ships its own
core. The core is Go, exposed to Claude Code as an MCP server, and packaged
with the skills, agents, and hooks in one plugin. The workflow stays the one
claude-obsidian established: sources enter through an inbox, every change to
a vault is one reviewed operation, and the vault stays a plain directory of
Markdown that the user owns.

This document records what we keep, what we change, and why. The skills and
vault conventions derive from claude-obsidian (MIT, AgriciDaniel); the README
will say so.

## What the pivot buys

| Today (wrapper) | After (own core) |
|---|---|
| Python 3.11 and the claude-obsidian plugin must be installed and compatible | One Go binary and one plugin |
| Every skill shells out, copies an `approval_sha256`, and repeats `--generated-at` | The MCP server holds the plan; apply takes a plan id |
| Safety comes from a 4,800-line journal and backup engine | Safety comes from git: one commit per operation |
| Skills warn the model not to use Write/Edit on the vault | A hook refuses Write/Edit under `wiki/` |
| Atlas parses another product's output formats | Atlas writes the log it later reads |
| Four filing modes, addresses, capture queues, release gates | Two modes, no addresses, no queues |

## Evidence from the spike

A throwaway plugin with a Go MCP server (official `go-sdk` v1.4.0) and a
hook proved the path on this machine, Claude Code 2.1.270, Go 1.24.2:

- `.mcp.json` in the plugin root with `command: ${CLAUDE_PLUGIN_ROOT}/bin/...`
  connects at session start. Tools appear as
  `mcp__plugin_<plugin>_<server>__<tool>`.
- The server process starts in the project directory, receives
  `CLAUDE_PROJECT_DIR` and `CLAUDE_PLUGIN_ROOT`, and answers `roots/list`.
- One server process serves the whole session, so a plan created by one call
  is still there for the next call.
- A `PreToolUse` hook on `Write|Edit|MultiEdit` that prints
  `permissionDecision: deny` blocks the write. The model sees the reason and
  writes elsewhere.
- `go-sdk` v1.4.0 needs Go 1.24; v1.5 and later need Go 1.25. We pin v1.4.0
  until the machine's Go is upgraded.

## Three rules, restated

1. **One operation, one commit.** Every change to a vault is a plan the model
   builds, a preview the user sees, and one git commit the core makes. There
   is no other write path.
2. **The vault is the user's.** Hand edits in Obsidian are normal. The core
   commits them under their own message before it applies anything, so a
   rollback never touches what the user typed.
3. **Code owns what code can derive.** The log, the source ledger, the
   history, and the lint report come from the core. The model writes pages.

The three atlas-level rules (never write into a vault from the tree, a vault
never learns the atlas exists, never store a computable fact) stay as they
are.

## The vault

Layout, identical to claude-obsidian where that costs nothing, so an existing
claude-obsidian vault adopts in one step.

```text
vault/
├── .claude-atlas.json          identity and mode (tracked)
├── .git/                       one commit per operation
├── .gitignore                  .vault-meta/, .obsidian/workspace*.json, .trash/
├── .obsidian/                  app settings (tracked)
├── inbox/                      staging: the user drops files here
├── .raw/captured/<sha256>.ext  immutable copies of ingested sources
└── wiki/
    ├── index.md  log.md  hot.md  overview.md
    ├── sources/ entities/ concepts/ questions/ sessions/     (generic)
    ├── notes/ mocs/                                          (lyt)
    ├── canvases/
    └── meta/ledgers/source-ledger.json
```

`.claude-atlas.json`:

```json
{ "schema": "claude-atlas.vault.v1", "mode": "generic", "created": "2026-09-12" }
```

Runtime state under `.vault-meta/` is ignored by git and safe to delete: the
lock file and an `inflight.json` that exists only while an apply runs.

### Page conventions

Kept from claude-obsidian so the user's current vault lints clean:

- Frontmatter is flat YAML with `title`, `type`, `status`, `created`,
  `updated`, `tags`; `aliases` optional.
- Types: `source`, `entity`, `concept`, `question`, `session`, `overview`,
  `meta`, `fold`, and for LYT `note`, `moc`.
- Statuses: `seed`, `developing`, `evergreen`, `answered`, `provisional`,
  `contested`, `deprecated`, `archived`, plus whatever the vault already uses.
- Files are named by title, sanitized for the filesystem, not slugified.
  Obsidian links read as `[[Attention Is All You Need]]`.
- Only `wiki/index.md` is named `index`. A folder's index page takes the
  folder's name: `wiki/tasks/tasks.md`, `wiki/canvases/canvases.md`. Two
  pages with one basename give a bare link two targets, and Obsidian picks one
  while lint resolves `[[index]]` under `wiki/` first and reports nothing.

### Modes

| Mode | New pages go to | Navigation |
|---|---|---|
| `generic` (default) | a folder per type under `wiki/` | `wiki/index.md` |
| `lyt` | `wiki/notes/`, one idea per note | `wiki/mocs/*.md`, `wiki/index.md` is the home MOC |

Mode is a field in `.claude-atlas.json`. Changing it is an operation of kind
`config` and changes routing for future pages only. PARA and Zettelkasten are
not carried over.

## The core

Go packages, all under `internal/`, all tested against temporary vaults with
real git.

| Package | Owns |
|---|---|
| `vault` | identity file, layout, `Init`, `Adopt`, templates, mode routing, page skeletons |
| `gitx` | the few git commands the core needs, with stable parsing |
| `txn` | plans, preview, apply, recovery, undo |
| `capture` | inbox listing, copy into `.raw/captured/`, source ledger records |
| `ledger` | `source-ledger.json` read, validate, merge |
| `lint` | link parser, resolver, findings, Markdown report |
| `mcpserver` | the tools, thin over the packages above |
| `hooks` | session-start context, the write guard, stop status |

`internal/product` (the Python shim) is deleted. `refresh` calls `lint` and
reads the log in-process.

### A plan

```json
{
  "kind": "ingest",
  "summary": "Ingest the DINOv2 paper: source page, two concept pages, index",
  "writes": [
    { "path": "wiki/sources/DINOv2.md", "mode": "create", "content": "..." },
    { "path": "wiki/index.md", "mode": "replace", "content": "...", "base_sha256": "ab12…" },
    { "path": "wiki/hot.md", "mode": "replace", "content": "..." }
  ],
  "sources": [ { "id": "src-…", "ingested": true, "pages": ["wiki/sources/DINOv2.md"], "authority": "primary" } ]
}
```

- `mode` is `create`, `replace`, or `delete`.
- `base_sha256` is the hash the model last saw. When omitted for a replace,
  the core records the current hash at plan time. Either way, apply refuses
  if the file changed after that.
- `sources` updates ledger records. The model never edits the ledger JSON.
- The core writes `wiki/log.md`. A plan that names it is rejected.

Kinds and their write scope, enforced at plan time:

| Kind | May write |
|---|---|
| `ingest` | `wiki/**`; `.raw/captured/*` is create-only through `capture` |
| `save`, `markdown`, `repair`, `fold` | `wiki/**` |
| `canvas` | `wiki/canvases/**/*.canvas`, `wiki/canvases/canvases.md` |
| `base` | `wiki/**/*.base` |
| `config` | `.claude-atlas.json` |
| `setup` | the template paths, from `Init` and `Adopt` only |

Reserved everywhere: `wiki/log.md`, `.git`, `.vault-meta`, `.obsidian`,
`inbox`, `kb/`, `repos/`. Limits: 256 writes, 16 MiB per write, one plan per
vault at a time (a new plan replaces the old one).

Plan time also validates content: frontmatter parses and carries the required
keys for a wiki page, JSON files parse, `.base` files parse as YAML, and links
in new content resolve. Unresolved links come back as warnings in the preview,
not as errors, because a plan can create the target in the same operation.

### Apply

1. Take the vault lock (`flock` on `.vault-meta/lock`).
2. If `inflight.json` exists, recover first (step 7), then continue.
3. If `git status` is dirty, commit everything as `manual: N files changed by
   hand`. The tree is now clean, so every later change is ours.
4. Check every `base_sha256` against the file. A mismatch aborts with
   `conflict` and names the path. The model re-reads and plans again.
5. Write `inflight.json` listing the operation id and the target paths.
6. Write each file (temp file, rename), delete the deletes, prepend the log
   entry, merge the ledger, then `git add` those paths and commit as
   `<kind>: <summary>` with an `atlas-operation: <id>` trailer.
7. On any failure: for each target path, restore it from `HEAD` if it existed,
   otherwise remove it. Delete `inflight.json`. Return the error.
8. Delete `inflight.json`, release the lock, return the operation id, the
   commit, and the changed paths.

Operation ids are `<kind>-<yyyymmdd>-<hhmmss>-<4 hex>`, minted by the core.

### Undo and history

`undo` reverts one operation commit with `git revert --no-edit`. It commits
manual edits first, like apply. A revert that conflicts is aborted and
reported; the user resolves it in git or asks for a narrower repair.

`history` reads `git log` and returns operations with their kind, summary,
date, and paths. `wiki/log.md` is the human-readable copy of the same facts.

### Lint

Ported from claude-obsidian's engine, read-only, deterministic:
dead and ambiguous links, duplicate basenames, orphans, missing frontmatter,
empty sections, stale index entries, read errors. One addition: pages that no
index or MOC links to, since "every new page joins the index" is a rule the
skills follow and lint can check.

## The plugin

```text
claude-atlas/                      this repository, also the marketplace
├── .claude-plugin/plugin.json      name claude-atlas
├── .claude-plugin/marketplace.json this repo at ./
├── .mcp.json                       { "atlas": { "command": "${CLAUDE_PLUGIN_ROOT}/bin/atlas", "args": ["mcp"] } }
├── bin/atlas                       shell wrapper that finds the installed binary
├── hooks/hooks.json
├── skills/<name>/SKILL.md
├── agents/wiki-ingest.md, wiki-lint.md
├── cmd/, internal/, docs/, Makefile, go.mod
```

The plugin cache is code; it never holds a vault or a binary we built. The
wrapper looks for the binary in `$CLAUDE_ATLAS_BIN`, on `PATH`, in
`~/go/bin`, in the Homebrew prefixes, then in `${CLAUDE_PLUGIN_DATA}/bin`. If
none exists it prints the install command and exits. The binary reads the
plugin's `plugin.json` at start and warns when the two versions differ.

Install order stays as it is now: install the binary, run `claude-atlas
setup`, which adds this repository as a marketplace and installs the plugin.
A later release can teach the wrapper to download a checksummed binary into
`${CLAUDE_PLUGIN_DATA}`; that is not part of this design.

### Tools

Server name `atlas`. Every tool takes an optional `vault`; when absent the
server resolves `CLAUDE_ATLAS_VAULT`, then the nearest `.claude-atlas.json`
above `CLAUDE_PROJECT_DIR`, and fails closed otherwise.

| Tool | Reads or writes | Returns |
|---|---|---|
| `status` | reads | vault path, mode, page count, git state, last operation, pending recovery, plugin and binary versions |
| `inbox` | reads | files in `inbox/` with size, kind, and whether each is captured |
| `capture` | writes (its own commit) | for each file: source id, stored path, sha256, whether it was already captured |
| `route` | reads | the path for a new page of a type, the existing page if one matches the title or an alias, and a skeleton with the vault's frontmatter |
| `plan` | reads, holds | plan id, preview (create, replace, delete, diff sizes), warnings |
| `apply` | writes | operation id, commit, changed paths |
| `undo` | writes | the revert commit |
| `history` | reads | recent operations |
| `lint` | reads | the report |

The model reads pages with its own Read, Grep, and Glob tools. A `search`
tool with BM25 ranking is a later addition, not part of this design.

### Hooks

| Event | Command | Effect |
|---|---|---|
| `SessionStart` | `atlas hook session-start` | when the project is a vault and `claude_code.session_context` is on: vault name, mode, `wiki/hot.md` (bounded), and any pending recovery |
| `PreToolUse` on `Write|Edit|MultiEdit|NotebookEdit` | `atlas hook guard` | deny when the path is under a vault's `wiki/`, `.raw/`, or is its `.claude-atlas.json` |
| `Stop` | `atlas hook stop` | warn when `inflight.json` exists |

The guard finds the vault by walking up from the target path to a
`.claude-atlas.json`. It needs no registry. Shell writes are not guarded;
rule 2 (commit manual edits first) keeps them recoverable.

### Skills

Ported from claude-obsidian with the invocation rewritten from
`python3 "$CORE" ...` to tool calls, and the transaction prose replaced by
the plan and apply contract. Names are unchanged so the slash menu reads as
before, under `/claude-atlas:`.

| Skill | Change beyond the rewrite |
|---|---|
| `wiki` | routes; setup points at `claude-atlas new-project` or `new-knowledge` and `claude-atlas adopt` |
| `wiki-ingest` | uses `inbox`, `capture`, `route`, `plan`, `apply`; ledger via the `sources` field |
| `wiki-query` | read-only; retrieval is Grep and Glob until `search` exists |
| `wiki-lint` | calls `lint`; repairs are a `repair` plan |
| `wiki-mode` | `generic` and `lyt` only; set through a `config` plan |
| `save` | unchanged in spirit |
| `wiki-fold` | unchanged in spirit |
| `canvas`, `obsidian-bases`, `obsidian-markdown` | syntax references kept; writes through `plan` |
| `think` | unchanged |

Dropped: `autoresearch`, `defuddle`, `wiki-retrieve`, `wiki-cli`, and the
`verifier` agent. Their references (`mcp-setup.md`, `rest-api.md`,
`install-modes.md`, `git-setup.md`) go with them.

Agents: `wiki-ingest` (read-only worker, returns draft packets) and
`wiki-lint` keep their contracts, with tools limited to Read, Grep, Glob and
the read-only atlas tools.

## The CLI

Kept: everything the atlas tree needs (`setup`, `new-project`, `new-knowledge`,
`view`, `open-vault`, `open-claude`, `link`, `unlink`, `links`, `list`,
`refresh`, `info`, `doctor`, `version`).

Changed:

- `new-project` and `new-knowledge` create the vault in-process and make the
  first commit.
- `adopt PATH` accepts a claude-obsidian vault too: it adds
  `.claude-atlas.json`, converts `.claude-obsidian.json` if present, runs
  `git init` when needed, and commits a baseline.
- `setup` installs this plugin instead of claude-obsidian.
- `doctor` checks the binary, the plugin, git, and every vault.
- `open-claude` sets `CLAUDE_ATLAS_VAULT`.

New, all thin over the packages the MCP server uses: `mcp`, `hook`, `lint`,
`history`, `undo`, `recover`, `mode`, `apply PLAN.json` for scripts and
tests.

## Dependencies

`go.mod` gains `github.com/modelcontextprotocol/go-sdk` v1.4.0. Nothing
else. `git` becomes a runtime requirement; `python3` is no longer one.
macOS and Linux are supported; Windows is not.

## Phases

1. Core packages with tests: `vault`, `gitx`, `txn`, `capture`, `ledger`,
   `lint`. `refresh` moves onto them and `internal/product` is deleted.
2. MCP server, hooks, plugin manifests, wrapper script. In-process tool
   tests over the SDK's in-memory transport.
3. Skills and agents ported. `setup`, `new-project`, `new-knowledge`, `adopt`,
   `doctor`, `open-claude` updated. README, CLAUDE.md.
4. Live run on this machine: a new vault, one ingest from the inbox, a query,
   a lint, an undo. Adopt `MyKnowledgeVault`.
5. Later: `search`, Homebrew tap, release binaries, wrapper download.

## Decisions

| Question | Decision |
|---|---|
| Rollback mechanism | git, one commit per operation; manual edits committed first |
| Where the plan lives between preview and apply | in the MCP server's memory, keyed by plan id |
| Who writes `wiki/log.md` | the core, from the plan's summary and paths |
| Who writes the source ledger | the core, from `capture` and the plan's `sources` field |
| Claim ledger | not in this design (see open questions) |
| Page addresses (`c-000001`) | dropped |
| Capture queue, URL and OCR adapters | dropped; the inbox is the only intake |
| `.raw/` in git | tracked; `capture` warns above 50 MiB per file |
| Modes | `generic`, `lyt` |
| Filing names | titles, not slugs |
| MCP library | official `go-sdk`, pinned to v1.4.0 for Go 1.24 |
| Reading pages | the model's own Read, Grep, Glob |

## Open questions

1. **Claim ledger.** claude-obsidian keeps a second ledger of falsifiable
   claims with support, contradiction, risk, and independence rules. The
   user's current vault has zero records in it. Proposal: leave it out, keep
   the source ledger, and add claims later if a real need appears.
2. **Auto-committing hand edits.** Apply commits whatever is dirty in the
   vault before it runs. Git history in the vault will then contain the
   user's Obsidian edits as `manual:` commits. That is the price of exact
   rollback. Acceptable?
3. **Binary distribution.** Install the binary first (brew or `go install`)
   and let the plugin find it, or have the plugin download a release on first
   session. Proposal: install first, download later once releases exist.
4. **Go toolchain.** Pin `go-sdk` v1.4.0 and stay on Go 1.24, or upgrade the
   machine's Go and track the current SDK. Either works today.
