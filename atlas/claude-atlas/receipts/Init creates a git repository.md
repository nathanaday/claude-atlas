---
type: receipt
thread: thr-20260917-635f
title: "Init creates a git repository"
outcome: completed
created: 2026-09-17
---

> [!receipt] Init creates a git repository · completed
> [Stub](<../stubs/Init creates a git repository.md>) → Spec → Plan → **Receipt**
> `thr-20260917-635f` · [Thread](<../threads/archive/Init creates a git repository.md>) · filed 2026-09-17

`init` now makes a work folder a git repository when it is in none.

- `vaults.InitProject` runs `git init` on `main` and commits nothing. It returns a `ProjectInit` with `Git`: `created`, `existing`, `enclosed`, or `skipped`.
- It leaves an existing repository as it is. It makes no repository in a folder inside another repository, because that repository already holds the folder's history.
- It checks the folder before `git init`. If `project.Init` then fails, it removes the `.git` it made.
- `init --no-git` and the `project` tool's `no_git` skip the repository. The CLI prints a `git` step; the tool reports `git`.
- Updated: `docs/usage.md`, `docs/v3-design.md`, `skills/atlas-project/SKILL.md`, `CLAUDE.md`.

Not done: the six projects you already made without git (`itl/*`, `usc/cs513-course`, `usc/cs566-course`, `usc/cs566-project`) are unchanged. Run `git init` in them by hand if you want repositories there.

Tests: `TestInitProjectMakesARepository`, `TestInitMakesAGitRepository`.
