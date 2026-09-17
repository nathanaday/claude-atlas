---
name: task-run
description: "Execute a planned task and keep its page current: set it active, work the plan, write progress at every stopping point, and hand off when done or blocked. Use for run this task, work on, continue, resume, pick up where we left off, next step on the task."
---

# Run a task

Read [tasks.md](../wiki/references/tasks.md). Tools: `status`, `tasks`,
`task`. The task page is the only memory between sessions; keep it true.
Progress is prose on the page, written with Edit; the status changes through
`task`.

Tasks live in a project. This skill runs in the project session, inside the
work, where the ordinary file tools reach the code and the documents.

## Start or resume

1. Call `tasks`. Take the task named, else the active one updated last, else
   the first planned one. Read its page.
2. A task with no Plan section is not ready: hand off to `task-plan`.
3. If the status is planned: with Edit, add a first line under `## Progress`
   saying what you are starting, then call `task` with `status: active`.
   Do this before working.
4. If it is active, read the last Progress line first and continue from
   there. Say what you are picking up.

## Work

Follow the plan. If the plan names a skill, run that skill; its procedure
governs, and this page records the outcome. The work happens in the
project's folder with the ordinary tools. When the work is a repository, read
its CLAUDE.md first and commit the way it says; a project's `atlas/` folder is
tracked by that repository like any other folder, so a commit may carry the
task page's progress with the code.

The knowledge base changes only through `plan` and `apply`. Keep a decision
the user should be able to find later with the `save` skill. Read the
knowledge base when the plan does not answer; `wiki-query` says how.

### With superpowers

When superpowers is installed and the plan calls for it, `writing-plans` and
`executing-plans` run the steps; their procedure governs the run, this page
records the outcome. Their files go under `docs/superpowers/` of the work
folder. Without superpowers, the Plan section is the spec of record.

## Write progress

At every stopping point, add a dated line under `## Progress` with Edit:
what was done, what was decided or found, what is next. Stopping points are a
step finished, a decision made, a surprise, a block, and the end of a
session. Then call `task` with `id` and no other field, which sets `updated`
to today and regenerates the index. Before the session ends, always write
progress, even a line that says where you stopped.

## Hand off

- Blocked: write what it waits on in Progress, call `task` with
  `status: blocked`, and tell the user.
- The plan is done: do not mark it done here. Hand off to `task-finish`.
