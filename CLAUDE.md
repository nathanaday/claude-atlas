# claude-atlas

A Go binary and a Claude Code plugin. The binary creates and maintains
Obsidian knowledge bases, makes a folder of work a project, and serves the MCP
tools Claude uses in both; the plugin carries the skills and hooks. The atlas
side lists every knowledge base and project and shows them in a terminal view.

Read `README.md` first. This file holds what the code and README do not say.

## Sources of truth

| Thing | Location |
|---|---|
| Threads: a project's state as stub, spec, plan, receipt documents (built; 3.0.0) | `docs/threads-design.md` |
| v3: a project is an `atlas/<name>/` folder in the work, one knowledge base, phases (built; 2.0.0); its task sections are superseded by `threads-design.md` | `docs/v3-design.md` |
| v2: knowledge bases, projects, mounts, access (superseded by `v3-design.md`) | `docs/v2-design.md` |
| Core design and the reasons behind it | `docs/core-design.md` |
| The atlas side before v2 (superseded) | `docs/atlas-design.md` |
| Tasks (superseded by `threads-design.md`) | `docs/tasks-design.md` |
| Working from a project in v2 (superseded) | `docs/superpowers/specs/2026-09-17-working-from-a-project-design.md` |
| The atlas tools and the `atlas` skills in v2 (superseded in part) | `docs/superpowers/specs/2026-09-16-atlas-tools-design.md` |
| Original brainstorm (not a contract) | `docs/spec.md` |
| The skills' contracts | `skills/<name>/SKILL.md` and `skills/wiki/references/` |

## Why the project exists

claude-obsidian established a workflow the author wants to keep: an inbox,
immutable captured sources, one reviewed operation per change, pages that cite
their sources. Its Python core and its approval ritual (copy a hash, repeat a
timestamp) got in the way, and nothing showed many vaults at once. Atlas keeps
the workflow, replaces the core with Go and MCP, uses git in the vault as the
safety net, and adds the cross-vault view.

## Three rules for a knowledge base

1. One operation, one commit. `plan` validates, the user sees the preview,
   `apply` commits. There is no other write path; a PreToolUse hook refuses
   Write and Edit under `wiki/`.
2. The vault is the user's. Apply commits hand edits as `manual` operations
   first, so a rollback never touches what the user typed in Obsidian.
3. Code owns what code can derive. The core writes `wiki/log.md` and the
   source ledger; the model never targets them.

## Three rules for a project

1. A project is files. It has no git of its own and no engine:
   `atlas/<name>/project.json`, `threads/`, `stubs/`, `specs/`, `plans/`,
   `receipts/`, `phases/`, `inbox/`. The folder takes the project's name
   because a user may open it in Obsidian, which names a vault after its
   folder. Code owns the cards and the board under `threads/`, every
   document's frontmatter, and the first callout of every document; the
   model writes the rest of a document with Edit. The guard refuses
   `threads/`, `project.json`, and a new file written straight into a stage
   folder.
2. A thread's stage is never set: it is the furthest document that exists
   (stub, spec, plan, receipt). The `thread` tool files a document, and that
   is the only way a thread moves, so every change of state is a page the
   user can see. A receipt closes the thread and its card moves to
   `threads/archive/`. A thread names its phase; a phase never lists its
   threads and has no status.
3. Nothing reaches the knowledge base from a thread until the thread is
   completed, and then only through a plan the user sees.

## Three rules for the atlas

1. A project uses one knowledge base. A knowledge base never records which
   projects use it; the atlas computes that from the projects.
2. Projects never link each other. They share a knowledge base.
3. Ids travel; paths stay. Neither identity file holds a path; the atlas
   config holds every path, and nothing else. A project or a knowledge base
   heals its own entry when a session starts in it
   (`vaults.RegisterProject`, `vaults.RegisterKnowledge`).
   `~/.claude-atlas/state/registry.json` is derived, and `refresh` rebuilds it
   in full.

## Tool, skill, or hook

Three carriers, and a new capability splits across them rather than picking one.

1. A tool is a fact or a commit. Code owns whatever two correct runs must
   answer the same way (`status`, `route`, `threads`, `history`, `lint`) and
   every path that changes bytes (`capture`, `plan`, `apply`, `undo`,
   `thread`, `phase`, `stub`, `mode`). No tool writes prose; `thread` files
   the text the model gives it.
2. A skill is a procedure and a policy: which depth to read at, what counts
   as adequate evidence, how to cite, when to stop, which skill comes next.
   A skill is advice. When the model must be refused instead of advised, the
   rule belongs in a tool or in a hook, which is why `PreToolUse` guards
   `wiki/` and no skill asks nicely.
3. Tools and skills do not pair one to one, and naming them alike is the
   trap. `plan` and `apply` serve every writing skill, and `thread` serves
   every stage skill; `think` calls no tool of its own. Tools are nouns and
   stay few, because every description sits in every session's context;
   skills are verbs and load when they trigger. A wanted `wiki-query` tool means the
   split has not happened yet: the code part of querying is candidate
   selection (`search`) and a source's standing in the ledger, and the skill
   keeps the rest.

## Three layers, one backend

`view` is the whole atlas as one screen, for seeing and launching only; every
command is one thing; the atlas tools (`atlas`, `vault`, `project`,
`settings`, `stage`, and the thread tools) are the same things from a Claude
Code session. The rule: an action lives once, as a function in
`internal/vaults`, `internal/refresh`, `internal/vault`, `internal/project`,
`internal/threads`, `internal/txn`, or `internal/capture`. The CLI exposes it
as one subcommand. The TUI and the tools reach it through `actions.Atlas`,
which `actions.Bind` builds in one place. The TUI and the tools are subsets
of the CLI, never the reverse: a new key or a new tool gets a command in the
same change, and `docs/usage.md` carries the table that maps them.

## Layout

```
.claude-plugin/         plugin.json and marketplace.json; this repo is its own marketplace
.mcp.json               the atlas MCP server: scripts/atlas mcp
scripts/atlas           sh wrapper that finds the installed binary
hooks/hooks.json        SessionStart context, PreToolUse guard, PostToolUse touch, Stop warning
skills/                 one directory per skill; skills/wiki/references/ is shared
agents/                 wiki-ingest worker, wiki-lint interpreter
cmd/claude-atlas/       main
internal/cli/           argument parsing and one method per subcommand
internal/actions/       every atlas action as one struct of functions, and Bind, the one place it is built
internal/wizard/        the setup flow
internal/vault/         a knowledge base: identity file, layout, templates, Init, Adopt, Upgrade, mode routing, page skeletons
internal/project/       a project: atlas/<name>/project.json, Init, Open, Locate, FindAbove, Upgrade; writes only the identity file, the folder's name, and the Obsidian snippet (templates/)
internal/place/         where a session is: the project (anywhere inside the work) or the knowledge base, and the config heal
internal/gitx/          the git commands the core needs
internal/txn/           plans, preview, apply, recovery, undo, history
internal/threads/       threads over plain files: cards, stage documents, phases, Sync (the generated cards, callouts, and board), Migrate from 2.x tasks
internal/describe/      the page that describes a project in its knowledge base, how far the work moved since, and the snapshot a page cites; reads only, capture writes the snapshot
internal/capture/       inbox listing, staging into inbox/ (files, and a project's snapshot), capture into .raw/captured/
internal/ledger/        the source ledger
internal/lint/          the health check (ported from claude-obsidian's engine)
internal/mcpserver/     the tools, thin over the packages above
internal/hooks/         session-start (the project line with its knowledge base, page, and open threads, or the knowledge base line with its projects and inbox), guard, touched, stop
internal/claudecode/    Claude Code's plugin registry, `claude plugin`, launching claude in a knowledge base or a project
internal/registry/      the scan of the knowledge bases and projects the config lists, the resolved entries, the registry state file
internal/refresh/       derive one entry's state, rewrite the registry, list an entry's signals
internal/vaults/        create, adopt, register, heal, and edit knowledge bases; init, link, unlink, forget, and register projects
internal/links/         the facts git reports about a folder, and CleanName
internal/tui/           Bubble Tea screens: the view (Knowledge and Projects tabs, expand in place, open and launch keys)
internal/obsidian/      Obsidian's vault registry, obsidian:// URIs, restart
internal/home/          ~/.claude-atlas and config.json
internal/console/       prompts and step lines
```

`~/.claude-atlas/` holds `config.json` and `state/registry.json`. The
knowledge bases and the projects are the user's and live wherever the user
puts them. The atlas has no default location and never searches the disk:
the config lists every knowledge base under `knowledge` (`vaults.Register`,
`vaults.Unregister`) and every project's work folder under `projects`
(`vaults.InitProject`, `vaults.ForgetProject`). `new-knowledge` takes a
path; a bare name is a folder in the current directory
(`vaults.ResolvePath`). The `vault` tool takes only an absolute or `~` path,
because a session's folder is usually a project. A knowledge base never goes
inside another (`vaults.CheckNewPath`), but may go inside a project; a
project never goes inside a knowledge base or another project
(`project.CheckNew`). A
session heals its own entry by id when its folder moved or the config does
not list it (`vaults.RegisterKnowledge`, `vaults.RegisterProject`).

## Constraints

- Dependencies: `gopkg.in/yaml.v3`, the official MCP `go-sdk` pinned to v1.4.0
  (later versions need Go 1.25; the machine runs 1.24), and Bubble Tea, Bubbles,
  and Lip Gloss. Nothing else. Do not pull `golang.org/x/*` at `@latest`.
- The atlas tools call `actions.Bind` once per call over a config loaded for
  that call, and never use `plan`/`apply`: nothing they do deletes user data,
  and the skill's one-line statement is the preview. `atlas` without
  `refresh` writes nothing; every write tool scans afresh to resolve names.
- `git` is a runtime requirement. `python3` is not. macOS and Linux only.
- The binary and the plugin are installed separately. `scripts/atlas` finds the
  binary on PATH, in `~/go/bin`, in the Homebrew prefixes, or at
  `$CLAUDE_ATLAS_BIN`. `plugin.json` and `marketplace.json` carry the version
  the binary should match; `status` and `doctor` warn on a mismatch.
- Every write into a knowledge base goes through `txn.Prepare` and
  `txn.Apply`. `vault.Init`, `vault.Adopt`, `vault.Upgrade`, and
  `vault.UpdateConfig` are the only code that writes vault files directly, and
  only before or outside an operation. The template includes the vault's CSS
  snippet and an appearance file that enables it; upgrade merges the snippet
  into an existing appearance file.
- A vault is a knowledge base (`vault.Kind` is the one value the identity file
  may carry, in the v3 identity file with an `id`, a `name`, a `mode`, and a
  `scope`). `Open` reads a v2 knowledge base as it is and refuses a v2 project
  vault (`vault.ErrProjectVault`); `Upgrade` raises the schema. Templates live
  under `internal/vault/templates/knowledge/`.
- A vault's own facts change only through `vault.UpdateConfig`, which
  validates the identity file, refuses a new kind or a new id, and commits the
  file as a `setup` operation. `vaults.EditIdentity` is its only caller. A
  vault's folder takes its name: `vaults.EditIdentity` renames the leaf and
  keeps the parent, a taken folder refuses the whole edit, and the folder name
  is `links.CleanName` of the vault's name.
- A project's identity is `atlas/<name>/project.json` (`project.Config`:
  schema, id, name, description, created, and the one `knowledge` it uses,
  by id and name). `project.Init` writes it and the folders and nothing
  else; `project.Save` is the only other writer, for link, unlink, and edit.
  The project folder is `links.CleanName` of the project's name; `Save`
  moves it when the name changes, and a taken folder refuses the save.
  `project.Locate` finds the folder as the one child of `atlas/` that holds
  `project.json`, and refuses two. A project in the flat layout of 2.2.0
  and earlier (`atlas/project.json`) is refused with `project.ErrFlat` and
  scanned as `ReasonFlat`; only `claude-atlas upgrade` moves it
  (`project.Upgrade`).
  `vaults.InitProject` also runs `git init` in a work folder that is in no
  repository (on `main`, no commit), unless the caller asks for none. A
  project session heals the config (`vaults.RegisterProject`): an unknown
  project is added, one whose id sits at another path is moved, and one
  listed at a path that is gone is taken for the moved one only when it is the
  only one gone.
- The project side has no engine. `threads` reads and writes plain files.
  `Load` reads the cards and the documents and derives each thread's stage;
  a page it cannot use is a `Problem`, never an error. `Start`, `File`,
  `Set`, `Reopen`, `Touch`, and the phase functions change pages, and every
  one ends in `Sync`, which rewrites each card's `stage`, `outcome`, body,
  and folder, the first callout of each document (`replaceLead`, which
  replaces only a callout of a stage's own type), and `threads/threads.md`.
  `Sync` writes a file only when its content differs and never changes
  `updated`; the session-start hook runs it. `setField` rewrites one
  frontmatter line and keeps every other line, so a hand-added property
  survives. A document names its thread by id, never by file name. A
  thread's `phase` must name a phase page; `Set` refuses an unknown one,
  `RemovePhase` refuses while a thread names it, `RenamePhase` rewrites
  every card that does. There is no ledger; `updated` on the card is the
  last touch, `Stale` reads it, and the `touched` hook sets it when a
  document is edited.
- `project.Init` and `EnsureFolders` write `.obsidian/snippets/claude-atlas.css`
  (the stage callouts and folder colors) into the project folder, and
  `appearance.json` only when there is none, so a user who turned the
  snippet off keeps it off. `upgrade` rewrites the snippet.
- In the atlas, "thread" means a project's thread only. The bullets under
  `## Active Threads` in a knowledge base's `wiki/hot.md` are shown as "Hot
  topics" (`refresh.HotTopics`).
- A kind bounds a plan's writes (`txn.allowed`). Reserved: `wiki/log.md`, the
  source ledger, `ideas/`, `.git`, `.vault-meta`, `.obsidian`, `.raw` except
  through capture, `inbox` except deletes in an ingest.
- A new vault lints clean, and `lint.TestNewVaultHasNoFindings` holds the
  template to that. A folder's index page takes the folder's name
  (`wiki/canvases/canvases.md`), so no page shares the basename of
  `wiki/index.md`. Lint's `kind_errors` names a v2 project folder left in a
  knowledge base (`wiki/tasks`, `wiki/questions`, `wiki/sessions`, `kb`,
  `repos`).
- A knowledge base commits into the git repository that holds it
  (`vault.RepoAt`, `gitx.At`): its own, or the working tree above it with
  every command scoped to its folder, so a knowledge base inside a project
  shares the project's repository and history. `vault.Init` and
  `vault.Adopt` run `git init` only when no repository holds the folder
  (`joinRepo`), and a folder the holding repository ignores is refused
  (`vault.HostFor`). Nothing records which; a clone answers the same way.
  `describe` leaves a knowledge base inside the work out of the snapshot and
  out of `Behind` (`describe.KnowledgeDirs`).
- The server and the hooks resolve the session through `place.Resolve`: an
  explicit path, `CLAUDE_ATLAS_VAULT`, then the nearer of the nearest
  `atlas/<name>/project.json` and the nearest `.claude-atlas.json` at or above the
  working directory, so a knowledge base inside a project is its own place. A
  project session's knowledge base comes from the registry by the id in
  `project.json`; a project whose knowledge base is not on this machine still
  works for threads, and the place carries `KnowledgeError`.
- A source captured from a project session records `via`, the project's id
  and name, as provenance (`capture.Capture` with a `*ledger.Via`).
- The scan is the truth. `registry.Scan` reads the identity file under every
  path in `config.knowledge` and `atlas/<name>/project.json` under every path
  in `config.projects`, and nothing else. An entry the atlas knows but cannot
  read becomes an entry with a `Path`, an `Error`, and a `Reason` code
  (`ReasonV1`, `ReasonV2Project`, `ReasonFlat`, `ReasonUnreadable`, `ReasonSchema`,
  `ReasonMissing`, `ReasonNotProject`, `ReasonNotVault`); `list` and `doctor` decide on the code, and the view
  files it under `problems`. Every command that acts on an entry scans
  afresh; `registry.json` is for display only.
- The page that describes a project is an entity page in its knowledge base
  with `entity_type: project` and a `project` property holding the project's
  id or name (`describe.Page`); its `commit` is what `Behind` counts from
  when the work is a repository.
- Lint is read-only; refresh writes nothing a vault's git tracks.
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

1.0.0 is the first v2 release; 1.1.0 is the view with tabs; 1.2.0 is
clusters; 1.3.0 is the atlas tools and skills; 1.4.0 is repositories in the
knowledge base; 1.4.1 is the same release, republished so the plugin cache
took the whole of it. 2.0.0 is v3: a project is an `atlas/` folder in the
work, one knowledge base per project, phases, and no mounts, clusters,
grants, or linked repositories. 2.1.0 lists every knowledge base in the
config and drops the vaults directory and `relocate`; `init` makes the work
a git repository. 3.0.0 puts a project in `atlas/<name>/` and replaces tasks
with threads; `upgrade` moves a 2.x project over.

## Open questions

- A `search` tool with BM25 ranking, once Grep proves insufficient.
- Many knowledge bases on one project, once one is not enough in practice.
- Distribution: a Homebrew tap and release binaries; then the wrapper can
  download a checksummed binary into `${CLAUDE_PLUGIN_DATA}`.
