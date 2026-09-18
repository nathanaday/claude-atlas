# Threads: how a project tracks its state

Status: designed and built 2026-09-17. The plugin and the binary are 3.0.0.

This document replaces the task model of `v3-design.md` (its sections "Tasks
and phases", and the task rows of its tool, CLI, view, and skill tables) and
the page contract of `tasks-design.md`. Everything else in `v3-design.md`
stands: a project is `atlas/<name>/` in the work, it has no git of its own
and no engine, and it uses one knowledge base. Phases stay as they were, with
threads in the place of tasks.

## What changes and why

A task was one page with a status. The list of tasks said what was open, and
little else. A task that was `active` said nothing about whether anyone had
agreed what done means, whether an approach existed, or how the work ended.
The thinking around a task lived in the conversation and was gone when the
session ended.

A project's state is better described by the things that move through it.
A **thread** is one line of work: an issue, a feature, a chore. Threads
appear over time, and each one moves through four stages:

| Stage | The document answers |
|---|---|
| **stub** | What is this about? A short, loose note where the thread begins. |
| **spec** | What will be true when it is done, and why? |
| **plan** | How will the work go? Then: how is it going? |
| **receipt** | How did it end: completed, or killed? |

Each stage is a file in its own folder. The board shows every thread at its
stage. A user who opens `atlas/<name>/` in Obsidian sees the whole state of
the project, with each stage in its own color.

## One rule: the stage is the furthest document

Nothing sets a thread's stage. The stage is the furthest document that
exists for the thread. A thread moves to `spec` when its spec is filed, and
in no other way.

This rule is the design. It gives three things:

1. **The documents are always captured.** A session cannot move a thread
   along in conversation only, because there is nothing else to move. The
   visible file is the state change.
2. **One source of truth.** No status field can disagree with the files.
   `stage` and `outcome` on the card are written by code from the documents,
   for Obsidian to show and for Bases to query.
3. **Hand edits work.** Deleting a receipt in Obsidian reopens the thread.
   Deleting a plan moves the thread back to its spec. The next sync makes
   every generated page agree.

A stage may be skipped. A one-line fix goes from stub to plan, or from stub
to receipt. A skipped document may be filed later; the stage does not move
back. A receipt closes the thread, and a closed thread takes no more
documents until it is reopened.

## Layout

```text
atlas/<name>/
├── project.json
├── threads/
│   ├── threads.md            the board, generated
│   ├── <Title>.md            the card of an open thread, generated
│   └── archive/<Title>.md    the card of a closed thread
├── stubs/<Title>.md
├── specs/<Title>.md
├── plans/<Title>.md
├── receipts/<Title>.md
├── phases/<Title>.md
├── inbox/                    notes that wait to become threads
└── .obsidian/
    ├── snippets/claude-atlas.css   one callout color and icon per stage
    ├── appearance.json             enables the snippet
    └── .gitignore                  workspace*.json
```

- Every page of a thread has the same file name, the thread's title. The
  folder says which page it is. A second thread with the same title takes
  `Title (2).md` for every page.
- `threads/` holds the open threads. A closed thread's card moves to
  `threads/archive/`. The documents never move, so links to them hold.
- A document finds its thread by the `thread` id in its frontmatter, never
  by its file name, so a page renamed by hand is still found.

### The card

```yaml
---
type: thread
thread_id: thr-20260917-3f2a
title: "Filter vehicle false alarms"
stage: plan              # written by code from the documents
outcome: ""              # written by code from the receipt
priority: normal         # high, normal, low, someday
phase: "Alarm quality"   # the title of a phase page, or ""
blocked: ""              # what an open thread waits on, in one line
created: 2026-09-17
updated: 2026-09-17
---
```

The card's body is generated: a `[!thread]` callout with the state and a
link to each document, then an embed of each document in order. One page
shows the thread from end to end. Code owns the whole card. `setField`
rewrites one frontmatter line at a time, so a property the user adds by hand
survives.

A thread has no due date. A phase carries the timeline.

### A document

```yaml
---
type: spec               # the stage
thread: thr-20260917-3f2a
title: "Filter vehicle false alarms"
created: 2026-09-18
---

> [!spec] Filter vehicle false alarms
> [Stub](<../stubs/…>) → **Spec** → [Plan](<../plans/…>) → Receipt
> `thr-20260917-3f2a` · [Thread](<../threads/…>) · filed 2026-09-18

The prose.
```

A receipt also carries `outcome: completed` or `outcome: killed`, and a
killed receipt uses the `[!killed]` callout.

The first callout is code's. It gives the page its stage's color and icon,
and it is the way to every other document of the thread. Sync rewrites it
when a sibling is filed, the thread is renamed, or the card moves to the
archive. Only a leading callout of type `stub`, `spec`, `plan`, `receipt`,
`killed`, or `phase` is replaced; a callout of any other type is the user's.
Everything under the first callout is prose, which the model writes with
Edit and the user writes in any editor.

Links are relative markdown links, so they open in Obsidian and in any other
viewer. Embeds are Obsidian wikilinks, which other viewers show as text.

### The board

`threads/threads.md` is generated. It lists the open threads under one
callout per stage, the furthest stage first (plan, spec, stub), each row
with a link to the card, a link to the document of the stage, priority,
phase, last update, and `blocked` or `stale`. Then the phases with open and
closed counts, then the closed threads with a link to each receipt, then the
pages that could not be read.

## Writing

| Who | Writes |
|---|---|
| `threads.Start`, `File`, `Set`, `Reopen`, `Touch`, the phase functions | cards, new documents, frontmatter |
| `threads.Sync` | every card's `stage`, `outcome`, body, and folder; every document's first callout; the board |
| Edit, from the model | the prose of a document that exists, and of a phase page |
| The user | anything |

Every write ends in `Sync`. `Sync` writes a file only when its content
differs, never changes `updated`, and is safe to run at any time. The
session-start hook runs it, so hand edits made in Obsidian show on the board
at the next session.

The guard (`PreToolUse`) refuses Write and Edit under `threads/` and on
`project.json`, and refuses a Write that would create a new file in a stage
folder: a new document comes from the `thread` tool, which gives it the
thread's id. The refusal names the call to make.

The `touched` hook (`PostToolUse` on Write and Edit) marks the thread that
owns an edited document as updated today. No skill has to say "now touch the
thread".

`Load` reports a page it cannot use as a problem beside the board, and never
repairs one: a card or a document with bad frontmatter, a document whose
thread does not exist, a second document of one stage, a receipt with no
valid outcome, a card with no documents, a phase that names no page.

## Tools

| Tool | Does |
|---|---|
| `threads` | reads the board of a project: counts by stage, phases, open and closed threads with the absolute path of each document, inbox notes, problems. `id` narrows to one thread. In a knowledge base session with no `project`, every project that uses it. |
| `thread` | without `id`, opens a thread from `text` (the stub). With `id` and `stage`, files that stage's document from `text`; a receipt takes `outcome`. With `id` and `priority`, `phase`, `blocked`, or `title`, changes the card. With `id` alone, marks it touched. `reopen` deletes the receipt. |
| `phase` | as before: create, rename, reorder, remove. |

`plant`, `tasks`, and `task` are gone. Two tools carry every thread action,
because a tool's description sits in every session's context. The `thread`
tool passes the model's text into the document; it writes no prose of its
own.

## The CLI

```bash
claude-atlas threads [PROJECT] [--all] [--stage S] [--json]
claude-atlas thread PROJECT new TEXT... [--title T] [--priority P] [--phase NAME]
claude-atlas thread PROJECT show ID [--json]
claude-atlas thread PROJECT file ID STAGE [--text T | --file PATH] [--outcome completed|killed]
claude-atlas thread PROJECT close ID TEXT... [--killed]
claude-atlas thread PROJECT set ID [--title T] [--priority P] [--phase NAME] [--blocked TEXT]
claude-atlas thread PROJECT reopen ID
claude-atlas open-claude NAME --thread ID
```

`PROJECT` is a name, a path, or `.`. `ID` is a thread's id, its title, or
the start of its title. `file` and `new` read the text from stdin when it is
not a terminal. `open-claude --thread` starts the session with the skill for
the thread's next stage. In the view, `n` on a project opens a thread from
one line.

## Skills

One skill per stage, and one to orient. They are short. Each says what the
stage is for, what the document holds, and which call files it; the rest is
the model's judgment.

| Skill | Does |
|---|---|
| `thread` | the board, one thread, quick card changes, phases, a review of the whole board, routing |
| `thread-stub` | opens a thread from a sentence or an inbox note, in the user's words, with no questions |
| `thread-spec` | reads the stub, the code, and the knowledge base; decides what it can; asks only what it cannot; files the spec |
| `thread-plan` | explores the code with the spec in hand, as in plan mode, and files what it found |
| `thread-run` | works the plan in slices, commits, tests, writes progress in the plan document |
| `thread-receipt` | checks that the tests pass, files the receipt, offers the knowledge base what the work taught |
| `work` | the short road: one sentence to a thread with a plan, then the work |

### What the skills take from superpowers, and what they leave

Taken:

- The ceremony of a piece of work is visible, and every document stays.
- Commit along the way.
- Subagents do real work, where the work splits.
- Behavior that can break gets a test.
- Tests pass before a thread completes or a branch merges.

Left:

- **Quotas.** Superpowers fails where it makes the model hunt for a number
  of things: three questions, three flaws. A model that must ask three
  questions spends its effort on finding questions. `thread-spec` asks what
  reading cannot answer and a wrong guess would change; none is a common
  count. Questions go out together, each with a recommended answer.
- **A full implementation plan with a critical review by subagents.** With
  current models it is a delay and a cost. The plan is what plan mode finds,
  kept as a document, reviewed by the user.
- **Confinement to the spec.** A spec written before the work does not see
  what the work shows, least of all in the interface and how it feels. The
  spec states intent and the experience wanted; `thread-run` tells the model
  to own the outcome, take the better way when the work shows one, and
  record the change under Progress.
- **Very small subagents with very specific steps.** A subagent that sees
  one step decides for that step alone. `thread-run` gives a subagent the
  spec, the plan, and a whole slice to own, tests included.

## Migration

`claude-atlas upgrade [NAME | --all]` turns each task page of 2.x into a
thread (`threads.Migrate`): Idea becomes the stub, Plan and Progress the
plan, Outcome the receipt. `done` becomes `completed`, `cancelled` becomes
`killed`, and a `blocked` task is a blocked thread. `task-20260917-3f2a`
becomes `thr-20260917-3f2a`. A page it cannot read stays in `tasks/` and is
named. The command also writes the CSS snippet. Until it runs, the hook and
`status` say that the project holds task pages. Nothing moves the user's
files without that command.

## Decisions

| Question | Decision |
|---|---|
| Where the stage lives | nowhere; it is the furthest document that exists |
| What keeps a thread's identity | the card under `threads/`, owned by code |
| Closed threads | the card moves to `threads/archive/`; the documents stay in place |
| Skipping a stage | allowed in any direction but past a receipt |
| Moving back a stage | delete the document; `reopen` does it for a receipt |
| Work in progress | lives in the plan document, under Progress; there is no "active" state. `updated` and `stale` say how fresh it is |
| Blocked | a line of text on the card, not a stage |
| Due dates | dropped; phases carry the timeline |
| File names | the thread's title in every folder; identity is the id in the frontmatter |
| Who writes a new document | the `thread` tool, from the model's `text`; Edit revises it |
| Colors and icons | Obsidian callouts, from a CSS snippet in the project folder's own `.obsidian/` |
| Tools | two, `threads` and `thread`, plus `phase` |
| The knowledge base term "Active Threads" in `wiki/hot.md` | the atlas shows it as "Hot topics", so "thread" means one thing |

## Left for later

- A Threads tab in the view.
- A Stop hook line when the session changed the work and touched no thread.
- Links between threads: one thread that follows from another's receipt.
- An Obsidian Base over the cards, shipped with the snippet.
