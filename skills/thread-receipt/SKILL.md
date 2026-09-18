---
name: thread-receipt
description: "Close a thread with its receipt: completed, with what was delivered and how it was verified, or killed, with the reason; then offer the knowledge base what the work taught. Use for finish, done, close this thread, complete, ship it, kill, cancel, abandon, drop this thread, wrap up."
---

# Close a thread

Read [threads.md](../wiki/references/threads.md). Tools: `threads`, `thread`,
and `status`, `route`, `plan`, `apply` for the knowledge base.

The receipt is the record of how the thread ended. A thread may close from
any stage: a stub that was never worth doing gets a receipt as much as a
feature that shipped.

## Decide the outcome

Call `threads` with the `id` and read the documents. `completed` means the
spec's definition of done holds, or the plan's when there is no spec.
`killed` means the work stops for good. Ask when it is unclear.

Before `completed`: run the tests and the checks the plan names, now, and
read the result. A thread does not complete on tests that fail or were not
run. When the work sits on a branch, it merges after they pass.

## Write the receipt

Call `thread` with `id`, `stage: receipt`, `outcome`, and the receipt as
`text`:

- **Completed:** what was delivered, in the user's terms; where it is
  (commits, files, pages); how it was verified, with the commands and the
  result; where the work left the spec or the plan, and why; what is left
  undone.
- **Killed:** the reason, and what to keep from the work so far.

Keep it short and exact. The earlier documents stay as they are: they are
the record of how the thread went.

The card moves to `threads/archive/`. Report the receipt's path. When the
thread's phase is now finished, say so.

## What is left

Work left undone is a new thread: offer a stub for each item, in the same
phase, and open the ones the user picks.

## The knowledge base learns from completed work

Only for a `completed` thread whose project has a knowledge base, offer one
operation there. Say what it would hold, and wait for a yes.

- **The project's page.** `status` says whether one exists and how far
  behind it is. When it exists, an update: what the project now delivers.
  When it does not, offer `describe`.
- **A durable fact.** Knowledge about the ecosystem, not this project alone
  (a cause found, a mitigation that works, a measurement), belongs on a
  concept page. `route` says whether one exists.

On yes: read the pages and [operations.md](../wiki/references/operations.md),
build one plan of kind `save` with the page writes, `wiki/index.md` when a
page is new, and `wiki/hot.md` under 500 words. Show the preview and apply.
Declining costs nothing.
