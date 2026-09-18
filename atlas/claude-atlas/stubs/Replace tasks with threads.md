---
type: stub
thread: thr-20260917-5ac2
title: "Replace tasks with threads"
created: 2026-09-17
---

> [!stub] Replace tasks with threads
> **Stub** → [Spec](<../specs/Replace tasks with threads.md>) → [Plan](<../plans/Replace tasks with threads.md>) → Receipt
> `thr-20260917-5ac2` · [Thread](<../threads/Replace tasks with threads.md>) · filed 2026-09-17

The only real entity in the project state system is the task: ingest tasks, then a simple task list. That is not enough for clear project state tracking.

Center things on threads. A project has many threads that arise naturally over time. Each thread moves from end to end through distinctive steps: stub, spec, plan, receipt. Each stage has its own file in a designated directory (threads/, stubs/, specs/, plans/, receipts/), tracked by the thread id, laid out with custom Obsidian callouts that give each stage a color and an icon. A skill per stage, lighter and quicker than superpowers. A CLI command set for registration and state getters and setters. The state files must always be captured: visibility is the point of powering part of the repo with Obsidian.
