---
type: spec
thread: thr-20260917-5ac2
title: "Replace tasks with threads"
created: 2026-09-17
---

> [!spec] Replace tasks with threads
> [Stub](<../stubs/Replace tasks with threads.md>) → **Spec** → [Plan](<../plans/Replace tasks with threads.md>) → Receipt
> `thr-20260917-5ac2` · [Thread](<../threads/Replace tasks with threads.md>) · filed 2026-09-17

The design is `docs/threads-design.md`; it is the spec of record for this thread.

Done when:

- A thread's stage is the furthest document that exists, so no thread moves without its file.
- `threads` and `thread` replace `plant`, `tasks`, and `task` in the MCP server, the CLI, the view, the hooks, and the registry.
- Each stage has a skill: `thread-stub`, `thread-spec`, `thread-plan`, `thread-run`, `thread-receipt`, with `thread` to orient.
- Every page opens in Obsidian with its stage's callout color and icon, and the card shows the whole thread.
- `claude-atlas upgrade` turns 2.x task pages into threads.
- `make test` passes.
