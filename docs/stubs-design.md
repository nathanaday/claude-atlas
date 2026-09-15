# Stubs

Status: designed 2026-09-14, not implemented.

A user writes `[[vanishing gradient problem]]` in a page to mark a topic they
will write up later. This document sets out how lint, the core, the session
hook, and the atlas treat that link, and how it becomes a page.

## Before this design

- Lint reported the link under `dead_links` and counted it in `issues_found`.
  The atlas showed it as a dead link.
- Nothing created a page for it.
- Refresh counted pages with `status: seed` as "seed pages". Lint and the
  session hook did not.
- A click on the link in Obsidian created an empty note at the vault root.
  Lint resolves links against every file in the vault but reads only `wiki/`
  as pages, so the link stopped being dead and nothing checked the empty note.

## Lint

Lint stays read-only. It gains two lists. Neither counts in
`category_counts`, `issues_found`, or `lint --strict`, and a new vault shows
neither.

### Wanted pages

`wanted_pages` lists links that name a page nobody has written. A link is a
wanted page when all of these hold:

1. Its syntax is `wikilink`. An embed with no target stays a dead link.
2. No file in the vault resolves it.
3. Its file part (the text before `#` and `|`) contains no `/`, and
   `vault.SanitizeTitle` returns it unchanged. A page named after it then
   resolves the link.
4. Its source is not `wiki/log.md`, a page under `wiki/folds/`, or an index
   page (`index.md`, `_index.md`, a folder index). The log links pages it
   created, and a page deleted later leaves that link with no target. An index
   link with no target stays a dead link and a stale index entry. A MOC is not
   an index here.
5. No page stem or alias is a near match.

Near match: lowercase both names and drop every character that is not a
letter or a digit. The names match when the results are equal, or when the
Levenshtein distance is at most 1 for a name of up to 8 characters and at
most 2 for a longer one. The link stays in `dead_links` with `suggestion` set
to the matching stem or alias, so `[[Atals]]` suggests `Atlas`.

Entries group by file part without regard to case, since links resolve that
way. Each entry holds `title`, the file part as first written in path and
line order, and `links`, each with `source` and `line`.

### Stubs

`stubs` lists pages that exist and hold nothing yet. A page under `wiki/` is a
stub when either:

- its frontmatter has `status: seed`, and its body is empty once the
  frontmatter, heading lines, block id lines, HTML comments, and whitespace are
  removed; or
- the file holds only whitespace and a wiki page other than the log links to
  it.

The log, the hot cache, the overview, index pages, task pages, and pages under
`wiki/meta/` and `wiki/folds/` are never stubs.

Each entry holds `path` and `linked_from`, the source pages. A stub is not
reported in `unindexed_pages` or `empty_sections`. An empty file that is a
stub is not reported in `missing_frontmatter`. A stub that nothing links to
still appears in `orphans`. An empty file that nothing links to is not a stub
and keeps its `missing_frontmatter` and `orphans` findings.

### Report

- `Summary` gains `wanted_pages` and `stubs`.
- `ReportVersion` becomes 2, because `dead_links` no longer holds wanted pages.
- The Markdown report adds "Wanted pages" and "Stubs to fill" after the issue
  sections.
- `Report.Problems`, which feeds plan warnings, reports a wanted page in a
  written page as `<source>:<line> links to "<title>", which has no page yet`.

## The stub operation

`txn.StubRequest(v, titles, now)` builds the request. The `stub` tool and
`claude-atlas stub` pass it to `txn.Prepare` and `txn.Apply` at once, as
`plant` does: no preview, one commit, and `undo` reverts it.

- With no titles, it stubs every wanted page and every empty-file stub in a
  fresh lint report.
- With titles, each `{title, type}` must name a wanted page or an empty-file
  stub, compared without regard to case. Otherwise it refuses and says why: a
  page exists (with its path), a near match exists (with the suggestion), or
  nothing links to the title.
- The type defaults to `concept` in generic mode and `note` in lyt mode. It
  must be a type the mode files. `source` is refused, because a source page
  needs a captured file and a ledger record.
- The path comes from `Vault.RouteFor`, and the content from `vault.Skeleton`:
  frontmatter with `status: seed`, the title, and the type's headings.
- The file name is the title as first written.
- An empty-file stub whose path equals the routed path is replaced. Otherwise
  the empty file is deleted and the stub created in the same request. Apply
  first commits the empty file as a `manual` edit, so `undo` restores it.
  Prepare records the empty file's hash, so apply fails with a conflict if the
  user typed into it in the meantime.
- With nothing to stub, it makes no commit and returns an empty list.

The request has the new kind `stub`. It may create, replace, and delete `.md`
files under `wiki/`, and nothing under `wiki/tasks/`. The summary is
`stub <title>` for one page, and `stub <n> pages: <a>, <b>, <c>` for more,
naming at most three titles and then `and <n> more`.

It returns each stub's title, type, and path, with the operation id and
commit.

### Surfaces

| Surface | Form |
|---|---|
| MCP tool `stub` | `titles` (optional list of `{title, type}`), `vault` |
| CLI | `claude-atlas stub VAULT [TITLE...] [--type T]` |
| TUI | none; the TUI is a subset of the CLI |

## Filling a stub

A stub is filled by an ordinary operation: `save` when the content comes from
the conversation, `wiki-ingest` when it comes from a source, or a hand edit in
Obsidian. The plan replaces the page, sets `status: developing`, and adds the
page to `wiki/index.md` or a MOC. Once the body holds content, lint no longer
lists the page as a stub. It then reports the page as unindexed until an index
or MOC links it, and reports the headings that are still empty.

## Where the counts appear

### Session start

After the task lines, the hook prints one line when either count is above
zero:

```
Stubs: 3 pages to fill (vanishing gradient problem, …). Wanted: 2 linked pages do not exist yet (…); the stub tool creates them.
```

Each list names at most five titles, then `and <n> more`. The hook runs lint
for the counts. Lint took 8 to 29 ms on eleven vaults of 4 to 66 pages; the
hook timeout is 10 s.

### Atlas

- `tree.Unfinished` replaces `SeedPages` (`seed_pages`) with `Stubs`
  (`stubs`) and adds `WantedPages` (`wanted_pages`). Refresh takes both from
  the lint report it already runs. `refresh.SeedPages` is removed. Refresh
  rebuilds the state files, so the rename needs no migration.
- `Overview.md`, the TUI detail pane, and `claude-atlas info` show
  "empty sections · stubs · wanted pages · dead links". The unfinished total
  adds all four.

## Obsidian settings

- The template `.obsidian/app.json` gains `"newFileLocation": "folder"` and
  `"newFileFolderPath": "wiki"`. A click on a placeholder link then creates
  the empty note under `wiki/`, where lint lists it as a stub.
- `vault.Upgrade` and `vault.Adopt` add both keys to an existing `app.json`
  only when `newFileLocation` is absent, keeping every other setting.
- Obsidian uses the same setting for a note made with Cmd+N. Such a note
  lands in `wiki/`, and without frontmatter lint reports it as missing
  frontmatter, as it does any hand-made page under `wiki/`.

## Skills and docs

- `wiki-lint`: the two lists, and a section on stubbing wanted pages: call
  `stub` with no titles to take the defaults, or with titles and types when a
  name is a person, product, or organization (`entity`); report the paths.
- `wiki`: a routing row for filling a stub.
- `references/frontmatter.md`: what a stub is.
- The `wiki-lint` agent, `docs/core-design.md`, `docs/usage.md`, the
  `Reference.md` template, and CLAUDE.md.
- The plugin version goes up in `plugin.json` and `marketplace.json`.

## Left for later

- Lint resolves `[[Atlas]]` to both `Atlas.md` and `Atlas.canvas` and reports
  the link as ambiguous. That is a separate change.
- `status` does not report the counts; the session hook does.
