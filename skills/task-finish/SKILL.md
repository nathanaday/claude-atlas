---
name: task-finish
description: "Finish a task: done with an outcome, or cancelled with a reason; archive its page, then offer the knowledge base what the work taught. Use for finish, done with the task, close, cancel, kill, abandon, drop this task."
---

# Finish a task

Read [tasks.md](../wiki/references/tasks.md). Tools: `tasks`, `task`,
`plant` for what is left over, and `stage`, `route`, `plan`, `apply` for the
knowledge base.

Tasks live in a project. In a knowledge base session, pass `project` to the
task tools and read the page by the absolute path `tasks` returns.

## Close it

1. Call `tasks` and take the task the user means. Read its page.
2. Decide the status with the user: `done` when the plan's definition of done
   holds; `cancelled` when the work stops for good. Ask when it is unclear.
3. With Edit, write the `## Outcome` section: what changed and where (files,
   commits, pages); what was learned that belongs in the knowledge base;
   what was left undone. For a cancelled task, the reason.
4. Call `task` with `id` and the status. The tool moves the page to
   `tasks/archive/`, sets `updated`, and regenerates the index. Report the
   new path. A phase whose every task is now finished shows as finished in
   the index; say so.

Never rewrite the Idea, Plan, or Progress sections when finishing; they are
the record of how the task went.

## The knowledge base learns from finished work

A planted task never reaches the knowledge base. A finished one may. After
the status is set, and only for a `done` task whose project has a knowledge
base, offer one operation there. Say what it would hold, and wait for a yes:

- **The project's page.** `status` says whether a page describes the project
  and how far behind it is. When one exists, an update: what the project now
  delivers, in the page's own words, with the task's outcome as the source of
  the change. When none exists, offer `describe` instead, which writes it
  from a snapshot.
- **A durable fact.** When the Outcome holds knowledge about the ecosystem,
  not the project alone (a cause found, a mitigation that works, a
  measurement), a concept page or an update to one. `route` says whether a
  page exists. A fact about how this one project works stays on the
  project's page.

On yes: read the pages, build one plan of kind `save` with the page writes,
`wiki/index.md` when a page is new, and `wiki/hot.md` under 500 words. Show
the preview and apply. Read [operations.md](../wiki/references/operations.md)
first. Declining costs nothing; say so in one line and stop.

## What is left

- Work left undone is a new task: offer to `plant` each item, and do so when
  the user agrees, in the same phase.
- A lesson that belongs in the knowledge base but is not the project's page
  or a concept is a `save`, offered, not assumed.
