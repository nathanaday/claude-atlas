# Tasks

A task is a page under `wiki/tasks/`. Its status is the truth; the core keeps
the ledger and the index. Read this before any task skill changes a page.

## The page

```yaml
---
type: task
title: "Ingest skips the trust dialog"
status: planted          # planted, planned, active, blocked, done, cancelled
priority: normal         # high, normal, low, someday
created: 2026-09-13
updated: 2026-09-13      # bump on every change to the page
tags:
  - task
task_id: task-20260913-3f2a
due: ""                  # optional date
workdir: ""              # optional: the repository the work happens in
---

# Ingest skips the trust dialog

## Idea
The note as planted, verbatim. Never rewrite it.

## Plan
Written by task-plan.

## Progress
- 2026-09-14 · what was done, decided, or found; what is next.

## Outcome
Written by task-finish.
```

Open tasks (planted, planned, active, blocked) sit in `wiki/tasks/`. Finished
tasks (done, cancelled) sit in `wiki/tasks/archive/`. The core refuses a page
whose folder disagrees with its status, a status or priority outside the
lists, a missing or reused `task_id`, or a malformed date. Add sections as
the task advances; an empty section is a lint finding.

## Changing a task

Every change is one plan of kind `task`, previewed and applied like any other
operation. A `task` plan may write:

| Path | Allowed |
|---|---|
| `wiki/tasks/*.md`, `wiki/tasks/archive/*.md` | create, replace, delete |
| `wiki/hot.md` | replace, when active threads should name the task |
| `inbox/tasks/*` | delete, once planted |

Never `wiki/tasks/index.md` or the task ledger: the core rewrites both from
the pages in the same commit.

To change a status, replace the whole page with the new frontmatter and
`updated` set to today. To finish, delete the page at its open path and
create it under `wiki/tasks/archive/` in the same plan; links by title keep
resolving. To plant, call the `plant` tool; no plan is needed.

## Reading tasks

`tasks` lists them from the ledger: open ones first in board order (active,
blocked, planned, planted; then priority; then age), each with its page,
workdir, last touch, and history. `tasks` with `all` includes the archive.
`status` reports counts, stale tasks, and notes waiting in `inbox/tasks/`.

## Working in a repository

A task's `workdir` is usually a repository the project mounts. Before changing
files there, call `repos` (or read `status`, which names the repository when
the session runs inside one). Each repository says how changes land:

- `pr`: work on a branch, commit there, and open a pull request; never push
  to the default branch.
- `commit`: commit on the current branch.

The policy is the user's choice, kept on the repository's page in the atlas.
Do not change it from a session; say when it gets in the way.

## Freshness

A task lives across sessions. The page is the only memory: write progress
before a session ends, name what is next, and say what blocks. An active task
with no operation for 14 days is stale; `status`, lint, and the atlas say so.
