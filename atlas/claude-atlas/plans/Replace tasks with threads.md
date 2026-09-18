---
type: plan
thread: thr-20260917-5ac2
title: "Replace tasks with threads"
created: 2026-09-17
---

> [!plan] Replace tasks with threads
> [Stub](<../stubs/Replace tasks with threads.md>) → [Spec](<../specs/Replace tasks with threads.md>) → **Plan** → Receipt
> `thr-20260917-5ac2` · [Thread](<../threads/Replace tasks with threads.md>) · filed 2026-09-17

Approach: a new package `internal/threads` replaces `internal/tasks`. `Load` derives each stage from the documents; every write ends in `Sync`, which regenerates the cards, the first callout of each document, and the board. The consumers (mcpserver, cli, hooks, registry, refresh, tui, actions) move over in one change. The skills and the docs follow.

Tested by the package tests of `threads`, the in-process MCP tests, the CLI tests, the hook tests, and a manual run of the binary in a scratch project.

## Progress

- 2026-09-17 · Built all of the above. `go vet ./...` and `go test ./...` pass. This project was upgraded with the new binary: its four archived tasks are now closed threads. Not done: nothing is committed, the plugin is not republished, and nobody has looked at the pages in Obsidian yet. Next: the user reviews, then `thread-receipt`.
