# claude-atlas

A Go binary and a Claude Code plugin. The binary creates and maintains
Obsidian knowledge vaults and serves the MCP tools Claude uses inside them; the
plugin carries the skills and hooks. The atlas side lists every vault from a
scan of the vaults directory and shows them in a terminal view.

Read `README.md` first. This file holds what the code and README do not say.

## Sources of truth

| Thing | Location |
|---|---|
| v2: knowledge bases, projects, mounts, access (phases 1–5 built: kinds, the registry, mounts, the view's mount keys, projects inside repositories) | `docs/v2-design.md` |
| Core design and the reasons behind it | `docs/core-design.md` |
| The atlas side before v2 (superseded by `v2-design.md`) | `docs/atlas-design.md` |
| Tasks: pages, ledger, skills, repos reaching the vault | `docs/tasks-design.md` |
| Original brainstorm (not a contract) | `docs/spec.md` |
| The skills' contracts | `skills/<name>/SKILL.md` and `skills/wiki/references/` |

## Why the project exists

claude-obsidian established a workflow the author wants to keep: an inbox,
immutable captured sources, one reviewed operation per change, pages that cite
their sources. Its Python core and its approval ritual (copy a hash, repeat a
timestamp) got in the way, and nothing showed many vaults at once. Atlas keeps
the workflow, replaces the core with Go and MCP, uses git in the vault as the
safety net, and adds the cross-vault view.

## Three rules for a vault

1. One operation, one commit. `plan` validates, the user sees the preview,
   `apply` commits. There is no other write path; a PreToolUse hook refuses
   Write and Edit under `wiki/`.
2. The vault is the user's. Apply commits hand edits as `manual` operations
   first, so a rollback never touches what the user typed in Obsidian.
3. Code owns what code can derive. The core writes `wiki/log.md`, the source
   ledger, the task ledger, and `wiki/tasks/tasks.md`; the model never targets
   them. A task's status is the truth and its folder follows it: finished
   tasks sit in `wiki/tasks/archive/`, and the core refuses a page whose
   folder disagrees.

## Three rules for the atlas

1. A knowledge base never learns who mounts it. The atlas computes that list
   from the projects' identity files.
2. A project never links another project. Projects share knowledge bases.
3. Ids travel; paths stay. The identity file holds no path; the atlas config
   holds the paths the atlas cannot compute, and nothing else.
   `~/.claude-atlas/state/registry.json` is derived, and `refresh` rebuilds it
   in full.

## Two layers, one backend

`view` is the whole atlas as one screen; every command is one thing from it.
The rule: an action lives once, as a function in `internal/vaults`,
`internal/refresh`, `internal/vault`, or `internal/txn`. The CLI exposes it as
one subcommand. The TUI reaches it through `tui.Hooks`, which `cli.hooks`
builds in one place. The TUI is a subset of the CLI, never the reverse: a new
key gets a command in the same change, and `docs/usage.md` carries the table
that maps them.

## Layout

```
.claude-plugin/         plugin.json and marketplace.json; this repo is its own marketplace
.mcp.json               the atlas MCP server: scripts/atlas mcp
scripts/atlas           sh wrapper that finds the installed binary
hooks/hooks.json        SessionStart context, PreToolUse guard, Stop warning
skills/                 one directory per skill; skills/wiki/references/ is shared
agents/                 wiki-ingest worker, wiki-lint interpreter
cmd/claude-atlas/       main
internal/cli/           argument parsing and one method per subcommand
internal/wizard/        the setup flow
internal/vault/         identity file, layout, templates, Init, Adopt, mode routing, page skeletons
internal/gitx/          the git commands the core needs
internal/txn/           plans, preview, apply, recovery, undo, history, planting a task
internal/tasks/         task pages, the derived task ledger and index; never decides to write
internal/discover/      the project a folder belongs to, through the projects' repositories
internal/capture/       inbox listing and capture into .raw/captured/
internal/ledger/        the source ledger
internal/lint/          the health check (ported from claude-obsidian's engine)
internal/mcpserver/     the tools, thin over the packages above
internal/hooks/         session-start (a vault or a project's repository, open tasks, hot cache), guard, stop
internal/claudecode/    Claude Code's plugin registry, `claude plugin`, launching claude in a vault
internal/registry/      the scan for identity files, the resolved entries, the registry state file
internal/refresh/       derive one vault's state, rewrite the registry, list a vault's signals
internal/vaults/        create, adopt, register, edit identity files, link and create repositories
internal/links/         git init, create and clone, change policies, the facts git reports
internal/tui/           Bubble Tea screens: the view, the vault editor, the repositories screen, tasks, ingest, the add and adopt screens
internal/obsidian/      Obsidian's vault registry, obsidian:// URIs, restart
internal/home/          ~/.claude-atlas and config.json
internal/console/       prompts and step lines
```

`~/.claude-atlas/` holds `config.json` and `state/registry.json`. The vaults
are the user's and live under the vaults directory (default
`~/Documents/Vaults`). A new vault goes to `<vaults dir>/knowledge/<name>` or
`<vaults dir>/projects/<name>` unless the user gives a path (`vaults.PathFor`,
`vaults.ResolvePath`). A vault outside the vaults directory is listed in the
config, because the scan cannot find it there (`vaults.Register`); a vault
inside it needs no entry and cannot be forgotten (`vaults.Unregister`). A vault
never goes inside another vault (`vaults.CheckNewPath`).

## Constraints

- Dependencies: `gopkg.in/yaml.v3`, the official MCP `go-sdk` pinned to v1.4.0
  (later versions need Go 1.25; the machine runs 1.24), and Bubble Tea, Bubbles,
  and Lip Gloss. Nothing else. Do not pull `golang.org/x/*` at `@latest`.
- `git` is a runtime requirement. `python3` is not. macOS and Linux only.
- The binary and the plugin are installed separately. `scripts/atlas` finds the
  binary on PATH, in `~/go/bin`, in the Homebrew prefixes, or at
  `$CLAUDE_ATLAS_BIN`. `plugin.json` and `marketplace.json` carry the version
  the binary should match; `status` and `doctor` warn on a mismatch.
- Every write path goes through `txn.Prepare` and `txn.Apply`. `vault.Init`,
  `vault.InitIn`, `vault.Adopt`, `vault.Upgrade`, and `vault.UpdateConfig` are
  the only code that writes vault files directly, and only before or outside
  an operation. The template includes the vault's CSS snippet and an
  appearance file that enables it; upgrade merges the snippet into an
  existing appearance file. The symlinks under a project's `kb/`, which
  `mount` and `refresh` create, are local state git ignores.
- A vault has a kind, `knowledge` or `project` (`vault.Kind`, in the v2
  identity file with an `id` and a `name`). A knowledge base has no inbox,
  ideas, tasks, questions, or sessions; `txn`, the tools, lint, and the hook
  refuse or skip them there. `new-project`, `new-knowledge`, and `adopt --as`
  choose the kind; it does not change afterwards. Templates live under
  `internal/vault/templates/{common,knowledge,project}/`.
- A vault's own facts change only through `vault.UpdateConfig`, which
  validates the whole identity file against the kind, refuses a new kind or a
  new id, and commits the file as a `setup` operation. `vaults.EditIdentity`
  and the repository functions are its only callers.
- A repository is where a project's deliverables go, always a git repository;
  memory stays in the vault, and ingest sources are not repositories. The
  project's identity file records the name, the remote, and the change policy
  (`changes: pr` or `commit`); a repository outside `<project>/repos/<name>`
  has its path in the atlas config, and a path inside the vault but outside
  `repos/` is refused. Sessions learn the policy from the hook and the `repos`
  tool.
- A project may live inside a repository at `REPO/atlas/` (`new-project NAME
  --in REPO`). `vault.HostRepo` detects the layout from the filesystem;
  nothing stores it. Every git command the engine runs there takes the
  pathspec `atlas/` (`gitx.Repo.Prefix`), so an operation never commits code
  outside the vault. The host repository is the project's first repository in
  the registry (`Entry.Host`), without an entry in the identity file; `link`,
  `new-repo`, and `unlink` refuse its name, and `edit-repo` sets its change
  policy.
- A kind bounds a plan's writes (`txn.allowed`). Reserved everywhere:
  `wiki/log.md`, both ledgers, `wiki/tasks/tasks.md` and its old path
  `wiki/tasks/index.md`, `.git`, `.vault-meta`, `.obsidian`, `.raw` except
  through capture, `inbox` except deletes in an ingest, `inbox/tasks` except
  deletes in a task operation, `kb/`, `repos/`. Only a `task` or `repair` plan
  may touch a page under `wiki/tasks/`.
- A new vault lints clean, and `lint.TestNewVaultHasNoFindings` holds the
  template to that. A folder's index page takes the folder's name
  (`wiki/tasks/tasks.md`, `wiki/canvases/canvases.md`), so no page shares the
  basename of `wiki/index.md`. When a template path changes, `vault.Upgrade`
  and `vault.Adopt` move the old file (`vault.legacyPaths`).
- The server and the hooks resolve the vault in this order: an explicit
  `vault`, `CLAUDE_ATLAS_VAULT`, the nearest identity file, then the registry:
  a folder inside exactly one project's repository belongs to that project
  (`discover.Vault`).
- A project session may write a mounted knowledge base when its mount is
  effectively `write` (`registry.Effective` of the request and the grant); a
  knowledge base session runs maintenance kinds only; a source captured into
  a knowledge base records `via`, the project it came through.
- The scan is the truth. `registry.Scan` walks the vaults directory at most
  five levels deep for identity files, skips dot-directories and
  `node_modules`, never descends into a vault it has found, and adds the paths
  in `config.vaults`. A vault the atlas knows but cannot read (a v1 identity
  file, one that is not JSON, a registered folder that is gone) becomes an
  entry with a `Path`, an `Error`, and a `Reason` code; `list` and `doctor`
  decide on the code, `remove` forgets such an entry, and the view files it
  under `problems`. Every command that acts on a vault scans afresh;
  `registry.json` is for display only.
- In a project, lint resolves links through the symlinks under `kb/`;
  findings are about the project's own pages.
- Lint is read-only; refresh writes nothing a vault's git tracks (it recreates
  the ignored `kb/` symlinks).
- TUI models keep all logic in `Update`; tests drive them with `tea.KeyMsg`.
- Tests never touch a real `~/.claude-atlas`, never install a plugin, and skip
  when `git` is missing. MCP tools are tested in-process over the SDK's
  in-memory transport.
- Prose follows the user's global writing guide.

## Claude Code plugin facts, verified on 2.1.270

- A plugin's `.mcp.json` may run `${CLAUDE_PLUGIN_ROOT}/...`. The server starts
  in the project directory with `CLAUDE_PROJECT_DIR` and `CLAUDE_PLUGIN_ROOT`
  set, one process per session. Tools are named
  `mcp__plugin_<plugin>_<server>__<tool>`.
- A PreToolUse hook that prints `hookSpecificOutput.permissionDecision: deny`
  blocks the tool; the model sees `permissionDecisionReason`.
- SessionStart hook stdout becomes context. Stop hooks report through
  `systemMessage`.
- Plugin agents may list MCP tools in `tools:`.
- The repository's `main` branch is a marketplace named
  `nathanaday-claude-atlas`; a local checkout works as a marketplace source
  for development (`claude-atlas setup --plugin-source /path/to/checkout`).
- A session started inside a checkout of this repository reports that a
  project MCP server `${CLAUDE_PLUGIN_ROOT}/scripts/atlas` failed to start:
  Claude Code reads the checkout's own `.mcp.json` as a project server, and
  that variable is set only for plugins. The installed plugin's copy works;
  the message is noise.

## Obsidian facts

- `obsidian://open?path=` only opens vaults Obsidian already knows. Obsidian
  reads its registry (`obsidian.json` under its config dir) once at launch,
  prunes entries whose path is gone, and rewrites the file whenever its state
  changes. `open-vault` quits Obsidian first (macOS, AppleScript), adds one
  entry, relaunches, then opens the URI. Verified on Obsidian 1.8.7 / 1.13.7.

## Build and test

```
make build      # build/claude-atlas
make install    # go install into $(go env GOPATH)/bin
make test
```

The installed plugin is a git clone of this repository at a commit, and
`claude plugin update` fetches a new one only when the version in
`.claude-plugin/plugin.json` and `marketplace.json` went up. So a skill change
reaches Claude Code after: bump both versions, commit, then

```
claude plugin marketplace update nathanaday-claude-atlas
claude plugin update claude-atlas@nathanaday-claude-atlas
make install
```

`make install` stamps the binary with the same version, so `doctor` and
`status` can tell when the two drift. Uncommitted skill edits can be tried
with `claude --plugin-dir .` from inside a vault. End-to-end by hand:
`claude -p "..."` inside a vault with
`--allowedTools "mcp__plugin_claude-atlas_atlas__*,Read,Grep,Glob,Skill"`.

## Open questions

- A `search` tool with BM25 ranking, once Grep proves insufficient.
- A Claude Code skill for the bird's-eye conversation across every vault.
- Distribution: a Homebrew tap and release binaries; then the wrapper can
  download a checksummed binary into `${CLAUDE_PLUGIN_DATA}`.
