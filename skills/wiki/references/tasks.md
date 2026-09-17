# Tasks and phases

A task is a page under a project's `atlas/tasks/`. Its status is the truth;
the core renders `atlas/tasks/tasks.md` from the pages. There is no engine on
the project side: the frontmatter changes through the `plant`, `task`, and
`phase` tools, and the prose changes with Edit. Read this before any task
skill changes a page.

## The task page

```yaml
---
type: task
title: "Filter vehicle false alarms"
status: planted          # planted, planned, active, blocked, done, cancelled
priority: normal         # high, normal, low, someday
phase: "Alarm quality"   # the title of a phase page, or ""
due: ""                  # optional date
created: 2026-09-17
updated: 2026-09-17      # the tools set it on every change
task_id: task-20260917-3f2a
---

# Filter vehicle false alarms

## Idea
The note as planted, verbatim. Never rewrite it.

## Plan
Written by task-plan, with Edit.

## Progress
- 2026-09-18 · what was done, decided, or found; what is next.

## Outcome
Written by task-finish, with Edit.
```

Open tasks (planted, planned, active, blocked) sit in `tasks/`. Finished
tasks (done, cancelled) sit in `tasks/archive/`. The `task` tool moves the
page when the status crosses that line. `tasks` reports a page whose folder
disagrees with its status, a status or priority outside the lists, a missing
or reused `task_id`, a malformed date, or a phase with no page; setting the
status again with `task` repairs the folder.

## The phase page

```yaml
---
type: phase
title: "Alarm quality"
order: 1
created: 2026-09-17
updated: 2026-09-17
---

# Alarm quality

## Goal
False alarms from vehicles and sun reflection under 1 per camera-day, without
losing the true-positive rate measured in the July field test.
```

A phase holds prose and an order, nothing generated. Which tasks it holds is
derived from the tasks' `phase` fields and shown in `tasks.md`; a phase never
lists its tasks. A phase has no status: it is finished when every task in it
is finished and it holds at least one. A task's `phase` must name a phase
page that exists.

## Changing a task

| Change | How |
|---|---|
| Plant | `plant` with `title`, `text`, and optionally `priority`, `phase`, `due`, `from` (an inbox note to remove), `plan`, `start` |
| Status, priority, phase, due | `task` with `id` and the fields; `id` is the task id or its title |
| Plan, Progress, Outcome | Edit on the page; then `task` with `id` alone to set `updated` |
| Finish | `task` with `status: done` or `cancelled`; the page moves to `tasks/archive/` |
| A phase | `phase` with `action` create, rename, reorder, or remove |

`plant` with `plan` writes the Plan section and the task is `planned`; with
`start` as well it is `active` with a first Progress line, which is how
`work` records a change in one call. `start` without `plan` is refused.

Never write `atlas/tasks/tasks.md`; the hook refuses it, and the tools
regenerate it after every change. The user may edit any page by hand in any
editor.

## From a knowledge base session

`plant`, `tasks`, `task`, and `phase` take `project`, the name of a project
that uses the knowledge base. `tasks` with no `project` boards every one.
The page's absolute path in the result is how a session reads it from there.

## Freshness

A task lives across sessions. The page is the only memory: write progress
before a session ends, name what is next, and say what blocks. An active task
whose `updated` is 14 days old is stale; `status`, the hook, and the atlas say
so.

## The knowledge base

A planted task never reaches the knowledge base. A finished one may:
`task-finish` offers one operation there, an update to the project's page or
a concept page, as a plan the user sees. The knowledge base is evidence for
planning and review; the task pages are the project's own state.
