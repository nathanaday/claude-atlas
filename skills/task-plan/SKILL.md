---
name: task-plan
description: "Plan a task: read it, ask what changes the plan, choose an approach or a skill, write the Plan section with a definition of done, and set the status to planned. Use for plan this task, groom, break it down, how should we do this, ready this task."
---

# Plan a task

Read [tasks.md](../wiki/references/tasks.md). Tools: `status`, `tasks`,
`plan`, `apply`.

## Understand

1. Call `tasks` and pick the task the user means; a planted one unless told
   otherwise. Read its page with Read.
2. Read `wiki/hot.md` and the pages the task touches, at most five. Use Grep
   under `wiki/` for prior decisions, sources, and open questions on the same
   subject.
3. Ask the questions whose answers change the plan, and only those: what
   done looks like, where the work happens, what must not change, what is
   already decided. One round.

## Choose the approach

- If an installed skill fits the work, name it and say why. Offer it; never
  assume it. If the user takes it, the plan says so and the skill's own
  procedure governs the run.
- Otherwise plan directly: the steps in order, each small enough to finish
  in one sitting, with what each produces.
- Say where the work happens. Deliverables, code or a paper or a deck, go in
  a repository the project mounts: set `workdir` to it. If the project has
  none yet, say so; `claude-atlas new-repo` creates one. Vault work has no
  workdir.

## Write the plan

One plan of kind `task` that replaces the page:

- `status: planned`, `updated` set to today, `workdir` and `due` when known,
  `priority` when the user changed it;
- a `## Plan` section: the approach in one paragraph, the steps, what done
  looks like, and the risks or unknowns that could change the plan;
- the Idea section untouched.

Show the preview and apply. Do not start the work; that is `task-run`. If
the user wants to start now, hand off to it after the apply.
