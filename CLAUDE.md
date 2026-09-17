# claude-atlas

A Go binary and a Claude Code plugin. The binary creates and maintains
Obsidian knowledge vaults and serves the MCP tools Claude uses inside them; the
plugin carries the skills and hooks. The atlas side lists every vault from a
scan of the vaults directory and shows them in a terminal view.

Read `README.md` first. This file holds what the code and README do not say.

## Sources of truth

| Thing | Location |
|---|---|
| v2: knowledge bases, projects, mounts, access (phases 1–6 built) | `docs/v2-design.md` |
| Core design and the reasons behind it | `docs/core-design.md` |
| The atlas side before v2 (superseded by `v2-design.md`) | `docs/atlas-design.md` |
| Tasks: pages, ledger, skills, repos reaching the vault | `docs/tasks-design.md` |
| Working from a project: repository pages, the work skill (phases 1–2 built) | `docs/superpowers/specs/2026-09-17-working-from-a-project-design.md` |
| The atlas tools and the `atlas` skills | `docs/superpowers/specs/2026-09-16-atlas-tools-design.md` |
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
   from the projects' identity files. A member never learns which clusters
   hold it.
2. A project never links another project. Projects share knowledge bases.
3. Ids travel; paths stay. The identity file holds no path; the atlas config
   holds the paths the atlas cannot compute, and nothing else.
   `~/.claude-atlas/state/registry.json` is derived, and `refresh` rebuilds it
   in full.

## Tool, skill, or hook

Three carriers, and a new capability splits across them rather than picking one.

1. A tool is a fact or a commit. Code owns whatever two correct runs must
   answer the same way (`status`, `route`, `mounts`, `tasks`, `history`,
   `lint`) and every path that changes bytes (`capture`, `plan`, `apply`,
   `undo`, `plant`, `stub`, `mode`). No tool writes prose.
2. A skill is a procedure and a policy: which depth to read at, what counts
   as adequate evidence, how to cite, when to stop, which skill comes next.
   A skill is advice. When the model must be refused instead of advised, the
   rule belongs in a tool or in a hook, which is why `PreToolUse` guards
   `wiki/` and no skill asks nicely.
3. Tools and skills do not pair one to one, and naming them alike is the
   trap. `plan` and `apply` serve every writing skill; `think` and
   `task-plan` call no tool of their own. Tools are nouns and stay few,
   because every description sits in every session's context; skills are
   verbs and load when they trigger. A wanted `wiki-query` tool means the
   split has not happened yet: the code part of querying is candidate
   selection (`search`) and a source's standing in the ledger, and the skill
   keeps the rest.

## Three layers, one backend

`view` is the whole atlas as one screen; every command is one thing from it;
the atlas tools (`atlas`, `vault`, `mount`, `cluster`, `repo`, `settings`,
`stage`) are the same things from a Claude Code session. The rule: an action
lives once, as a function in `internal/vaults`, `internal/refresh`,
`internal/vault`, `internal/txn`, or `internal/capture`. The CLI exposes it
as one subcommand. The TUI and the tools reach it through `actions.Atlas`,
which `actions.Bind` builds in one place. The TUI and the tools are subsets
of the CLI, never the reverse: a new key or a new tool gets a command in the
same change, and `docs/usage.md` carries the table that maps them.

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
internal/actions/       every atlas action as one struct of functions, and Bind, the one place it is built
internal/wizard/        the setup flow
internal/vault/         identity file, layout, templates, Init, Adopt, mode routing, page skeletons
internal/gitx/          the git commands the core needs
internal/txn/           plans, preview, apply, recovery, undo, history, planting a task
internal/tasks/         task pages, the derived task ledger and index; never decides to write
internal/discover/      the project a folder belongs to, through the projects' repositories
internal/repomap/       the page that describes a repository, the commit it was written from, how far the repository moved since, and the snapshot a page cites; reads only, capture writes the snapshot
internal/capture/       inbox listing, staging into inbox/ (files, and a repository's snapshot), capture into .raw/captured/
internal/ledger/        the source ledger
internal/lint/          the health check (ported from claude-obsidian's engine)
internal/mcpserver/     the tools, thin over the packages above
internal/hooks/         session-start (the kind line for a vault or a project's repository, the Knowledge: mount lines, the Repository: lines, the search sentence, the stubs and wanted counts, open tasks, hot cache), guard, stop
internal/claudecode/    Claude Code's plugin registry, `claude plugin`, launching claude in a vault
internal/registry/      the scan for identity files, the resolved entries, the registry state file
internal/refresh/       derive one vault's state, rewrite the registry, list a vault's signals
internal/vaults/        create, adopt, register, edit identity files, link and create repositories, adopt those waiting under repos/, add and remove a cluster's members
internal/links/         git init, create and clone, change policies, the facts git reports
internal/tui/           Bubble Tea screens: the view (a tab per kind, boards of boxes with connectors), the vault editor, the repositories screen, tasks, ingest, the add and adopt screens
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
- The atlas tools call `actions.Bind` once per call over a config loaded for
  that call, and never use `plan`/`apply`: nothing they do deletes user data,
  and the skill's one-line statement is the preview. `atlas` without
  `refresh` writes nothing; every write tool scans afresh to resolve names.
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
- A vault's folder takes its name. `vaults.EditIdentity` renames the leaf and
  keeps the parent, so a vault inside and one outside the vaults directory
  move alike; it returns the path the vault sits at afterwards, and every
  caller threads it through (`tui.Hooks.Edit` too, so the view keeps its
  cursor and its expanded block). A taken folder refuses the whole edit, the
  folder name is `links.CleanName` of the vault's name, a case-only rename is
  allowed on a case-insensitive filesystem (`os.SameFile`), and a project at
  `REPO/atlas/` keeps its folder. Mount folders keep their own names, because
  pages link through `kb/<name>`.
- A repository is where a project's deliverables go, always a git repository;
  memory stays in the vault, and ingest sources are not repositories. The
  project's identity file records the name, the remote, and the change policy
  (`changes: pr` or `commit`); a repository outside `<project>/repos/<name>`
  has its path in the atlas config, and a path inside the vault but outside
  `repos/` is refused. Sessions learn the policy from the hook and the `repos`
  tool.
- The folder decides membership. A git repository under `<project>/repos/` is
  a repository of that project: `vaults.AdoptRepos`, which `refresh.Registry`
  runs with `ensure`, writes it to the identity file, and `RemoveRepo` refuses
  a name whose folder is still there. Adoption is the only place a scan leads
  to a write, so it lives behind `ensure` and never in `registry.Scan`.
- Every repository the atlas links records `changes` explicitly, from the
  config's `default_repo_changes` (`commit` unless set). `links.Policy`'s
  fallback to `pr` on a remote is now only for an identity file written before
  that, or edited by hand.
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

1.0.0 is the first v2 release; 1.1.0 is the view with tabs; 1.2.0 is
clusters; 1.3.0 is the atlas tools and skills; 1.4.0 is repositories in the
knowledge base.

## Open questions

- A `search` tool with BM25 ranking, once Grep proves insufficient.
- A Claude Code skill for the bird's-eye conversation across every vault.
- Distribution: a Homebrew tap and release binaries; then the wrapper can
  download a checksummed binary into `${CLAUDE_PLUGIN_DATA}`.
