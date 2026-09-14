---
name: task
description: "Orient in the vault's tasks and route: list what is open, show one task, move it between statuses without ceremony, or hand off to task-plant, task-plan, task-run, and task-finish. Use for tasks, todo, what is open, task board, block or unblock a task, reprioritize, set a due date, my tasks."
---

# Tasks

Read [tasks.md](../wiki/references/tasks.md). Tools: `status`, `tasks`, `plan`,
`apply` on the atlas MCP server.

## See what is open

1. Call `status` for the counts, stale tasks, and notes waiting in
   `inbox/tasks/`. Call `tasks` for the list.
2. Show a compact table: status, priority, title, id, last touch, workdir, and
   `stale` where it applies. Group by status in board order. Name the notes
   waiting in `inbox/tasks/`.
3. If the user means another project's vault, pass its path as `vault`;
   `claude-atlas list` prints every project and its path.

## Route

| The user wants | Skill |
|---|---|
| Add an idea, or turn the notes in `inbox/tasks/` into tasks | `task-plant` |
| Decide how to do a task | `task-plan` |
| Work on a task, continue one, resume | `task-run` |
| Close a task as done or cancelled | `task-finish` |

Offer the next step for the task in view: a planted task wants a plan; a
planned one wants a run; an active one wants to continue; a stale one wants
a decision.

## Quick moves

Block, unblock, reprioritize, set a due date, or set the workdir without
ceremony: one plan of kind `task` that replaces the page with the changed
frontmatter and `updated` set to today. When blocking, add a dated line under
`## Progress` saying what it waits on. Show the preview and apply. Read the
page first; a replace needs its full content.

Never mark a task done here; that is `task-finish`, which moves the page.
