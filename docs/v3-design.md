# Atlas v3: a project is a folder in your work

Status: designed and built 2026-09-17. The plugin and the binary are 2.0.0.

This version replaces the project vault, mounts, clusters, access, grants,
and linked repositories from `v2-design.md`. The knowledge base and its
engine stay as `core-design.md` describes them. `tasks-design.md` still
describes a task page; the parts of it about the vault engine, the ledger,
and repositories are superseded here. Nothing keeps compatibility with a v2
project vault. Knowledge bases need one field removed.

## What changes and why

Use on a few real projects found v2 heavier than the problem.
The brainstorm of 2026-09-17 in this project's `ideas/` folder is the source.

- **Four entities to keep straight.** Knowledge bases, clusters, projects,
  and repositories all behave differently, and a new user cannot tell where
  to start a session. The work was always in one of two places: the code, or
  the knowledge base.
- **Paths that break.** A project vault lives under the vaults directory and
  reaches its code through `repos/` or a path in the config. Moving a
  codebase, which happens, breaks the link or forces a project to move with
  it.
- **Ceremony where none is needed.** A project vault carried the whole engine:
  its own wiki, raw store, ledgers, git, and operations, to hold a handful of
  task pages.
- **Tasks with nowhere to go.** A task page has a priority and a due date, but
  nothing groups tasks or orders them, and the skills never ask about either.

v3 keeps two things and drops the rest:

| Entity | v2 | v3 |
|---|---|---|
| Knowledge base | a vault, no inbox, guarded by grants | a vault with an inbox; the place you open and work from |
| Project | a vault under the vaults directory, mounts, repositories | a folder named `atlas/` inside the work, no git of its own, one knowledge base |
| Cluster, grant, access, mount, linked repository | entities | gone |
| Task | a page in the project vault | a page in `atlas/tasks/`, with a phase |
| Phase | none | a page in `atlas/phases/`; tasks name it |

The knowledge base is now the center. It is the Obsidian vault the user
opens. It holds every durable fact about an ecosystem, one page per project
in that ecosystem, and its own inbox. A session there sees every project that
uses it and can plant work into any of them. A session in a project sees its
tasks and its one knowledge base.

## The knowledge base

Unchanged in layout except for two folders that return:

```text
product-x/
├── .claude-atlas.json
├── .git/  .gitignore  .obsidian/
├── .raw/captured/<sha256>.ext
├── inbox/                              sources to ingest
├── ideas/                              the user's scratch space; nothing reads it
└── wiki/
    ├── index.md  log.md  hot.md  overview.md
    ├── sources/  entities/  concepts/  canvases/
    └── meta/ledgers/source-ledger.json
```

The identity file loses `access` and `grants`:

```json
{
  "schema": "claude-atlas.vault.v3",
  "id": "6f1d2a9c-3b7e-4c1a-9f2d-8e5a1b0c7d44",
  "kind": "knowledge",
  "name": "product-x",
  "mode": "generic",
  "created": "2026-09-14",
  "scope": "The thermal fire-detection product line: cameras, firmware, alarm pipeline, false-alarm sources and mitigations."
}
```

`vault.Upgrade` drops the two fields from a v2 file and raises the schema.
Everything else about the engine stands: one operation, one commit; `plan`
then `apply`; undo; the source ledger; lint; the hot cache; modes.

**Projects in the knowledge base.** A project that uses a knowledge base
should have a page there, `wiki/entities/<project name>.md`, `type: entity`,
which says what the project is, how it is laid out, and what it has
delivered. The `describe` skill writes it from the project's folder, the way
`repo-map` writes a repository page today: a snapshot staged into the inbox,
read, and filed as an entity page with the commit it was written from. A
project that is not a git repository is described from its files without a
commit. The session-start hook in both kinds names the projects the knowledge
base has no page for. Nothing writes the page without a session, because it
is prose.

**Where its history lives.** A knowledge base commits into the git
repository that holds it. Most have their own. One inside a project, for a
project whose knowledge serves nothing else, commits into the project's
repository: every command is scoped to its folder, so an operation, a manual
commit, and an undo touch nothing outside it, and the user's staged and
changed code stays as it was. `init` and `adopt` run `git init` only when no
repository holds the folder, so no repository ever sits inside another. The
knowledge base's history follows the project's branch. `describe` counts
only commits outside the knowledge base when it says how far the work moved.

A knowledge base that no project uses is fine. A project without a knowledge
base is fine too; the task skills work and the query skill says there is
nothing to search.

## The project

Any folder becomes a project by gaining a folder named `atlas/`:

```text
webapp/                                 a repository, or a folder of documents
├── ...                                 the user's work, untouched
└── atlas/
    ├── project.json                    identity
    ├── tasks/
    │   ├── tasks.md                    generated: every task, grouped by phase
    │   ├── archive/                    done and cancelled
    │   └── <Title>.md
    ├── phases/
    │   └── <Title>.md
    └── inbox/                          task notes, dropped by hand
```

- `atlas/` is not a git repository and not an Obsidian vault. When the work
  is a repository, the repository tracks `atlas/` like any other folder, on
  the branch the user is on. When it is not, nothing tracks it. There is no
  operation, no ledger, no undo, no `.raw/`, no wiki, no hot cache, and no
  `kb/` symlink. The engine does not run here.
- `project.json` is visible because the folder is the user's and the file
  says what the folder is. It holds no path.

```json
{
  "schema": "claude-atlas.project.v3",
  "id": "b3e0f5a2-9c14-4d6e-8a7b-2f1e0c9d8b7a",
  "name": "webapp",
  "description": "The customer-facing web application for the fire-detection product.",
  "created": "2026-09-17",
  "knowledge": { "id": "6f1d2a9c-…", "name": "product-x" }
}
```

- `knowledge` names the one knowledge base the project uses, by id; `name` is
  for people and for messages when the id is not found on this machine. It
  may be absent. A project uses at most one knowledge base; a knowledge base
  serves any number of projects.
- `tags` is gone. With projects grouped by knowledge base, the view needs no
  second grouping.

### Creating one

```bash
cd ~/code/webapp
claude-atlas init                                # name: the folder's; asks for a description and a knowledge base
claude-atlas init --name "Web App" --knowledge product-x --description "…"
claude-atlas init --no-knowledge                 # ask nothing
```

`init` writes `atlas/` with its four entries, adds the folder to the atlas
config, runs `git init` when the folder is in no repository (no commit;
`--no-git` skips it), and prints what it did and what comes next: describe the project in
the knowledge base, plant a task. In a terminal with no flags it asks for the
description and offers the knowledge bases the atlas knows, with none as a
choice. It refuses a folder that already has `atlas/` without a
`project.json`, and a folder inside another project or inside a vault.

`link KB` and `unlink` change `knowledge`. `forget` removes the folder from
the config and leaves `atlas/` alone. Deleting `atlas/` is how a project
ends.

## Tasks and phases

### A task page

`atlas/tasks/<Title>.md`, as `tasks-design.md` describes it, with `phase`
added and `workdir`, `repos`, and `tags` removed:

```yaml
---
type: task
title: "Filter vehicle false alarms"
status: planted          # planted, planned, active, blocked, done, cancelled
priority: normal         # high, normal, low, someday
phase: "Alarm quality"   # the title of a phase page, or empty
due: ""
created: 2026-09-17
updated: 2026-09-17
task_id: task-20260917-3f2a
---

## Idea
## Plan
## Progress
## Outcome
```

The sections keep their meaning: Idea is the user's words as planted, Plan is
what task-plan wrote with a definition of done, Progress is dated lines,
Outcome is what task-finish wrote. Open tasks sit in `tasks/`; done and
cancelled ones in `tasks/archive/`. The status is the truth and the folder
follows it.

### A phase page

A phase is a named group of tasks with an order: a slice of the timeline.
`atlas/phases/<Title>.md`:

```yaml
---
type: phase
title: "Alarm quality"
order: 1
created: 2026-09-17
updated: 2026-09-17
---

## Goal
False alarms from vehicles and sun reflection under 1 per camera-day, without
losing the true-positive rate measured in the July field test.
```

- A phase page holds the user's prose and nothing generated. Which tasks it
  holds is derived from the tasks' `phase` fields and shown in `tasks.md`.
  One source of truth: a task names its phase; a phase never lists its
  tasks.
- A phase has no status. It is finished when every task in it is finished and
  it holds at least one. `tasks.md` shows it under "Finished phases" then.
- `order` sorts phases in `tasks.md` and in every listing. Ties sort by title.
- A task's `phase` must name a phase page that exists. The `task` tool refuses
  an unknown one; `phase remove` refuses while a task still names it;
  `phase rename` rewrites every task that names it.

### tasks.md

Generated from the pages after every tool call that touches a task or a
phase, and at session start. The model and the user never write it.

```markdown
# Tasks

## Alarm quality (1)

| Task | Status | Priority | Due | Updated |
| Filter vehicle false alarms | planted | high | — | 2026-09-17 |

## No phase

| Task | Status | Priority | Due | Updated |
| Test a fresh installation | planted | normal | — | 2026-09-17 |

## Finished phases

| Phase | Tasks | Last finished |

## Archive

| Task | Status | Phase | Finished |
```

### Writing on the project side

There is no engine here, so the rules are simpler than a vault's:

- The `plant`, `task`, and `phase` tools create pages and change frontmatter.
  `task` sets status, priority, phase, and due; on `done` or `cancelled` it
  moves the page to `archive/`. Every call validates the page and regenerates
  `tasks.md`.
- The Plan, Progress, and Outcome sections are prose. The model writes them
  with Edit on the task page. The guard allows it. The user may edit any
  page by hand, in any editor, including the frontmatter.
- The guard refuses Write and Edit on `tasks.md` and `project.json` only.
- The `tasks` tool reads every page and reports a page it cannot parse, a
  status and folder that disagree, and a phase that names no page, as
  problems beside the list. It never repairs one; the `task` tool does, on
  request, by setting the status again.

### Planting from anywhere

Three doors, one result: a page with status `planted`.

1. A sentence in a project session: the `task-plant` skill calls `plant`.
2. A note in `atlas/inbox/`: the `task-plant` skill turns each note into a
   page and removes the note.
3. From outside the project: `claude-atlas plant PROJECT "text"`, the `p` key
   in the view, or `plant` with `project` set from a knowledge base session.

`plant` takes `title`, `text`, and optionally `priority`, `phase`, `due`, and
`project`. In a project session `project` is implied. In a knowledge base
session it names any project that uses this knowledge base.

### The knowledge base learns from finished work

A planted task is a half thought and never reaches the knowledge base. A
finished one may. The `task-finish` skill, after writing the Outcome, offers
one operation in the project's knowledge base: an update to the project's
entity page, and, when the outcome is a durable fact about the ecosystem, a
concept page or a change to one. The offer is a `plan` the user sees and an
`apply` that commits, like any other. Declining it costs nothing.

## Sessions

A session finds its place by walking up from the working directory:

1. `CLAUDE_ATLAS_VAULT` or an explicit `vault`, as today.
2. The nearer of the nearest ancestor that holds `atlas/project.json`, a
   project session anywhere inside the work, and the nearest ancestor that
   holds `.claude-atlas.json`, a knowledge base session. A knowledge base
   inside a project is its own session.
3. Otherwise no atlas; the hook prints nothing.

`discover` no longer searches repositories through the registry; the folder
carries its own identity.

**Self-healing registration.** The hook in a project session compares the
project's id with the config. Known at this path: nothing. Known at another
path: the config is rewritten to this path, and the hook says so. Not known,
as after a clone on another machine: the project is added, and the hook says
so. A moved project heals the moment the user works in it, and the view shows
it as missing until then. A knowledge base id the config cannot resolve is
reported, not guessed.

The session-start hook in a project:

```text
claude-atlas: project webapp at ~/code/webapp (git, main)
Knowledge: product-x · The thermal fire-detection product line … · 140 pages · ~/Vaults/product-x
This project has no page in product-x; the describe skill writes it.
Search the knowledge base (the wiki-query skill) before answering from the code alone.
Open tasks: 4 (active 1, blocked 0, planned 1, planted 2) in 2 phases. Notes waiting: 1.
- [active] Filter vehicle false alarms · Alarm quality · last touched 2026-09-17
- …
```

In a knowledge base:

```text
claude-atlas: knowledge base product-x (generic) at ~/Vaults/product-x
Projects: webapp (~/code/webapp, 4 open tasks), firmware (~/code/fw, 0 open tasks), legacy-gateway (missing: ~/old/gateway)
Not yet described here: firmware.
Inbox: 2 sources waiting.
<vault-context> hot.md </vault-context>
```

What each session can do:

| | knowledge base session | project session |
|---|---|---|
| ingest, save, query, lint, fold, repair, stub, mode, canvas | yes, on this vault | on the project's knowledge base, through `vault` |
| plant, tasks, task, phase | on any project of this knowledge base, by name | on this project |
| describe a project | any project of this knowledge base | this project |
| the bird's-eye conversation: what is going on across projects, what to work on next | yes | no |

A project session that ingests writes into the knowledge base directly: the
`capture` record carries `via` with the project's id and name, as today. The
project's own inbox holds task notes only; a document that should become
knowledge goes to the knowledge base's inbox, or the user names its path in
the session and the ingest skill captures it from there.

## The atlas

The atlas keeps its home at `~/.claude-atlas/`, as today: `config.json` and
the derived `state/`. It is the one place on the machine that knows where
everything is. `config.json`, schema `claude-atlas.config.v3`:

```json
{
  "schema": "claude-atlas.config.v3",
  "knowledge": ["~/Vaults/product-x", "~/elsewhere/papers"],
  "projects": ["~/code/webapp", "~/code/fw", "~/Documents/thesis"],
  "plugin": {}, "claude_code": {}, "heat": {}
}
```

- `knowledge` lists every knowledge base's folder. `new-knowledge` and
  `adopt` write the list, `remove` drops from it, and a session started in a
  knowledge base heals it by id, as for a project. A bare name given to
  `new-knowledge` is a folder in the current directory.
- `projects` lists the work folders, the parent of each `atlas/`. `init` and
  the hook write the list; `forget` removes from it.
- The atlas never searches the disk. Both kinds live wherever the user puts
  them, and the config names each one.
- `vaults_dir` is gone, and `relocate` with it. A knowledge base's folder
  moves like a project's: by hand, and the next session in it heals the
  entry. A v3 config that still carries `vaults_dir` loads without it; a
  knowledge base that only the old walk found is registered again with
  `adopt`.
- `repos` and `default_repo_changes` are gone.
- `state/registry.json` is derived: every knowledge base with its projects,
  page counts, heat, and inbox count; every project with its path, its
  knowledge base resolved, its open task counts by status, its phases, and
  whether the path exists. `refresh` rebuilds it. Every command that acts on
  a vault or a project resolves afresh.

## Tools

| Tool | Session | Change |
|---|---|---|
| `status` | both | a project's knowledge base and its task counts; a knowledge base's projects |
| `inbox` | both | a knowledge base's sources; a project's task notes |
| `capture`, `plan`, `apply`, `undo`, `history`, `route`, `lint`, `stub`, `mode` | both; the target is always a knowledge base | no access check |
| `stage` | both | stages files, or a project's snapshot, into a knowledge base inbox |
| `plant`, `tasks` | both | `project` names the target from a knowledge base session; `phase` on plant |
| `task` | both | new: set status, priority, phase, due; move to archive on finish |
| `phase` | both | new: create, rename, reorder, remove |
| `atlas` | both | every knowledge base with its projects; `refresh` |
| `vault` | both | create a knowledge base, adopt one, edit scope or mode, forget |
| `project` | both | new: init here, link or unlink a knowledge base, forget |
| `settings` | both | unchanged |
| `mount`, `mounts`, `cluster`, `repo`, `repos` | | removed |

Operation kinds lose the per-kind gate: `task` is no longer an operation
kind, and every kind runs in a knowledge base. `txn.allowed` reserves
`inbox/` except deletes in an ingest, `ideas/`, and the paths it reserves
today.

## The CLI

| Command | Does |
|---|---|
| `init [--name] [--description] [--knowledge KB \| --no-knowledge] [--no-git]` | make the current folder a project, and a git repository when it is in none |
| `link KB`, `unlink` | set or clear the project's knowledge base, from inside it or with `--project NAME` |
| `forget PROJECT` | drop it from the config |
| `describe PROJECT` | stage a snapshot into the knowledge base inbox; the skill writes the page |
| `new-knowledge NAME [--scope] [--mode] [PATH]`, `adopt PATH`, `edit KB --scope --mode`, `remove KB` | knowledge bases, as today minus access |
| `plant PROJECT TEXT [--phase] [--priority] [--due]`, `tasks [PROJECT]`, `task PROJECT ID --status --priority --phase --due`, `phase PROJECT …` | tasks from the terminal |
| `list`, `show NAME`, `refresh`, `doctor`, `config` | the atlas |
| `open-vault KB`, `open-claude NAME [--task ID]`, `view` | launching |
| `ingest KB [PATH…]` | stage files into a knowledge base inbox |

Removed: `new-project`, `mount`, `unmount`, `grant`, `revoke`, `new-cluster`,
`cluster`, `new-repo`, `link` in its repository sense, `unlink` in that
sense, `edit-repo`, `repos`.

## The view

Two tabs, Knowledge and Projects, and no editing. The Knowledge tab lists
every knowledge base with its scope, page count, inbox count, and the
projects that use it. The Projects tab lists every project grouped by
knowledge base, with its path, open task count, current phase, and a mark
when the path is missing. Enter expands a row in place with what `show`
prints. Keys:

| Key | Command |
|---|---|
| Enter | `show NAME` |
| `o` on a knowledge base | `open-vault KB` |
| `c` | `open-claude NAME` |
| `p` on a project | `plant PROJECT TEXT` |
| `R` | `refresh` |
| `←` `→` tabs, `h` keys, `q` quit | |

Removed: the add, adopt, edit, mount, cluster, link, ingest, and task
screens. Creating and changing things is the CLI's and the session's job.
The view is for seeing what exists and getting there.

## Skills

| Skill | Change |
|---|---|
| `atlas` | orients across knowledge bases and their projects; refresh; settings |
| `atlas-knowledge` | create a knowledge base, change its scope; no access, no clusters |
| `atlas-project` | init here, link or unlink a knowledge base, forget; runs `describe` afterwards when the user says yes |
| `atlas-mount`, `atlas-repo` | removed |
| `repo-map` | becomes `describe`: the project's entity page in its knowledge base, from a repository or a folder of documents |
| `work` | in a knowledge base session: name the projects a change touches and plant a task in each with its plan. In a project session: plan and start the task here |
| `task` | lists and routes as today, with phases; gains a review mode: read every open task and the knowledge base, say which tasks look misguided or duplicated, which matter most, and what is missing, then plant what the user picks |
| `task-plant` | `phase` when the user names one; the three doors |
| `task-plan` | asks which phase when the project has phases and the task has none |
| `task-run` | writes Progress with Edit; sets status through `task` |
| `task-finish` | writes Outcome, sets the status through `task`, then offers the knowledge base update |
| `wiki`, `wiki-ingest`, `wiki-query`, `save`, `wiki-lint`, `wiki-fold`, `wiki-mode`, `canvas`, `obsidian-bases`, `obsidian-markdown`, `think` | run in a knowledge base session; ingest, query, and save also from a project session against its knowledge base |
| `references/mounts.md` | removed |

The `wiki-ingest` agent's brief carries the knowledge base's path and, from a
project session, the project's name for `via`.

## Rules

The three vault rules stand for a knowledge base: one operation, one commit;
the vault is the user's; code owns what it can derive. v3 replaces the v2
atlas rules with:

4. Two kinds, one engine, and the engine runs only in a knowledge base. A
   project is files.
5. A project uses one knowledge base. A knowledge base serves many projects
   and never records which; the atlas computes that from the projects.
6. Projects never link each other. They share a knowledge base.
7. Ids travel; paths stay. `project.json` and the identity file hold no
   path. The config holds every path, and a project heals its own entry when
   a session starts in it.
8. Knowledge lives in the knowledge base. A project holds tasks and phases
   and nothing else the atlas reads.
9. A task names its phase; a phase never lists its tasks. `tasks.md` is
   generated.
10. Nothing reaches the knowledge base from a task until the task is finished,
    and then only through a plan the user sees.

## Build order

1. Engine: the v3 identity file without access and grants; `vault.Upgrade`;
   `inbox/` and `ideas/` in the knowledge template; the operation kinds
   without the per-kind gate; the removal of the project kind, mounts,
   clusters, grants, `kb/`, the pathspec layout, and `discover` through
   repositories. `lint.TestNewVaultHasNoFindings` for the one kind.
2. Project: `project.json`, `init`, `link`, `unlink`, `forget`; the config
   v3 with `projects`; the scan for knowledge bases only; the registry with
   projects; discovery by walking up; the hook's self-healing.
3. Tasks: the page without `workdir`, `repos`, `tags`; phase pages; the
   generated `tasks.md`; `plant`, `task`, `phase`, `tasks` over plain files
   with no ledger; the guard on `tasks.md` alone; `project` on `plant` and
   `tasks`; the CLI commands.
4. Hooks: the two session-start forms above; `status`.
5. View: two tabs, five keys; delete the other screens.
6. Skills and agents: the table above; `describe`; README, CLAUDE.md,
   `usage.md`. Plugin and binary to 2.0.0.
7. On this machine: `init` in each repository that had a project vault,
   `link` to its knowledge base, `describe`. The v2 project vaults are
   deleted by hand; none holds tasks worth moving.

## Decisions

| Question | Decision |
|---|---|
| Where a project lives | `atlas/` inside the work, always; no standalone project vault |
| Git on the project side | none of its own; the host repository tracks `atlas/` if there is one |
| Obsidian on the project side | none; the knowledge base is the vault the user opens |
| The identity file's name and place | `atlas/project.json`, visible |
| Knowledge bases per project | one; many is left for later |
| A knowledge base inside a project | allowed; it commits into the project's repository, or into its own when the project is in none |
| Clusters, access, grants | removed |
| Linked repositories, `repos/`, change policies | removed; the work folder is the repository when it is one |
| Where knowledge enters | the knowledge base's own inbox, and from a project session directly into it |
| A phase | a page with prose and an order; tasks name it; membership is derived |
| A phase's status | none; derived from its tasks |
| The task ledger | removed; `updated` on the page and `tasks.md` carry what it carried |
| How the model edits a task's prose | Edit on the page; frontmatter through `task` |
| When a task reaches the knowledge base | on finish, as an offered operation |
| The project's page in the knowledge base | `wiki/entities/<name>.md`, written by `describe`, refreshed by `task-finish` |
| Registering a project | `init` writes the config; the hook heals a moved or cloned one by id |
| The view | list and launch only |
| Migration | none; recreate the projects with `init` |

## Left for later

- Many knowledge bases on one project, once one is not enough in practice.
- `promote`: a task's Outcome, or a page from a project's folder, into the
  knowledge base with links rewritten.
- A `search` tool across a knowledge base and the tasks of its projects.
- A Tasks tab in the view, if the CLI's `tasks` proves too far away.
- `init --dir NAME` for a repository that already uses a folder named
  `atlas/` for something else.
- A Stop hook line when the session's active task page was not touched.
- Dependencies between tasks, and between phases, beyond `order`.
