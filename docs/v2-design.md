# Atlas v2: knowledge bases and projects

Status: designed 2026-09-14. Phases 1–6 are built. Phase 7 (migration by
hand) is not.
This is a new version of the project. Existing vaults migrate by hand;
nothing here keeps compatibility with the v1 identity file, the v1 layout,
or the atlas vault.
`core-design.md` still describes the engine. `atlas-design.md` and the atlas
sections of `tasks-design.md` are superseded by this document.

## What changes and why

Atlas gives a project a knowledge base. The knowledge base is the wiki as it
is today: sources, entities, concepts, ingest, query, lint. The project is
where the work is: tasks, questions, ideas, notes, and linked repositories. A
project mounts one or many knowledge bases, and several projects share one.

Today every vault is both, so knowledge that two projects need is written
twice. On 2026-09-14 `cs566-course` created a concept page `Agent Skills`
while `personal-kb` already had a page with the alias `agent skills`;
`cs513-course` and `cs513-project` both hold `Models of Computation`. No
check sees across vaults: Obsidian's links, backlinks, graph, and rename,
`route`'s match on a title, and lint's duplicate check all stop at the vault
boundary. v2 moves shared knowledge into a vault of its own and gives a
project a way to link into it.

The atlas vault (`~/Documents/Atlas`) goes away. The atlas keeps config and
derived state under `~/.claude-atlas/`. `view` and the CLI are the view across
vaults.

## One engine, two kinds

A vault has a kind: `knowledge` or `project`. Both use the same engine:
`wiki/`, `.raw/`, the source ledger, plans, apply, undo, lint, git. The kind
decides which parts exist.

| | knowledge | project |
|---|---|---|
| `wiki/`: sources, entities, concepts | yes | yes, about the project itself |
| `wiki/`: questions, sessions | no | yes |
| `inbox/` | no | yes |
| `wiki/tasks/`, `inbox/tasks/`, `ideas/`, the task ledger | no | yes |
| `kb/` mounts | no | yes |
| repositories | no | yes |
| `scope`, `access`, `grants` | yes | no |

A knowledge base:

```text
ai-ml/
├── .claude-atlas.json
├── .git/  .gitignore  .obsidian/
├── .raw/captured/<sha256>.ext
└── wiki/
    ├── index.md  log.md  hot.md  overview.md
    ├── sources/  entities/  concepts/  canvases/
    └── meta/ledgers/source-ledger.json
```

A project:

```text
cs566/
├── .claude-atlas.json
├── .git/  .gitignore  .obsidian/
├── .raw/captured/<sha256>.ext
├── inbox/  inbox/tasks/  ideas/
├── kb/ai-ml -> ~/Documents/Vaults/knowledge/ai-ml/wiki    symlink, ignored by git
├── repos/<name>/                                           optional, ignored by git
└── wiki/
    ├── index.md  log.md  hot.md  overview.md
    ├── sources/  entities/  concepts/  questions/  sessions/  canvases/
    ├── tasks/  tasks/archive/
    └── meta/ledgers/source-ledger.json  task-ledger.json
```

Each vault has its own mode (`generic` or `lyt`), git history, and hot
cache. `vault.RoutableTypes` takes the kind as well as the mode.

## The identity file

`.claude-atlas.json`, schema `claude-atlas.vault.v2`, holds the facts that
travel with the vault. It never holds a path. A path is a fact about one
machine and lives in the atlas config.

A knowledge base:

```json
{
  "schema": "claude-atlas.vault.v2",
  "id": "6f1d2a9c-3b7e-4c1a-9f2d-8e5a1b0c7d44",
  "kind": "knowledge",
  "name": "ai-ml",
  "mode": "generic",
  "created": "2026-09-14",
  "scope": "Machine learning: models, training, evaluation, deployment, agents.",
  "access": "open",
  "grants": [{ "id": "b3e0…", "name": "cs566", "access": "write" }]
}
```

A project:

```json
{
  "schema": "claude-atlas.vault.v2",
  "id": "b3e0f5a2-…",
  "kind": "project",
  "name": "cs566",
  "mode": "generic",
  "created": "2026-09-14",
  "tags": ["usc"],
  "mounts": [{ "id": "6f1d…", "name": "ai-ml", "access": "write" }],
  "repos": [{ "name": "cs566-hw", "remote": "git@github.com:…", "changes": "commit" }]
}
```

- `id` is a UUID minted at creation. Names repeat across machines; ids do
  not. The atlas, mounts, and grants refer to a vault by id. `name` is for
  people and for the `kb/<name>` folder. `vault.Name()` reads it from the
  file, not from the folder.
- `scope` is one sentence. The ingest skill reads it to choose the knowledge
  base a source belongs to.
- `tags` replaces the category tree. `view` groups projects by tag.
- `repos` records a repository by name, with its remote and its change policy
  (`pr` or `commit`, as today). A repository at `<project>/repos/<name>/`
  needs no path. The atlas config records the path of any other.
- `mode` changes through a `config` operation, as today. Every other field
  changes through the CLI (`mount`, `unmount`, `grant`, `revoke`, `edit`),
  which commits the file as a `setup` operation.

## Mounts

A project mounts a knowledge base as a symlink:

```text
<project>/kb/<name>  ->  <knowledge base>/wiki
```

- `kb/` is in the project's `.gitignore`. The symlink is local state:
  `claude-atlas mount` creates it, and `doctor` and `refresh` recreate a
  missing one from the identity file and the atlas config. A project cloned
  onto another machine gets its links back once the knowledge bases are
  registered there.
- A `[[link]]` in a project page resolves to a knowledge base page in
  Obsidian, in the graph, in backlinks, and in lint. Two mounts with one page
  name fall back to `[[kb/ai-ml/concepts/Backpropagation]]`, the way Obsidian
  resolves any duplicate.
- The atlas tools return real paths. ripgrep does not follow symlinks by
  default, so a skill that greps a mount is handed the knowledge base's
  `wiki/` path, not `kb/<name>`. The one exception is a stub the `stub` tool
  seeds through a title's `target`: it returns that page's path as
  `kb/<name>/<path inside the knowledge base's wiki>`, readable from the
  project through the symlink. The response's `operations[].vault` still
  names the knowledge base's own root, where the page was committed.
- A knowledge base never links a project and never records who mounts it. The
  atlas computes that list from the projects' identity files.
- A rename in Obsidian through a mount edits the knowledge base's files. The
  next operation in that knowledge base commits the change as `manual`, like
  any hand edit. Other projects that link the renamed page see a dead link at
  their next lint.
- Two knowledge bases with one name on a machine: `mount` asks for the mount
  folder's name (`--as NAME`).

Checked 2026-09-15: `~/Documents` is not an iCloud zone on the author's
machine (`brctl status` reports no client zone; only `Desktop` is linked into
CloudDocs), and a symlink under `~/Documents/Vaults` survived unchanged.

The Obsidian follow-symlink check needs a live Obsidian and is a manual step.
Create a project and a knowledge base (`new-project`, `new-knowledge`),
`mount` the knowledge base on the project, `open-vault` the project, and
write `[[<a knowledge base page>]]` in the project's `wiki/hot.md` in
Obsidian. Confirm the link opens the page, the graph shows it, and a bare
`[[index]]` opens the project's own index, not the mount's. If any of these
fails, the fallback is `obsidian://open` links that the atlas tools resolve.
Not yet run.

## Access

There is no authentication: one machine, one user. Access states intent, so a
session in one project does not write a knowledge base by accident.

- A knowledge base sets `access`. `open`: every project that mounts it may
  write. `guarded`: a project may write only when `grants` names its id with
  `write`; every other project reads. Both are visible to every project. A
  guarded knowledge base is not hidden.
- A project's mount sets `access`: `read` or `write`. `write` is the default;
  `mount --read` asks for less.
- The effective access is the lesser of the two. `status` reports it per
  mount. `plan` refuses a write into a knowledge base the project may not
  write. The `guard` hook denies Write and Edit under `kb/`, and under every
  vault's `wiki/`, `.raw/`, and identity file, as today.
- `claude-atlas grant KB PROJECT --write|--read` and `revoke KB PROJECT|ID` edit
  `grants`. There is no block list: `guarded` with an empty list is read-only
  for every project.

The docs say plainly that this is hygiene, not security. Any process on the
machine can read the files.

## Knowledge enters through a project

A knowledge base has no inbox. Every source enters through a project's
`inbox/`, and every knowledge operation in a knowledge base comes from a
project session. An operation touches one vault. A source that yields pages
in two vaults is two operations, one commit each.

The ingest flow in a project session:

1. `inbox` lists the project's inbox, as today.
2. The skill reads the source and calls `route` with the title and type.
   `route` answers for the project and for every mount: the existing page
   whose title or alias matches, with its vault and access; and the path a new
   page would take in each vault the project may write. A match anywhere means
   the skill links to that page instead of creating one. This is the check v1
   could not make.
3. The skill picks the target vault from the knowledge bases' `scope` lines,
   or the project's own wiki when no knowledge base fits. The preview names
   the target vault.
4. `capture` with `vault` set to the target copies the file into that vault's
   `.raw/captured/` and writes the ledger record, one commit in the target.
   When the target is a knowledge base, the record carries `via`: the
   project's id and name. That is provenance, not a link.
5. `plan` and `apply` on the target: the source page, concept pages, the
   index, the hot cache.
6. `plan` and `apply` on the project, kind `ingest`: remove the inbox file,
   and, when useful, a project page that cites the source through the mount.

The knowledge base's ledger says which project brought each source. The
project's log says what was filed where, because the `apply` summary names
the target vault.

A page a project filed internally can move to a knowledge base later. That is
a `promote` operation, left for later.

A knowledge base session is for maintenance: `lint`, `repair`, `fold`, `mode`,
`stub`, `undo`. `ingest` and `save` need a project. A knowledge base session
refuses them and names the projects that mount the knowledge base.

## Sessions

The server and the hooks resolve the session's vault in this order: an
explicit `vault`, `CLAUDE_ATLAS_VAULT`, the nearest identity file above the
working directory, then the atlas: a folder inside a project's repository
belongs to that project. Several matches name the candidates, as today.

The session-start hook prints, in a project:

```text
claude-atlas: project cs566 (generic) at ~/Documents/Vaults/projects/cs566.
Knowledge: ai-ml (write) · Machine learning: models, training, evaluation, deployment, agents · 61 pages · kb/ai-ml
Search the project and its knowledge bases (the wiki-query skill) before answering from the code alone.
Open tasks: …
<vault-context> the project's hot.md </vault-context>
```

One line per mount: name, effective access, scope, page count, mount folder.
The hot caches of mounted knowledge bases are not loaded. The query skill
reads them when it needs them.

In a knowledge base:

```text
claude-atlas: knowledge base ai-ml (generic, open), mounted by cs566 (write), self-study (write).
Knowledge enters through a project. Here: lint, repair, fold, stub.
<vault-context> the knowledge base's hot.md </vault-context>
```

`plan` with `vault` set to a mounted knowledge base checks the session's
project against the knowledge base's grants. With no session project, a
knowledge kind is refused.

## Tools

| Tool | Vault kind | Change in v2 |
|---|---|---|
| `status` | both | kind, id, name; a project's mounts with effective access; a knowledge base's access and the projects that mount it |
| `inbox` | project | none |
| `capture` | target: both; the file comes from the project's inbox | `via` on a knowledge base record |
| `route` | project | matches an existing page by title or alias across the project and its mounts |
| `plan` | both | the access check; kinds by vault kind |
| `apply` | both | consumes a plan the access check already admitted |
| `undo`, `mode` (a `set`) | both | gated the same way as `plan` |
| `history`, `mode` (a read) | both | none |
| `lint` | both | resolves links through mounts |
| `stub` | both | built (`stubs-design.md`); in a project, a title may name a writable mount as its `target`; the default is the project's own wiki |
| `plant`, `tasks`, `repos` | project | none |
| `mounts` | project | new: each mount with id, name, real path, requested and effective access, scope |

## Operations

Kinds by vault kind:

| Kind | knowledge | project |
|---|---|---|
| `ingest`, `save` | from a project session with write access | yes |
| `markdown`, `repair`, `fold`, `canvas`, `base`, `config`, `stub` | yes | yes |
| `task` | refused | yes |
| `capture`, `undo`, `setup` (the core's own) | yes | yes |

`txn.allowed` keeps its scope per kind. `kb/` and `repos/` are reserved
everywhere: they are not the project's files, so no plan writes under them.
`plan` checks access when the target is a knowledge base; `apply` commits a
plan the access check already admitted, and does not check again. `undo` and
a `mode` that sets a new mode are gated the same way as `plan`. A plan names
paths in one vault.

## Lint

Lint in a project reads the project's own `wiki/` as pages and every mount as
link targets. Findings are about the project's pages. A knowledge base's
findings come from its own lint.

- A bare link resolves in the project's own `wiki/` first, then in the mounts.
  Obsidian resolves an ambiguous bare link to the nearest file, and the
  project's root pages sit together in `wiki/`, so the two agree for links
  among them. The Obsidian check above confirms it, once run.
- A page name present in the project and in a mount, or in two mounts, is
  ambiguous, except the root pages (`index`, `log`, `hot`, `overview`) and the
  folder index pages (`canvases`), which every vault has and which resolve to
  the project's own.
- The wanted-page check in `stubs-design.md` looks in the mounts too. A link
  that resolves in a mount is not wanted. The near-match check also covers a
  mount's page names and aliases, so `[[Backpropogation]]` in a project is a
  dead link with the suggestion `Backpropagation`, whichever vault holds it.
- `stub` in a project creates a wanted page in the project's own wiki unless
  the title names a writable mount as its `target`. Then the page lands in
  the knowledge base, the operation's summary names the project, and the
  link resolves through the mount. A stub never lands in a read-only mount.
- A knowledge base with `inbox/`, `ideas/`, `wiki/tasks/`, the task ledger,
  `wiki/questions/`, or `wiki/sessions/` is a finding. A project with `grants`
  or `scope` is a finding.
- `mount_errors` lists four states under `kb/`: an entry that is not a symlink,
  a symlink that points at nothing, a target that is not a directory, and a
  target that is a directory other than a knowledge base's `wiki/`. The CLI's
  `lint` reads the symlinks and reports them; the MCP `lint` tool passes the
  registry's mounts instead, so it resolves links even before `refresh` has
  recreated a link, and it reports no `mount_errors`. `doctor`, `refresh`, and
  the session hook name a broken link.

## A project inside a repository

By default a project is its own git repository and mounts repositories, as
today. As an option, `new-project NAME --in REPO` creates the project at
`REPO/atlas/`, tracked by the repository's git, so a clone on a machine with
atlas installed is ready to work. The layout is detected, not stored: the
root is named `atlas`, has no `.git`, and its parent has one (`vault.HostRepo`).

- The vault root is `REPO/atlas/`; its git repository is `REPO`. Every git
  command the engine runs takes the pathspec `atlas/`. The manual-edits
  commit stages and commits only paths under `atlas/`, and the operation
  commit is `git commit -- atlas/`. An operation never commits half-written
  code outside `atlas/`.
- `atlas/.gitignore` ignores `.vault-meta/`, `kb/`, and Obsidian's workspace
  files, as a standalone vault does.
- `undo` reverts the operation commit, which touched only `atlas/`.
- Wiki commits land on the current branch. Under a `changes: pr` policy they
  ride the pull request with the code. That suits a small team; the docs say
  so.
- The repository is the project's repository. `repos` lists it without an
  entry in the identity file.
- The project sits outside the vaults directory, so `new-project --in` adds
  its path to the atlas config.

## The atlas

`~/.claude-atlas/config.json`, schema `claude-atlas.config.v2`:

```json
{
  "schema": "claude-atlas.config.v2",
  "vaults_dir": "~/Documents/Vaults",
  "vaults": ["~/SoftwareProjects/foo/atlas"],
  "repos": { "b3e0…/cs566-hw": "~/SoftwareProjects/cs566-hw" },
  "plugin": {}, "claude_code": {}, "heat": {}
}
```

- `vaults` lists vaults outside `vaults_dir`. `repos` maps a project id and a
  repository name to a path, for repositories outside the project folder.
  These are the only stored paths.
- The server, the hooks, and the CLI find vaults by scanning (`registry.Scan`):
  `vaults_dir` at most five levels deep, plus the listed paths, for identity
  files. The scan skips folders whose name starts with a dot and folders named
  `node_modules`, and does not descend into a vault it has found. It reads only
  the identity files. Nothing depends on the registry below.
- `state/registry.json` is derived for `view`, `list`, and `show`. `refresh`
  deletes the state directory and rewrites the file from the scan: every vault
  with id, kind, name, path, mode, page counts, heat, unfinished work, and last
  operation; for a project, its mounts and repositories resolved to paths with
  effective access, and its open tasks; for a knowledge base, the projects that
  mount it. A mount whose id the scan does not find is a finding. Every command
  that acts on a vault scans afresh, so a stale file never decides what it acts
  on; `doctor` scans and does not read the file at all.
- A vault the scan finds but cannot read (a v1 identity file, one that is not
  JSON) becomes an entry with a path and a reason. `view` files it under
  `problems`, `list` and `doctor` name it.

Default locations (`vaults.PathFor`): `<vaults dir>/knowledge/<name>` and
`<vaults dir>/projects/<name>`. The user may give a path. A vault never goes
inside another vault; `repos/<name>/` inside a project is the one exception,
and the project's git ignores it.

`view` is one tree. A project sits at `projects/<first tag>/<name>`, or
`projects/<name>` with no tag; a knowledge base at `knowledge/<name>`; a vault
the scan could not read under `problems`. Each is a box with its heat, name,
page count, unfinished count, and days idle. Enter opens the detail: id, path,
mode, and either a project's tags or a knowledge base's scope, access, grants,
and the projects that mount it; then the mounts with effective access, the
repositories with what git says, the state the last refresh derived, the open
tasks, and the signals. Every key is one command:

| Key | Command |
|---|---|
| `n` new project, `N` new knowledge base | `new-project NAME`, `new-knowledge NAME` |
| `a` adopt | `adopt PATH --as project\|knowledge` |
| Enter on a vault | `show NAME` |
| `e` edit, then `r` forget | `edit NAME --name --tags --scope --access`, `remove NAME` |
| `l` repositories | `new-repo`, `link`, `unlink`, `edit-repo`, `repos` |
| `t` tasks, `p` plant, `c` continue; `T` every project's tasks | `tasks`, `plant`, `open-claude --task` |
| `o` Obsidian, `c` Claude Code, `i` ingest | `open-vault`, `open-claude`, `ingest` |
| `R` refresh | `refresh` |
| `m` mounts: on a project `a` mount, `u` unmount; on a knowledge base `w` `r` grant, `x` revoke, `a` grant by name | `mount PROJECT KB [--read] [--as NAME]`, `unmount PROJECT KB\|NAME`, `grant KB PROJECT --write\|--read`, `revoke KB PROJECT\|ID` |

Removed: `relate`, `unrelate`, `edit-link`, the `related` field, the category
move, `Overview.md`, `Tree.md`, `categories/`, `repos/` pages, `About.md`,
`Reference.md`, and the graph settings the atlas vault carried. Packages
removed: `tree`, `pages`, `links/pages.go`, and the rendering half of
`refresh`. `discover` reads the scan instead of link pages. `vaults` keeps
create, adopt, mount, grant, and the repository functions.

## Rules

The three vault rules stand: one operation, one commit; the vault is the
user's; code owns what it can derive. v2 adds:

4. One engine, two kinds. The kind decides which folders, operation kinds,
   tools, and skills exist.
5. A knowledge base never learns who mounts it. The atlas computes that.
6. A project never links another project. Projects share knowledge bases.
7. Ids travel; paths stay. The identity file holds no path. The atlas config
   holds every path the atlas cannot compute.
8. Knowledge enters through a project. A knowledge base has no inbox, and its
   ledger records the project each source came through.
9. One vault per operation.
10. Split a knowledge base only at a trust boundary. Two knowledge bases cannot
    link each other, so a knowledge base is a domain, few and large; projects
    are many and small.

## Skills

| Skill | Change |
|---|---|
| `wiki` | routes by kind; in a knowledge base session, names the maintenance skills and the projects that mount it |
| `wiki-ingest` | the flow above: `route` across mounts, the target vault by scope, `capture` into it, one operation per vault |
| `wiki-query` | reads the project and every mount; a citation names the vault with the page |
| `save` | a project session only; may target a writable mount |
| `wiki-lint` | per vault; in a project, link findings through mounts |
| `task`, `task-plant`, `task-plan`, `task-run`, `task-finish` | a project only; otherwise unchanged |
| `wiki-mode`, `wiki-fold`, `canvas`, `obsidian-bases`, `obsidian-markdown`, `think` | unchanged |
| `references/mounts.md` | new: what a mount is, how links resolve, access |

The `wiki-ingest` agent's brief carries the mounts' real paths.

## Migration, by hand

No code migrates a v1 vault. `adopt PATH --as knowledge` writes the v2
identity file and removes `inbox/`, `ideas/`, `wiki/tasks/`, and the task
ledger, in one `setup` commit. Question and session pages stay, and lint
reports them until they are moved to a project or deleted. Adopt commits a
baseline first when the tree is dirty or has no history, so nothing it removes
is lost. `adopt PATH --as project` writes the identity file and keeps
everything. On this machine:

1. `new-knowledge ai-ml`. Copy the source, entity, and concept pages of
   `personal-kb` and `cs566-course` into it. Merge the two `.raw/captured/`
   folders (names are content hashes, so identical files collapse) and the
   two source ledgers. Merge duplicate concepts by hand. Run lint until clean.
2. `new-project cs566`, `new-project cs566-project`, `new-project self-study`.
   Move the syllabus, the reading list, and the project notes into them.
   `mount` each on `ai-ml`.
3. `adopt --as knowledge` or `--as project` for the rest, one at a time.
4. Delete `~/Documents/Atlas`. `setup` writes the v2 config.

## Phases

The stubs work (`superpowers/plans/2026-09-14-stubs-status.md`) landed on
`main` through Task 7 and stays. What it left open joins these phases: the
atlas counts move to the registry in phase 2; the mount rule, the near match
across mounts, and the stub `target` go in phase 3, which also wrote the
mounts and access documentation; the session-start counts line, the skills,
and the final review of the deferred findings go in phase 6; the end-to-end
check runs in phase 7 against a project with a mount.

1. Engine: the v2 identity file with id, kind, and name; templates by kind;
   operation kinds by vault kind; `new-knowledge`, `new-project`,
   `adopt --as`; tools gated by kind; `lint.TestNewVaultHasNoFindings` for
   both kinds.
2. Atlas state: config v2, the scan, `registry.json`, `refresh`, discovery
   through the scan. Delete `tree`, `pages`, `links/pages.go`, the atlas-vault
   rendering, and the `relate` commands.
3. Mounts and access. First the symlink check on this machine, recorded in
   this document. Then `mount`, `unmount`, `grant`, `revoke`; effective access
   in `plan`, `apply`, and the guard; `route` and lint across mounts; `via` in
   the ledger; the session hook; the `mounts` tool.
4. The mount and grant keys in `view`; CLI parity; `doctor` checks mounts and
   grants. The rest of `view` moved into phase 2: the screens had to run over
   the registry as soon as the project pages went.
5. Projects inside repositories: git with a pathspec; `new-project --in`.
6. Skills, agents, README, CLAUDE.md, `usage.md`. The plugin and the binary go
   to 1.0.0.
7. Migration on this machine, by hand, as above.

## Decisions

| Question | Decision |
|---|---|
| A new project entity, or a vault kind | a vault kind; one engine |
| How a project reaches a knowledge base | a symlink `kb/<name>` to its `wiki/`, ignored by git |
| Where paths live | the atlas config; never the identity file |
| Identity | a UUID per vault |
| Knowledge base inbox | none; sources enter through a project |
| A guarded knowledge base | read-only for unlisted projects, visible to all |
| Block list | none |
| Links between projects | none; `related` is removed |
| Duplicate titles across vaults | `route` answers across the project and its mounts |
| `cs566-course` and `cs566-project` | two projects on one knowledge base |
| The atlas vault | removed; `view` and the CLI over derived state |
| Categories | a `tags` list on the project |
| Migration | by hand; `adopt --as` writes the identity file and drops task scaffolding from a knowledge base |
| A project inside a repository | opt-in, at `REPO/atlas/`, commits with a pathspec |

## Left for later

- `promote`: move a page from a project into a knowledge base and rewrite the
  links.
- A `search` tool across a project and its mounts.
- A skill that grants access from a session.
- `claude-atlas projects --kb NAME`: the projects that mount a knowledge base,
  for a change that runs through all of them.
- The result of the Obsidian follow-symlink check, recorded in "Mounts" above.
- The add screen has no "in repository" option; the CLI's `new-project --in`
  has it.
- The TUI's repository rows carry no host mark, though the CLI's `show` and
  `repos` do.
- The `repos` tool does not mark the host row either. The SessionStart hook
  names the repository, so a session is not left guessing.
- `adopt` on an in-repository project that moved to a different repository
  keeps working, because nothing stores the layout.
