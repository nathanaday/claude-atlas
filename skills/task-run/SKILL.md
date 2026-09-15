---
name: task-run
description: "Execute a planned task and keep its page current: set it active, work the plan, write progress at every stopping point, and hand off when done or blocked. Use for run this task, work on, continue, resume, pick up where we left off, next step on the task."
---

# Run a task

Read [tasks.md](../wiki/references/tasks.md). Tools: `status`, `tasks`,
`plan`, `apply`. The task page is the only memory between sessions; keep it
true.

## Start or resume

1. Call `tasks`. Take the task named, else the active one whose workdir is
   this folder, else the active one touched last. Read its page.
2. A task with no Plan section is not ready: hand off to `task-plan`.
3. If the status is planned, set it active: one plan of kind `task` replacing
   the page with `status: active`, `updated` today, and a first line under
   `## Progress` saying what you are starting. Apply before working.
4. If it is active, read the last Progress line first and continue from
   there. Say what you are picking up.

## Work

Follow the plan. If the plan names a skill, run that skill; its procedure
governs, and this page records the outcome. In a repository, files change
with the ordinary tools, and the repository's change policy from `repos`
decides how they land: a branch and a pull request, or commits on the
current branch. The vault's `wiki/` changes only through `plan` and `apply`. Keep a decision the user should be able to find later with the
`save` skill.

## Write progress

At every stopping point, one plan of kind `task` that replaces the page with
a dated line under `## Progress`: what was done, what was decided or found,
what is next. Stopping points are a step finished, a decision made, a
surprise, a block, and the end of a session. Before the session ends, always
write progress, even a line that says where you stopped.

When `wiki/hot.md` should name the task among its active threads, replace it
in the same plan, under 500 words.

## Hand off

- Blocked: set `status: blocked`, write what it waits on in Progress, apply,
  and tell the user.
- The plan is done: do not mark it done here. Hand off to `task-finish`.
