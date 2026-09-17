---
name: task-finish
description: "Finish a task: done with an outcome, or cancelled with a reason; move its page to the archive, refresh the hot cache, and bring the pages describing the repositories it changed up to date. Use for finish, done with the task, close, cancel, kill, abandon, drop this task."
---

# Finish a task

Read [tasks.md](../wiki/references/tasks.md). Tools: `tasks`, `repos`,
`plan`, `apply`, and `plant` for what is left over.

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

## The repository pages

A change that landed makes the knowledge base older. After the Outcome is
applied, call `repos` and take each repository the task's `repos` names:

- `described` with `behind` above zero: offer to bring the page up to date,
  and on a yes run the `repo-map` skill's update for it. It stages the
  snapshot, which carries the log since the page's commit, captures it, and
  replaces the repository page with the new `commit`, what changed, and the
  concepts the task added. One offer per repository; the user's no is final
  for this task.
- no `described`: say the repository has no page and offer `repo-map`.
- `behind` at zero: nothing to do; say so in one line.

A task that changed no repository skips this section.

## What is left

- Work left undone is a new task: offer to `plant` each item, and do so when
  the user agrees.
- A lesson or a decision that belongs in the wiki is a `save`, offered, not
  assumed.

Never rewrite the Idea, Plan, or Progress sections when finishing; they are
the record of how the task went.
