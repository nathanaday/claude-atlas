---
name: task
description: "Orient in a project's tasks and route: list what is open by phase, show one task, move it between statuses without ceremony, create or change a phase, review the whole list for what is misguided or missing, or hand off to task-plant, task-plan, task-run, and task-finish. Use for tasks, todo, what is open, task board, block or unblock a task, reprioritize, set a due date, my tasks, phases, review my tasks, what should I work on."
---

# Tasks

Read [tasks.md](../wiki/references/tasks.md). Tools: `status`, `tasks`,
`task`, `phase`, `plant`.

Tasks live in a project. In a project session the tools act on it. In a
knowledge base session, pass `project` with the name of one of the projects
the hook's `Projects:` line lists; with no name, `tasks` boards every one of
them.

## See what is open

1. Call `tasks`. It returns the board: the phases in order, the open tasks
   with their status, priority, phase, due date, and last update, the
   archive, the notes waiting in the project's inbox, and any page it could
   not read.
2. Show a compact table grouped by phase in phase order, then the tasks with
   no phase, each group in board order (active, blocked, planned, planted;
   then priority; then age). Mark `stale` where it applies. Name the notes
   waiting.
3. Say what a page the tool could not read needs; the user fixes it by hand,
   or `task` sets its status again to move it.

## Route

| The user wants | Skill |
|---|---|
| Make a change now: do this, implement, fix this | `work` |
| Add an idea, or turn the notes in the inbox into tasks | `task-plant` |
| Decide how to do a task | `task-plan` |
| Work on a task, continue one, resume | `task-run` |
| Close a task as done or cancelled | `task-finish` |

Offer the next step for the task in view: a planted task wants a plan, or
`work` when the user wants it done now; a planned one wants a run; an active
one wants to continue; a stale one wants a decision.

## Quick moves

Block, unblock, reprioritize, set a due date, or move a task to a phase
without ceremony: one `task` call with `id` (a task id or the title) and the
fields that change. When blocking, first add a dated line under `## Progress`
with Edit saying what it waits on, then set the status. The tool sets
`updated` and regenerates the index.

Never mark a task done here; that is `task-finish`.

## Phases

A phase is a named slice of the timeline with an order and a goal. Tasks name
their phase; a phase never lists its tasks. `phase` with

- `action: create`, `title`, `goal`, and `order` (the next one when omitted)
  writes the page;
- `action: rename`, `title`, `new_title` retitles it and every task that
  names it;
- `action: reorder`, `title`, `order` moves it;
- `action: remove`, `title` deletes it; the tool refuses while a task still
  names it.

The goal is prose on the phase page; refine it with Edit when the user says
more. When the user wants to plan an effort, "let's plan phase one", read the
phase's goal and every task in it, then hand each task to `task-plan` in
order.

## Review

When the user asks what to work on, whether the list makes sense, or what is
missing:

1. Read every open task's page and the phase goals. Read `wiki/hot.md` and
   the project's page in the knowledge base (`status` names it), and Grep the
   knowledge base for the subjects the tasks name, at most five pages.
2. Say, in the user's terms: which tasks look misguided (a task that would
   not reach its phase's goal, or that solves a symptom the knowledge base
   traces to a cause elsewhere), which are duplicates of each other or of
   finished work in the archive, which matter most and why, and what is
   missing between the tasks and the goals.
3. Offer the changes as a list: reprioritize, merge, cancel, move to a phase,
   or plant a new task. Do nothing until the user picks. Then make each
   change with `task`, `plant`, or `task-finish`, and report.

The review reads the knowledge base as evidence and never writes it. A
durable fact the review surfaces is a `save`, offered.
