---
type: task
title: "Init creates a git repository"
status: done
priority: normal
phase: ""
due: ""
created: 2026-09-17
updated: 2026-09-17
task_id: task-20260917-635f
---

# Init creates a git repository

## Idea

New projects created through 'claude-atlas init' should be initialized with a .git directory if one does not already exist.

## Outcome

`init` now makes a work folder a git repository when it is in none.

- `vaults.InitProject` runs `git init` on `main` and commits nothing. It returns a `ProjectInit` with `Git`: `created`, `existing`, `enclosed`, or `skipped`.
- It leaves an existing repository as it is. It makes no repository in a folder inside another repository, because that repository already holds the folder's history.
- It checks the folder before `git init`. If `project.Init` then fails, it removes the `.git` it made.
- `init --no-git` and the `project` tool's `no_git` skip the repository. The CLI prints a `git` step; the tool reports `git`.
- Updated: `docs/usage.md`, `docs/v3-design.md`, `skills/atlas-project/SKILL.md`, `CLAUDE.md`.

Not done: the six projects you already made without git (`itl/*`, `usc/cs513-course`, `usc/cs566-course`, `usc/cs566-project`) are unchanged. Run `git init` in them by hand if you want repositories there.

Tests: `TestInitProjectMakesARepository`, `TestInitMakesAGitRepository`.
