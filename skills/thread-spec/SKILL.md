---
name: thread-spec
description: "Turn a thread's stub into a spec: read the stub, research the code and the knowledge base, decide what can be decided, ask only what cannot, and file the spec document. Use for spec this, define this thread, brainstorm, think this through, what should this be, flesh out the stub, requirements, design this."
---

# Write a thread's spec

Read [threads.md](../wiki/references/threads.md). Tools: `threads`, `thread`,
`status`.

The spec says what will be true when the thread is done, and why. It is the
agreement between the user and whoever does the work. It is quick to write:
most of the work is reading, and the user answers only what reading cannot.

## Understand before you ask

1. Call `threads` with the `id`. Read the stub.
2. Find the answers yourself: the code the thread touches, its tests, the
   CLAUDE.md of the work, recent commits, and the knowledge base (the
   project's page, `wiki/hot.md`, a Grep for the subject). A question the
   code answers is not a question for the user.
3. Decide what you can decide. Where one option is right, take it and give
   the reason in the spec. The user reads the spec and can disagree there.

## Ask only what changes the spec

A question goes to the user when the answer is theirs alone (a preference,
a priority, a constraint outside the code) and a wrong guess would change
what is built. There is no right number of questions: none is common, and
ten are fine for a thread that needs them. Ask them together in one
message, each with the answer you recommend, so the user can reply in a
line. Do not ask for approval section by section.

When the stub holds several independent pieces of work, say so first and
offer a thread for each (`thread-stub`). Spec the one the user picks.

## What the spec holds

Write for someone who has not seen this conversation. Use what the thread
needs and leave out the rest:

- The problem, and who has it.
- What done looks like, as behavior someone can check.
- The experience: how it should feel to use, read, or operate. State the
  intent, so that whoever builds it can make good choices you did not
  foresee.
- What is out of scope.
- Constraints: what must not change, what it must work with.
- Decisions made, each with its reason, and the options turned down.
- How it will be verified: the tests or checks that prove it.
- Open questions that the work itself will answer.

The spec says what and why. How belongs to the plan. Keep the spec free of
file-by-file instructions: a spec that dictates the code removes the
judgment of the one who writes the code.

## File it

Call `thread` with `id`, `stage: spec`, and the spec as `text`. The thread
is now at its spec. Tell the user the path, give the spec in a few lines,
and name the decisions you made for them. Revise with Edit when they answer.
A better title found on the way: `thread` with `title`.

Do not start the plan or the work here. Hand to `thread-plan`.
