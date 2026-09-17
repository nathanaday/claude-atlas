---
name: task-plan
description: "Plan a task: read it, ask what changes the plan, choose an approach or a skill, write the Plan section with a definition of done, and set the status to planned. Use for plan this task, groom, break it down, how should we do this, ready this task."
---

# Plan a task

Read [tasks.md](../wiki/references/tasks.md). Tools: `status`, `tasks`,
`task`. The plan is prose on the task page, written with Edit.

Tasks live in a project. In a knowledge base session, pass `project` to
`tasks` and `task`, and read the task page by the absolute path `tasks`
returns.

## Understand

1. Call `tasks` and pick the task the user means; a planted one unless told
   otherwise. Read its page with Read.
2. Read the phase's goal when the task names one. Read the project's page in
   the knowledge base (`status` names it) and Grep the knowledge base for the
   subject, at most five pages: prior decisions, sources, and known problems
   on the same subject. Read the work's CLAUDE.md when the work is a
   repository.
3. Ask the questions whose answers change the plan, and only those: what
   done looks like, what must not change, what is already decided. When the
   project has phases and the task names none, ask which phase, offering the
   open ones in order. One round.

## Choose the approach

- If an installed skill fits the work, name it and say why. Offer it; never
  assume it. If the user takes it, the plan says so and the skill's own
  procedure governs the run.
- Otherwise plan directly: the steps in order, each small enough to finish
  in one sitting, with what each produces.
- The work happens in the project's own folder, the parent of `atlas/`: the
  code, the paper, the deck. A step that changes the knowledge base says so
  and goes through `plan` and `apply` when it runs.

## Write the plan

1. With Edit on the task page, write the `## Plan` section: the approach in
   one paragraph, the steps, what done looks like, and the risks or unknowns
   that could change the plan. Leave the Idea section untouched.
2. Call `task` with `id`, `status: planned`, and `phase`, `priority`, or
   `due` when the answers set them. The tool sets `updated` and regenerates
   the index.

Do not start the work; that is `task-run`. If the user wants to start now,
hand off to it.
