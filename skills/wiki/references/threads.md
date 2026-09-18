# Threads and phases

A thread is one line of work in a project: an issue, a feature, a chore. It
moves through four stages, and each stage is a document in its own folder
under `atlas/<name>/`. Read this before any thread skill changes a page.

| Stage | Folder | The document holds | Skill |
|---|---|---|---|
| stub | `stubs/` | where the thread begins, in the user's words | `thread-stub` |
| spec | `specs/` | what will be true when the thread is done, and why | `thread-spec` |
| plan | `plans/` | how the work will go, then its progress | `thread-plan`, `thread-run` |
| receipt | `receipts/` | how the thread ended: completed or killed | `thread-receipt` |

## The rule that matters

The stage of a thread is never set. It is the furthest document that exists.
A thread moves to a stage when the `thread` tool files that stage's document,
and in no other way. So every change of stage leaves a page the user can open
in Obsidian. That visibility is the reason the project folder exists. Never
move a thread along in conversation only: file the document, then say where
it is.

A stage may be skipped. A one-line fix goes from stub to plan, or from stub
to receipt. A skipped document may still be filed later. A receipt closes the
thread.

## The pages

```text
atlas/<name>/
├── threads/
│   ├── threads.md          the board, generated
│   ├── <Title>.md          the card of an open thread, generated
│   └── archive/<Title>.md  the card of a closed thread
├── stubs/<Title>.md
├── specs/<Title>.md
├── plans/<Title>.md
├── receipts/<Title>.md
├── phases/<Title>.md
└── inbox/                  notes that wait to become threads
```

The card holds the thread's identity: `thread_id` (like
`thr-20260917-3f2a`), `title`, `priority` (high, normal, low, someday),
`phase`, `blocked`, `created`, `updated`. It shows `stage` and `outcome`,
which code writes from the documents. Its body links and embeds every
document, so one page shows the whole thread.

A document carries `type` (its stage), `thread` (the id), `title`, and
`created`; a receipt also carries `outcome`. Its first callout, in the
stage's color, is the way to the thread's other documents. Code owns that
callout and rewrites it. Everything under it is prose.

| Who writes | What |
|---|---|
| The `thread` and `phase` tools | the cards, the board, each document's frontmatter and first callout, a new document from `text` |
| Edit | the prose of a document that exists, and of a phase page |
| Nobody by hand in a session | `threads/` and `project.json`; the hook refuses, and it refuses a new file written straight into a stage folder |

The user may edit any page in any editor. Deleting a document by hand moves
the thread back to the stage before it.

## The tools

| To | Call |
|---|---|
| See the board, or one thread with the path of each document | `threads`, with `id` for one |
| Open a thread | `thread` with `text` (the stub), and optionally `title`, `priority`, `phase`, `from` (an inbox note, removed once the stub exists) |
| File a document | `thread` with `id`, `stage`, `text`; a receipt also takes `outcome`: `completed` or `killed` |
| Change a card | `thread` with `id` and `priority`, `phase`, `blocked`, or `title` |
| Block, unblock | `thread` with `id` and `blocked`: what it waits on, or `""` |
| Open a closed thread again | `thread` with `id` and `reopen`; the receipt is deleted |
| A phase | `phase` with `action` create, rename, reorder, or remove |

`id` is the thread's id, its title, or the start of its title. `text` is
markdown with no frontmatter. A document that exists is never filed again;
revise it with Edit. An Edit on a document marks its thread as updated today;
nothing else needs to say so.

In a knowledge base session every tool takes `project`, the name of a project
that uses the knowledge base; `threads` with no `project` lists them all.

## Phases

A phase is a named slice of the timeline with an order and a goal, a page
under `phases/`. A thread names its phase; a phase never lists its threads.
A phase has no status: it is finished when every thread in it is closed and
it holds at least one.

## Freshness

The documents are the only memory between sessions. Before a session ends,
the plan says where the work stopped and what is next. A thread with a plan
and no update for 14 days is stale; `status`, the hook, and the atlas say so.

## The knowledge base

An open thread never reaches the knowledge base. A completed one may:
`thread-receipt` offers one operation there, as a plan the user sees. The
knowledge base is evidence for a spec and a plan; the documents are the
project's own state.
