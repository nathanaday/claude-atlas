# Tasks

Status: proposed. This document sets out how tasks fit the vault, the plugin,
and the atlas. It takes the brainstorm in the personal-projects vault ("Atlas
ideas") as its starting point and says where and why it departs from it.

## What a task must do

- Carry an idea from a passing thought to done, across many sessions, without
  losing state between them.
- Cost nothing to plant. Most tasks start as "this is an open end"; the
  ceremony comes later, if ever.
- Show up where the user looks: the vault's own pages in Obsidian, the session
  that starts in the vault, and the atlas across every vault.
- Follow the vault's rules: one operation, one commit; the core owns what it
  can derive; the model writes pages the user reviews.

## A task is a page

`wiki/tasks/<Title>.md`, with the frontmatter every wiki page carries plus
what a task needs:

```yaml
---
type: task
title: "Ingest skips the trust dialog"
status: planted          # planted, planned, active, blocked, done, cancelled
priority: normal         # high, normal, low, someday; the atlas vocabulary
created: 2026-09-13
updated: 2026-09-13
tags:
  - task
task_id: task-20260913-3f2a
due: ""                  # optional date
workdir: ""              # optional: the folder the work happens in, usually a linked repo
---

## Idea
The note as planted, verbatim.

## Plan
Written by task-plan. Steps, the skill or approach chosen, what done looks like.

## Progress
- 2026-09-14 · first pass done; the dialog still shows on resume.

## Outcome
Written by task-finish: what changed, what was learned, what was left.
```

Open tasks (planted, planned, active, blocked) sit in `wiki/tasks/`. Finished
ones (done, cancelled) sit in `wiki/tasks/archive/`. The status is the truth
and the folder follows it: the core refuses a plan whose task page has a
terminal status outside `archive/`, or a live status inside it. Links to a
task resolve by its title in Obsidian and in lint, so the move on finish
breaks nothing.

The brainstorm proposed `pending/` and `archive/`. Open tasks live directly in
`wiki/tasks/`; a `pending/` folder would add a level every link and path has
to carry for no gain.

## The ledger and the index are derived

`wiki/meta/ledgers/task-ledger.json` is written by the core after every
operation that touches `wiki/tasks/`, the way the source ledger is written
after a capture. It holds, per task: id, path, title, status, priority, due,
created, updated, and the operations that touched it, with their dates and
summaries, read from git. The model never writes it; a plan that names it is
refused. The atlas reads it to see every vault's tasks without parsing pages.

`wiki/tasks/index.md` is written by the core from the ledger in the same
commit: open tasks first, sorted by status (active, blocked, planned,
planted), then priority, then age; the archive below. It is the triage page
the brainstorm called `triage.md`, and it stays correct because nobody
maintains it. Lint treats it as an index, so task pages never count as
unindexed. Triage order itself is the task pages' `priority` and `due`; a
separate ranking field is not needed until it is.

## What keeps a task fresh across sessions

- The session-start hook lists the open tasks after the hot cache: active
  ones first with their last touch, then blocked, planned, and planted, and
  how many notes wait in `inbox/tasks/`. Bounded like the hot cache.
- `status` reports the same counts and names tasks that look stale: active
  with no operation for 14 days.
- `task-run` writes progress into the page before the session ends and at
  every natural stopping point, as an operation, so the next session reads
  where the last one stopped. The ledger's history shows the gap.
- Lint gains `task_errors`: bad status, folder and status disagreeing,
  a planned or active task with an empty plan, a stale active task.
- The atlas refresh reads every ledger and signals blocked and stale tasks on
  the overview, next to the project's heat.

## Planting

Two doors, one result: a page with status `planted`.

1. A note in `inbox/tasks/`. The user drops a file there, from the terminal,
   from Obsidian, or through `claude-atlas plant`. The `inbox` tool and
   `status` report it as a task note, not a source. The `task-plant` skill
   turns each note into a task page and removes the note in the same plan;
   the wiki-ingest skill leaves `inbox/tasks/` alone.
2. A sentence in a session: "add a task: ...". The `task-plant` skill calls
   the `plant` tool with a title and the text.

`plant` is a core tool: it creates the page from the skeleton, gives it an id,
updates the ledger and the index, deletes the inbox note when given one, and
commits, as one operation of kind `task`. No plan preview is needed for a
plant; the page is the user's own words, and undo covers it. `claude-atlas
plant NAME "text"` and the `p` key in the atlas call the same function, so a
task can be planted from anywhere without a session.

Task notes are not sources: they are not captured into `.raw/`. The page's
Idea section keeps the text verbatim.

`ideas/` is added to the vault template as the user's scratch space, outside
the wiki and its rules. It is committed with the vault like everything else.
Nothing reads it; a note there becomes a source by moving it to `inbox/`, or a
task by moving it to `inbox/tasks/`.

## The task kind

A new operation kind, `task`, bounds what the ceremony skills may write:

| Path | Allowed |
|---|---|
| `wiki/tasks/**/*.md` except `index.md` | create, replace, delete |
| `wiki/hot.md` | replace, so active threads can name the task |
| `inbox/tasks/**` | delete, once planted |

The core validates a task page's frontmatter: `type: task`, a status from
the list, a priority from the atlas vocabulary, `task_id` present and unique,
dates well-formed, and the folder matching the status. It rebuilds the ledger
and the index after apply. `route` accepts `type: task` and returns the
skeleton above with a fresh id.

## The skills

The brainstorm names six skills. Five cover them; kill is an outcome of
finish, and view is the router.

| Skill | Does | Status after |
|---|---|---|
| `task` | Orient: list tasks from the ledger, pick one, show its page, move it between statuses without ceremony (block, unblock, reprioritize), and route to the skill below that fits. | as chosen |
| `task-plant` | Turn a sentence or the notes in `inbox/tasks/` into task pages through `plant`. No questions. | planted |
| `task-plan` | The ceremony. Read the page and the vault context. Ask the questions whose answers change the plan. Decide the approach: a skill that fits (offered, never assumed), or a direct plan with steps and a definition of done. Write the Plan section. | planned |
| `task-run` | Needs a plan. Set the status, execute the plan, and write progress at every stopping point. Stop when the plan says done and hand to finish, or when blocked and say on what. | active, blocked |
| `task-finish` | Close: done with an outcome, or cancelled with a reason. Move the page to `archive/`. Refresh the hot cache. | done, cancelled |

Each skill is one plan of kind `task` per change, previewed and applied like
every other operation. `wiki` routes task requests to `task`.

## Where the work happens

A task in a knowledge vault is often work in a linked repository. A session
started in the vault can read and write anywhere the user allows, but a
coding task is better run from the repo. The task page's `workdir` names the
folder; `claude-atlas task run ID` starts Claude Code there with the vault
selected through `CLAUDE_ATLAS_VAULT`, so the session has the repo as its
project and the vault's tools at hand, with `/claude-atlas:task-run ID` as its
first message. The atlas knows the project's linked repos, so the `t` screen
can offer them when a task has no workdir yet.

## The atlas side

The atlas reads ledgers; it never writes into a vault.

- `refresh` adds to a project's state: open task counts by status, the top
  open tasks, the number of waiting notes, and stale or blocked tasks.
- Overview: a Tasks column in the table, a cross-vault "Tasks" section that
  lists every open task by priority with its project, and signals for blocked
  and stale tasks. Task lines link with `obsidian://open?path=` so a click
  opens the task in its own vault.
- `view`: `t` on a project opens its tasks as boxes (title, status, priority,
  age, workdir), with `p` plant, `Enter` to open the page in Obsidian, and
  `c` to start Claude Code on the task. `T` from the tree shows the open tasks
  of every project in one list, the same keys.
- Commands: `claude-atlas tasks [NAME]`, `claude-atlas plant NAME "text"`,
  `claude-atlas task run NAME ID`, mirroring the keys.

Planting from another vault's session ("add a task to cs566-project") needs
the server to know the atlas's projects. A read-only `vaults` tool lists them
from the atlas config when it exists. The vault still learns nothing about the
atlas; the plugin does, the way the CLI already does.

## Phases

1. Core: the task page rules, the `task` kind, the ledger, the generated
   index, `plant`, `tasks`, and `route type=task`; task lines in `status` and
   the session hook; `inbox/tasks/` and `ideas/` in the template and in adopt;
   lint's `task_errors`; `claude-atlas plant` and `tasks`.
2. Skills: `task`, `task-plant`, `task-plan`, `task-run`, `task-finish`; the
   router and the ingest skill updated.
3. Atlas: refresh, overview, the `t` and `T` screens, `task run`.

## Open questions

- Execution in a repo: is starting the session in the repo with the vault
  selected the right shape for most tasks, or are most tasks vault work?
- Stale after 14 days: right threshold?
- Cross-vault planting through a `vaults` tool: wanted in the first cut?
