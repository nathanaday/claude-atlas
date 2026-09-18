---
name: thread
description: "Orient in a project's threads and route: show the board by stage, show one thread end to end, reprioritize, block or unblock, rename, move to a phase, create or change a phase, review the whole board for what is misguided or missing, or hand off to the stage skills. Use for threads, board, what is open, what should I work on, todo, tasks, my threads, block, unblock, reprioritize, phases, review the board."
---

# Threads

Read [threads.md](../wiki/references/threads.md). Tools: `status`, `threads`,
`thread`, `phase`.

A thread is one line of work. It moves stub, spec, plan, receipt, and each
stage is a document the user can open. In a knowledge base session pass
`project`; with no name, `threads` lists every project that uses it.

## Show the board

Call `threads`. Show the open threads under their stage, plan first, with
priority, phase, last update, and `blocked` or `stale` where it applies. Name
the notes that wait in the inbox and any page the tool could not read, with
what the page needs. Give the path of `threads/threads.md`: it is the same
board in Obsidian.

For one thread, call `threads` with `id` and read the documents it lists.

## Route

| The thread needs | Skill |
|---|---|
| To exist: an idea, a bug, a chore, a note in the inbox | `thread-stub` |
| A definition: what done means | `thread-spec` |
| An approach | `thread-plan` |
| The work, or the next step of it | `thread-run` |
| An end: completed or killed | `thread-receipt` |
| All of it now, from one sentence | `work` |

Offer the next stage for the thread in view. Size the ceremony to the
thread: a small one skips the spec, and a trivial one goes from stub to
receipt. Say which stages you skip and why.

## Quick moves

Priority, phase, title, block, unblock: one `thread` call with `id` and the
field. `blocked` holds what the thread waits on in one line; `""` unblocks.
Never close a thread here; a receipt needs its text, and `thread-receipt`
writes it.

## Phases

`phase` with `action: create` (`title`, `goal`, optional `order`), `rename`
(`title`, `new_title`; every thread that names it follows), `reorder`, or
`remove` (refused while a thread names it). The goal is prose on the phase
page; refine it with Edit.

## Review

When the user asks what to work on, or whether the board makes sense: read
the open threads' documents and the phase goals, and the knowledge base for
the subjects they name (`wiki/hot.md`, the project's page, a Grep). Then say
what you see: threads that would not reach their phase's goal or that treat
a symptom, duplicates of each other or of closed threads, what matters most
and why, and what is missing. Offer the changes as a list and make the ones
the user picks. The review reads the knowledge base and never writes it.
