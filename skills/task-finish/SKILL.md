---
name: task-finish
description: "Finish a task: done with an outcome, or cancelled with a reason; move its page to the archive and refresh the hot cache. Use for finish, done with the task, close, cancel, kill, abandon, drop this task."
---

# Finish a task

Read [tasks.md](../wiki/references/tasks.md). Tools: `tasks`, `plan`,
`apply`, and `plant` for what is left over.

Tasks live in a project. In a knowledge base session this skill does not
apply; the hook's first line names the projects that mount it.

## Close it

1. Call `tasks` and take the task the user means. Read its page.
2. Decide the status with the user: `done` when the plan's definition of done
   holds; `cancelled` when the work stops for good. Ask when it is unclear.
3. Write the `## Outcome` section: what changed and where (repository,
   pages, files); what was learned that belongs in the wiki; what was left
   undone. For a cancelled task, the reason.
4. One plan of kind `task`: delete the page at its open path, and create it
   under `wiki/tasks/archive/` with the same name, the new status, `updated`
   today, and the Outcome section. If `wiki/hot.md` names the task, replace
   it in the same plan. Show the preview and apply.

## What is left

- Work left undone is a new task: offer to `plant` each item, and do so when
  the user agrees.
- A lesson or a decision that belongs in the wiki is a `save`, offered, not
  assumed.

Never rewrite the Idea, Plan, or Progress sections when finishing; they are
the record of how the task went.
