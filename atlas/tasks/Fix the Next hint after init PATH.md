---
type: task
title: "Fix the Next hint after init PATH"
status: planted
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
