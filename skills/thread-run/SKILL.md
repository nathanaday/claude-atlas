---
name: thread-run
description: "Do the work of a thread and keep its plan document current: work the plan in slices, commit along the way, test, write progress at every stopping point, and hand off when done or blocked. Use for run this thread, work on, implement, continue, resume, pick up where we left off, next step."
---

# Run a thread

Read [threads.md](../wiki/references/threads.md). Tools: `threads`, `thread`.

The work happens in the work folder, the parent of `atlas/`, with the
ordinary tools. The plan document is the memory between sessions: keep it
true.

## Start or resume

Call `threads`. Take the thread named, else the one with a plan that was
updated last. Read its spec and its plan, and the last lines under
`## Progress`. Say what you are picking up. A thread with no plan goes to
`thread-plan`, unless the work is small enough to need none: then file a
plan of a few lines and go on.

## How to work

- **Own the outcome.** The spec is the intent and the plan is the approach;
  neither is a cage. When the work shows a better way to reach the intent,
  in the code, the interface, or the look and feel, take it, and record the
  change under Progress. When the intent itself should change, stop and ask.
- **Slices and commits.** Work in the slices the plan names. Commit each
  one when it works, the way the work's CLAUDE.md says. `atlas/<name>/` is
  tracked like any other folder, so a commit may carry the plan's progress
  with the code. Use a branch when the repository's habits call for one.
- **Tests.** Behavior that can break gets a test, written with the code or
  before it. Match the project's own standard; do not test what cannot
  fail. Run the tests before every commit. A thread closes only when they
  pass.
- **Subagents.** Use them where the work splits into independent slices or
  needs a wide search. Give each one the spec, the plan, and the reason for
  its slice, and let it own the slice end to end, tests included. A
  subagent that sees one small step decides for that step alone. Review
  what comes back before you build on it.
- **The knowledge base** changes only through `plan` and `apply`. A decision
  the user should find later is a `save`.

## Write progress

At every stopping point (a slice done, a decision, a surprise, a block, the
end of the session) add a dated line under `## Progress` in the plan
document with Edit: what was done, what was decided or found, what is next.
Add the heading the first time. The Edit marks the thread as updated.
Always write where you stopped before the session ends.

## Hand off

- Blocked: write what it waits on under Progress, then `thread` with `id`
  and `blocked`.
- The work is done and the tests pass: hand to `thread-receipt`. Do not
  close the thread here.
