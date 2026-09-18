---
name: thread-stub
description: "Open a thread from a sentence, from the notes in a project's atlas/<name>/inbox/, or in any project from its knowledge base: a card and a stub in the user's words, no questions asked. Use for add a thread, note this, stub this, remember to, open a thread, report a bug, idea for later, todo, inbox notes, open this in project X."
---

# Open a thread

Read [threads.md](../wiki/references/threads.md). Tools: `inbox`, `thread`.

A stub is where a thread begins: a short, loose note about an issue, a
feature, or a chore. It costs nothing. It holds what the user said, as they
said it; the thinking comes later, in the spec.

## Three doors, one result

1. **A sentence.** Call `thread` with `text`: the user's words, verbatim.
   Add `title` only when the first line would make a poor one. Add
   `priority` or `phase` only when the user named them.
2. **Notes in the inbox.** `inbox` lists them. For each note call `thread`
   with `from` and the note's path; its content becomes the stub and the
   note is removed. A note that holds several ideas becomes several
   threads: pass `text` for each and `from` on the last.
3. **From a knowledge base session.** The same call with `project`.

Ask nothing. Do not improve the text, research it, or plan it. When
something you noticed during other work deserves a thread, open one in your
own words and say that you did.

## Report

Say the title, the id, and the stub's path. When the user wants to go
further now, hand to `thread-spec`, or to `thread-plan` when the thread is
small enough to need no spec.
