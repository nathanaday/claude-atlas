---
type: task
title: "Fix the Next hint after init PATH"
status: done
priority: normal
phase: ""
due: ""
created: 2026-09-17
updated: 2026-09-17
task_id: task-20260917-1a4c
---

# Fix the Next hint after init PATH

## Idea

Fix the Next hint: 'claude-atlas init PATH' run from another folder prints 'claude-atlas plant . "..."', but '.' is not the project. The hint should name the project or its path.

## Outcome

`init` now builds its Next hint from where it ran (`internal/cli/cli.go`, `initProject`).

- Run inside the folder: `claude-atlas plant . "..."` and `claude`, as before.
- Run with a PATH from elsewhere: `claude-atlas plant NAME "..."` and `claude-atlas open-claude NAME`.
- `entryArg` gives the project's name when that name finds it, and its path when the name is shared (for example, a project and a knowledge base both called `welcome`). `shellArg` quotes a name with spaces.
- `sayCommands` sizes the comment column from the longest command, so a long name no longer runs into its comment.

Tests: `TestInitNextHintNamesTheProject`, `TestShellArg`.
