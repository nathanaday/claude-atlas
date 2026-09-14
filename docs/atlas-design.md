# The atlas as a graph

The atlas vault holds one page per project under `tree/`. This document
records how the tree became a graph and why the pieces sit where they do.

## Nodes

| Directory or page | One per | Written by |
|---|---|---|
| `tree/<category>/<project>.md` | project | the user |
| `repos/<name>.md` | linked git repository | `link`, the editor, or the user |
| `materials/<name>.md` | linked folder or file of sources | same |
| `categories/<path>.md` | folder under `tree/` | `refresh`, from scratch |
| `Tree.md` | the root | `refresh`, from scratch |

A project page links its category by sitting in the folder; the category page
links the project. A project page links its folders through `repos` and
`materials`, and other projects through `related`. Obsidian resolves links in
properties, so every one of these is an edge in the graph view.

## Why a linked folder is a page

Before, `repos` and `materials` held paths. The graph could not show a path,
and two projects that used one folder each held a copy of it.

A page per folder gives the folder one identity. The path lives once; a shared
folder is one node between the projects that use it; `[[` in Obsidian's
property editor lists the pages, so linking from inside Obsidian is a pick
rather than a paste. The page is authored, not generated: the user may rename
it, write notes in it, or create one by hand with `schema: atlas.link.v1`
and a `path`. Its kind is the directory it sits in. Refresh derives the facts
about the folder (branch, uncommitted changes, file count, newest file) into
`~/.claude-atlas/state/links.json` and the overview, never into the page.

Pages from before this design held plain paths. `refresh` upgrades them once:
it creates the page and rewrites the entry as a link. This is the only time
refresh changes a project page.

## Why categories are generated

A category is a folder, and a folder has no page, so the tree was invisible to
the graph. Category pages are computed entirely from the folders under
`tree/`, so they are derived state: `refresh` deletes `categories/` and
writes it again, and `Tree.md` with it. Putting them inside `tree/` as folder
notes would mix generated files into the user's space and make every walk
skip them.

## Relations

`related` is one list on one page. The other page shows the relation as a
backlink in Obsidian, and the atlas reads both directions for `show`, the
details screen, and the overview. Storing the relation on both pages would
mean two edits per change and two places to drift.

## Linking from the terminal

`claude-atlas link NAME TARGET` takes a path or a page name. A path is detected
as a repo when it holds `.git`; `--kind` overrides that for a new page only,
because an existing page keeps the kind of its directory. Every field in the
interactive screens that takes a path completes it as a shell does, and the
link field also completes page names. The ingest screen links the folder it
stages from, so the next ingest with no path knows where to look.
