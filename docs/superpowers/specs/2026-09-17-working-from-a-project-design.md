# Working from a project: repositories in the knowledge base, and the work skill

Date: 2026-09-17. Applies to claude-atlas 1.3.0; ships as 1.4.0. Phases 1
and 2 built on 2026-09-17.
Builds on `docs/tasks-design.md` and `docs/v2-design.md`. Nothing here
changes what a task, a mount, or a repository is.

## Goal

A software project in the atlas has one vault, several repositories, and
the knowledge bases it mounts. A session that starts in the vault should be
able to take a change from a sentence to commits in the right repositories,
with the knowledge bases as its evidence, and leave the vault and the
knowledge bases telling the truth afterwards. Today the pieces exist, but
three things are missing:

1. A session in the vault is not told what repositories the project has.
2. A knowledge base learns nothing about a repository until someone writes
   pages by hand. A mature repository added to a new project is invisible.
3. The task ceremony is four skills and four previews. A change the user
   wants now has no shorter path, and no path that spans two repositories.

The ordinary Claude Code workflow, a repository with its CLAUDE.md, stays as
it is. The project is a layer above it, not a replacement.

## What already holds

- The session-start hook derives everything a CLAUDE.md in the vault would
  say: kind, mounts, the search rule, stubs, open tasks, the hot cache. So
  the vault template gets no CLAUDE.md. A static file would repeat derived
  facts and drift.
- A project session's tools resolve to the project. The repositories sit at
  absolute paths, ordinary tools edit them, the guard protects only `wiki/`,
  and `repos` gives each change policy. Orchestration across repositories
  needs no new write path.
- Claude Code loads a subdirectory's CLAUDE.md on demand, so a repository
  under `repos/` brings its own instructions. A linked repository elsewhere
  on disk does not: Claude Code discovers CLAUDE.md files only above the
  starting directory and below it. Subagents inherit the parent's directory
  and its CLAUDE.md set, and take no working directory of their own. So a
  skill that works in a linked repository reads that repository's CLAUDE.md
  by path before it changes anything there.
- The ingest rule already decides where repository knowledge goes. What the
  project does next and did is project management and stays in the project's
  wiki: tasks, progress, decisions about the work. What the software is, what
  it can do, how it is built and laid out, its concepts, is knowledge another
  project may need, and goes to a knowledge base.

## The rule

CLAUDE.md's "Tool, skill, or hook" applies.

- Facts are tools: which repositories a project has, which page describes
  one, the commit that page was written from, how far HEAD has moved since.
  Two runs answer alike, so code answers.
- Procedure is skills: what to read before deciding, which repositories a
  change touches, when to ask, where a superpowers plan goes, what to update
  when the work lands.
- The hook carries the facts into the session's context and nothing else.

## Repositories in a session

`repos` gains two fields per repository:

| Field | Meaning |
|---|---|
| `claude_md` | the path of the repository's CLAUDE.md, empty when there is none |
| `described` | the page that describes the repository, or absent: `page` (vault-relative), `in` (the project's name or the mount's name), `commit` (what the page was written from), and `behind` (commits on the current branch since it; -1 when the commit is empty or not in the history) |

The description page is found by a scan of the project's wiki and every
mounted knowledge base for a page with `type: entity`,
`entity_type: repository`, and a `repo` property that names the repository:
its remote, compared without scheme, case, or a trailing `.git`, or its name.
Several matches take those in a knowledge base over the project's, then the
one nearest HEAD, then the first path, so two runs answer alike.

The session-start hook prints one line per repository in a project session,
after the mount lines, in the vault and in a repository alike:

```text
Repository: claude-atlas · changes: commit · repos/claude-atlas (main) · described in ai-tools (wiki/entities/claude-atlas.md) at fc70d93, 12 commits behind · CLAUDE.md: repos/claude-atlas/CLAUDE.md
Repository: paper · changes: pr · ~/code/paper (main) · not described in the wiki or a knowledge base
```

`claude-atlas repos [NAME]` prints the same description on each line. The
repositories screen in `view` shows a short form of it under the git facts,
from the state the last refresh derived (`State.RepoDescriptions`).

A new package `internal/repomap` owns this: the page lookup, the commit
comparison through `gitx`, and the snapshot below. `discover`, `mcpserver`,
`hooks`, `cli`, and `tui` call it; it writes nothing.

## A repository enters the knowledge base

A repository is not a source: the code lives in its own git and changes
under it. What is captured is a snapshot, a markdown file the core writes,
small enough to read and cite:

```yaml
---
title: "claude-atlas at fc70d93"
type: repo-snapshot
repo: github.com/nathanaday/claude-atlas    # the remote, or the name when there is none
name: claude-atlas
commit: fc70d93a…
branch: main
taken: 2026-09-17
since: ""                                   # the previous snapshot's commit, when one exists
---
```

The body carries, in this order: the repository's CLAUDE.md verbatim; its
README verbatim; the file list from `git ls-files`, folders only past 2000
paths; the first heading of every markdown file under `docs/`; and, when
`since` is set, `git log --stat` from `since` to `commit`, capped at 200
commits. Nothing else. The snapshot is the immutable source, captured into
`.raw/captured/` like any file, so every claim on a repository page cites a
commit through a source the ledger holds. A page that cited the repository
directly would point at a history that can be rewritten or deleted.

`stage` gains `repo`: the name of one of the project's repositories, not
with `paths` and without `dry_run`. It writes the snapshot to
`inbox/<name>-<short commit>.md`, or reports it already waiting or captured,
and remembers nothing; `stage` with no paths restages folders, not
repositories. `since` is filled from the described page's `commit`, and the
log section is left out when that commit is HEAD or not in the history. The CLI
counterpart is `claude-atlas ingest NAME --repo REPO`, which stages the
snapshot and continues as `ingest` does. `view`'s repositories screen gets
`i` on a row for the same thing.

The repository page is an entity page:

```yaml
---
type: entity
title: "claude-atlas"
entity_type: repository
repo: github.com/nathanaday/claude-atlas
commit: fc70d93a…
sources:
  - "[[claude-atlas at fc70d93]]"
---
```

Its body, from the snapshot and, when the skill needs more, from reading the
code: what the repository is for; how to build, test, and run it; its layout
at the level that changes slowly; the concepts and terms it introduces, each
its own concept page when another page would want to link it; and where to
look for what. It is a map, not a copy of the docs. `commit` is the fact the
core reads; the skill sets it to the snapshot's commit on every rewrite.

The `repo-map` skill runs the procedure, in a project session:

1. Call `repos`. Take the repositories the user names, else every one that
   is not described, and say which.
2. For each, decide the vault as `wiki-ingest` does: a mounted knowledge base
   whose scope covers the software, asked once when two do or none does; with
   no mount, hand off to `atlas-mount` first. The project's wiki is never the
   silent fallback.
3. `stage` with `repo`, then `capture` into the chosen vault, then `route`
   for the repository title and the concepts.
4. Read the snapshot in full. Read the code where the snapshot does not
   answer: the entry points, the package list, the test layout. Bounded: at
   most twenty files beyond the snapshot, and say so.
5. One plan of kind `ingest` per repository: the repository page, the concept
   pages, the source page. Preview, apply.

`wiki-ingest` gains one line: a `repo-snapshot` file in the inbox belongs to
`repo-map`, as a task note belongs to `task-plant`.

## The knowledge base stays current

`behind` is the signal. `status` names every repository whose page is more
than 20 commits behind, next to stale tasks, and the hook's repository line
carries the count always. Lint does not: it is read-only and vault-local,
and this fact needs the repository's git.

`task-finish` gains a step, after the Outcome and before the leftovers: for
each repository in the task's `repos`, when `behind` is above zero, offer to
bring the page up to date. On yes: `stage` with `repo`, capture, read the
snapshot's log section and the diff the task made, and one plan of kind
`ingest` that replaces the repository page with the new `commit`, updates
what changed, and adds or replaces concept pages for what is new. A
repository with no page is offered to `repo-map`.

A change that lands outside a task, in a plain repository session, still
moves `behind`; the next project session sees the count and the user runs
`repo-map` when it matters.

## The work skill

`work` is the entry point for a change. It owns what comes before the task
page has a plan, and hands the rest to the stage skills, so each stage keeps
one procedure.

1. **Orient.** Call `tasks` and `repos`. If the user names or means an open
   task, take its page; otherwise the prompt is the idea.
2. **Fact-find.** Read the repository pages of the repositories that could be
   involved, at most five. Grep the project's wiki and every `kb/` mount for
   the subject, and read what matches, at most five pages. Read each
   candidate repository's CLAUDE.md by path. Read `wiki/hot.md`.
3. **Decide.** Name the repositories the change touches and why, the approach
   in one paragraph, the steps in order with the repository each lands in,
   and what done looks like. Ask one round of questions only when an answer
   would change the repositories or the approach. State the plan and wait
   for a yes.
4. **Record.** A new task: `plant` with the title, the idea verbatim, `repos`,
   `plan`, and `start`. An existing task: one plan of kind `task` replacing
   the page with the Plan section, `repos`, `status: active`, and the first
   Progress line. One commit either way.
5. **Hand off** to `task-run`, which works the plan and writes progress, and
   then to `task-finish`.

The stage skills stay for a task that spans sessions or needs the questions
`task-plan` asks. `task` routes "do this", "make this change", "implement",
and a prompt that names a repository to `work`.

### With and without superpowers

`task-run` says where a plan's artifacts go. When superpowers is installed
and the change warrants it, `work` offers `superpowers:brainstorming` for the
decide step and `task-run` offers `superpowers:writing-plans` and
`superpowers:executing-plans` for the run. Their files go under
`docs/superpowers/` of the repository the change centres on, named by
absolute path, because superpowers writes relative to the working directory
and in a project session that is the vault root. A spec or plan never lands
in the vault. The task page links each file by path in its Plan section.

Without superpowers, the task page's Plan section is the spec of record, and
`task-run` works it directly under the repository's own CLAUDE.md. Either
way the record is the same: the task page holds the idea, the plan, the
progress, and the outcome, and the outcome names every commit or pull
request per repository. The task ledger keeps the operations.

### Several repositories

`task-run` works one repository at a time in the order the plan gives.
Before the first change in a repository: read its CLAUDE.md; take its policy
from `repos`; on `pr`, make the branch. A subagent sent into a repository is
told the repository's path and its CLAUDE.md path, and works by absolute
path. Progress lines name the repository. A step that needs two repositories
changed together, an interface and its caller, is two steps with the
interface first, and the plan says so.

## Task pages

The page gains one property and `plant` gains three arguments.

```yaml
workdir: ""              # unchanged: the folder a session opens in
repos:                   # every repository the task changes, by name
  - claude-atlas
  - paper
```

`workdir` stays a string, because `open-claude --task` opens one folder and
existing pages carry it. `repos` is a list of names; the core validates the
shape and the skill checks the names against `repos`. The `tasks` tool and
the ledger carry it; the atlas task board shows it.

`plant` gains `repos`, `plan` (the Plan section's text), and `start`. With
`plan`, the status is `planned`; with `plan` and `start`, the status is
`active` and Progress gets a dated line saying what is starting. `start`
without `plan` is refused. The CLI's `plant NAME TEXT` is unchanged.

## What is refused

- `stage` with `repo` for a name that is not one of the project's
  repositories, or for a repository whose folder is gone or has no commits.
- A `repos` name on a task page that is not a list of strings.
- `plant` with `start` and no `plan`.
- A repository page whose `commit` is not a hex string.

## Testing

- `repomap`: a temporary git repository with three commits; the snapshot's
  sections, the folder cap, `since` and the log; the page lookup across a
  project and a mount; `behind` for a commit in history, at HEAD, and not
  in history.
- `mcpserver`: `repos` with a described and an undescribed repository;
  `stage` with `repo` writes the snapshot into the inbox and refuses a wrong
  name; `plant` with `plan`, with `start`, and `start` alone.
- `hooks`: the repository line in a project session; none in a repository
  session or a knowledge base.
- `tasks`: `repos` on a page round-trips through the ledger and the index.
- `cli` and `tui`: the columns and the `i` key, driven with `tea.KeyMsg`.
- The skills by hand: `claude -p` in a project that links a real repository,
  through `repo-map`, then `work` on a small change, then `task-finish`.

## Phases

1. Facts: `internal/repomap` lookup and `behind`; `repos` fields; the hook's
   repository line; `claude-atlas repos` columns; the repositories screen.
2. Entry: the snapshot; `stage` with `repo`; `ingest --repo`; the `i` key;
   `entity_type: repository` in the frontmatter reference; the `repo-map`
   skill; the line in `wiki-ingest`.
3. Work: `repos` on task pages; `plant` with `plan` and `start`; the `work`
   skill; `task-run` for several repositories and superpowers; `task` routes
   to it; `docs/usage.md`.
4. Current: the `status` line; the sync step in `task-finish`.

Each phase is one branch. A phase that changes a skill or a hook bumps the
plugin version, because a skill change reaches Claude Code only through a
release; phase 1 changes only the binary.

## Decisions

- No CLAUDE.md in the vault template. The hook is the CLAUDE.md and derives
  its content. A user's standing instructions for a project go in
  `wiki/hot.md` or `wiki/overview.md`, which the hook and the skills read.
- A snapshot, not a direct citation of the repository. The immutable-source
  rule holds, and a page's evidence survives a rewritten history.
- The repository page goes to a knowledge base by the ingest rule. The
  project's wiki keeps the work: tasks, progress, decisions. The user
  confirmed the split on 2026-09-17.
- `work` sits beside the four stage skills and hands to them. One procedure
  per stage; one entry that skips the ceremony when the plan is short.
- Superpowers is offered, never required, and its files live in the
  repository, never the vault.
- `repos` on a task page, not a list-valued `workdir`. The old key keeps its
  meaning and its pages.
- `behind` and not lint. Lint stays vault-local and needs no repository git.

## Left for later

- A lint finding for a repository page whose `commit` is not in the
  repository's history, once lint can be told where the repositories are.
- Whether Claude Code loads a CLAUDE.md through the `kb/` symlinks; the docs
  do not say, and the skill reads by path anyway.
- A `search` tool across the project and its mounts, which the fact-find step
  would use instead of Grep.
- Planting a task into another project from `work`, when a change belongs to
  a repository this project does not have.
