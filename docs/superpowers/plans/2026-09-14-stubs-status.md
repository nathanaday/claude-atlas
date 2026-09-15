# Stubs: status and remaining work

Status on 2026-09-14. Work on the plan stopped after Task 7 because the
project's design is changing (`docs/v2-design.md`). This page records what is
on `main`, what is not done, and what v2 changes.

- Spec: `docs/stubs-design.md`
- Plan: `docs/superpowers/plans/2026-09-14-stubs.md`

## On main

| Task | What it does | Commits | Review |
|---|---|---|---|
| 1 | Lint lists wanted pages; a near match stays a dead link with a suggestion | `499ec32` | passed |
| 2 | Lint lists stubs and exempts them from unindexed, empty sections, and (empty files) missing frontmatter | `eee8fd4` | passed |
| 3 | The atlas counts stubs and wanted pages instead of seed pages | `924befa` | passed |
| 4 | New vaults, upgrade, and adopt put Obsidian's new notes under `wiki/` | `df8e578` | passed |
| 5 | `txn.StubPages` and the `stub` operation kind | `97003d7`, fix `dd3b93c` | passed after one fix |
| 6 | The `stub` MCP tool | `6b7a5a2` | passed |
| 7 | The `claude-atlas stub` command | `07fe4f2` | not reviewed; the review was stopped |

`go test ./...` and `go vet ./...` pass at `07fe4f2`.

What a user can do now:

- `claude-atlas lint` lists "Wanted pages" and "Stubs to fill" after the
  findings. `--strict` ignores both.
- `claude-atlas stub VAULT` and the `stub` tool create a seed page for each
  wanted page, as one commit that `undo` reverts.
- `refresh`, the overview, the TUI, and `show` count stubs and wanted pages.

What a user cannot do yet: see the counts at session start, or have Claude
use the `stub` tool through the `wiki-lint` skill. The installed plugin does
not change until its version goes up (Task 9).

## Not done

1. **Review Task 7.** Run the task review against `6b7a5a2..07fe4f2` with the
   Task 7 brief from the plan.
2. **Task 8: the session-start line.** The hook prints "Stubs: N pages to fill
   (…). Wanted: N linked pages do not exist yet (…)". Not started.
3. **Task 9: skills, docs, and version 0.8.0.** Not started. It covers the
   `wiki-lint` and `wiki` skills, `references/frontmatter.md`,
   `references/operations.md`, the `wiki-lint` agent, `core-design.md`,
   `usage.md`, the `Reference.md` template, CLAUDE.md, and both plugin
   manifests. It also brings `stubs-design.md` up to date: its status line, the
   `StubRequest` signature (now with `defaultType`), and the MCP tool's `type`
   argument.
4. **Task 10: the end-to-end check** in a scratch vault, including one click on
   a placeholder link in Obsidian, which the user does by hand.
5. **The final review** of `285edb9..07fe4f2`, with the deferred findings
   below.
6. **Existing vaults.** No real vault has run `claude-atlas upgrade` since
   Task 4, so none has the new-note folder yet.

## Decisions made during the work

- **Near match.** The spec's first rule matched `[[Chapter 2]]` to `Chapter 1`
  and `[[RNN]]` to `CNN`, and missed `[[Atals]]` for `Atlas`. The rule now
  requires equal digits, counts a swap of two adjacent letters as one edit,
  and matches a name of 4 characters or fewer only when equal. Task 1 updated
  the spec. If wrong: some typos become wanted pages, or some new topics show
  as dead links with a suggestion.
- **Empty files whose names cannot be page names.** A click on `[[What?]]`
  leaves `wiki/What?.md`. `stub` with no titles leaves such a file alone; with
  its title, `stub` refuses and says to rename the link. Without this, `stub`
  renamed the file to `What.md` and the working link became dead. If wrong:
  such files stay empty until the user renames the link.
- **The v2 mount rule is not built.** `v2-design.md` says a link that resolves
  in a mounted knowledge base is not wanted. Lint does not know mounts yet.

## Deferred review findings

None blocks use. The final review decides which to fix.

- Lint (`internal/lint/lint.go`):
  - The `.md`, `.canvas`, and `.base` exclusion in `wantedTitle` has no test
    and is not in the spec's rules.
  - `sortFindings` sorts wanted pages with `sort.Slice`; the file elsewhere
    uses `sort.SliceStable`.
  - `nearIndex.match` recomputes the digits of every candidate on each call.
  - No test shows that a seed stub with incomplete frontmatter still appears
    in `missing_frontmatter`.
  - `bodyEmpty` parses the frontmatter a second time.
- Vault (`internal/vault/vault.go`): `mergeSettings` replaces an `app.json`
  that is not valid JSON with only the merged keys. `mergeAppearance` did the
  same before.
- Stub operation (`internal/txn/stub.go`):
  - With no titles, one candidate that cannot be stubbed (an empty file under
    `wiki/tasks/notes/`, or a routed path that exists) stops the whole request.
    Skipping it would stub the rest.
  - Text typed into an empty file after lint reads it but before `Prepare`
    hashes it becomes the base, so apply deletes it without a conflict. The
    manual commit before apply keeps the text in git.
  - "nothing in the wiki links to" is wrong for a title that a link names but
    lint excludes (links from index pages, the log, folds, or with a folder).
  - `pageNamed` ignores the walk error and keeps walking after a match.
  - Two empty linked files with one name in different folders: only the second
    is a candidate.
  - A repeated title with a different type is skipped without a message.
  - `vault.Skeleton` is called although `route.Skeleton` holds the same text.
  - The refusal "wiki/What?.md cannot be a page's file name" names the file;
    the link text is what cannot become a page name.
  - Tests lack: a lyt-mode run end to end, a summary with more than three
    titles, undo and conflict on the in-place replace, and refusal reasons in
    `TestStubKind`. The `type` schema text omits the lyt types entity, concept,
    question, and session.
- MCP tool (`internal/mcpserver/server.go`): the `stub` description is one long
  sentence with a parenthetical.

## What v2 changes

| Part | Under v2 |
|---|---|
| Wanted pages and stubs in lint | Stay. Lint in a project resolves links through mounts, so a link that resolves in a mount is not wanted (v2 phase 3). Open question: whether a near match against a mount's page names counts. |
| `stub` operation, tool, command | Stay, in both vault kinds. Open question: in a project, whether a wanted page may be stubbed into a writable knowledge base, the way `route` answers across mounts. |
| Atlas counts (Task 3) | v2 phase 2 deletes `tree`, `pages`, and the atlas-vault rendering. The counts must move to the registry and `view`. |
| New-note folder (Task 4) | Applies to both kinds. `adopt --as` must merge the keys, as `adopt` does now. |
| Session-start line (Task 8) | v2 replaces the hook's output for projects and knowledge bases. Write the line against that format, in phase 3. |
| Skills and docs (Task 9) | The `Reference.md` template belongs to the atlas vault, which v2 removes; drop it. The skill and doc changes join phase 6. |
| End-to-end check (Task 10) | Run it against the v2 layout: a project with a mounted knowledge base. |

If v1 ships before v2, finish items 1 to 5 as the plan describes. If not, fold
them into v2: the mount rule and the session line into phase 3, the counts
into phase 2, and the skills and docs into phase 6.

## Where things are

- The work was done on branch `stubs` in a worktree under the session
  scratchpad and fast-forwarded into `main`. Once `main` holds it, remove both:
  `git worktree remove <path>` and `git branch -d stubs`.
- The task briefs, reports, and review packages were in the worktree's
  git-ignored `.superpowers/sdd/`. This page keeps what they held that matters.
