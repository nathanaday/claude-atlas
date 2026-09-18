---
type: receipt
thread: thr-20260917-1a4c
title: "Fix the Next hint after init PATH"
outcome: completed
created: 2026-09-17
---

> [!receipt] Fix the Next hint after init PATH · completed
> [Stub](<../stubs/Fix the Next hint after init PATH.md>) → Spec → Plan → **Receipt**
> `thr-20260917-1a4c` · [Thread](<../threads/archive/Fix the Next hint after init PATH.md>) · filed 2026-09-17

`init` now builds its Next hint from where it ran (`internal/cli/cli.go`, `initProject`).

- Run inside the folder: `claude-atlas plant . "..."` and `claude`, as before.
- Run with a PATH from elsewhere: `claude-atlas plant NAME "..."` and `claude-atlas open-claude NAME`.
- `entryArg` gives the project's name when that name finds it, and its path when the name is shared (for example, a project and a knowledge base both called `welcome`). `shellArg` quotes a name with spaces.
- `sayCommands` sizes the comment column from the longest command, so a long name no longer runs into its comment.

Tests: `TestInitNextHintNamesTheProject`, `TestShellArg`.
