# The atlas as a graph

The atlas vault holds one page per project under `tree/`. This document
records how the tree became a graph and why the pieces sit where they do.

## Nodes

| Directory or page | One per | Written by |
|---|---|---|
| `tree/<category>/<project>.md` | project | the user |
| `repos/<name>.md` | mounted repository | `link`, `new-repo`, the `l` screen, or the user |
| `categories/<path>.md` | folder under `tree/` | `refresh`, from scratch |
| `Tree.md` | the root | `refresh`, from scratch |

A project page links its category by sitting in the folder; the category page
links the project. A project page links its repositories through `repos`, and
other projects through `related`. Obsidian resolves links in properties, so
every one of these is an edge in the graph view.

## The wiki and the repository

A project has two kinds of place, and the atlas keeps them apart on purpose.

The vault is memory, thinking, and triage: pages Claude writes and cites, the
hot cache, the tasks. Its structure is the wiki's, every change is a reviewed
operation, and the core owns what it can derive.

A repository is where the deliverables are made: code, a paper, a deck, a
report. It is a git repository with its own history, and it owes nothing to
the wiki's layout: a course's papers and decks are not wiki pages and should
not be; a codebase has its own stack and workflow. What the repository gets
from the link is the vault behind it: a session started inside it reaches the
wiki and the tasks, and a task's `workdir` names it.

So a link is a mounted repository, always a git repository. `link` refuses a
plain folder until the user agrees to initialize one there, with one commit of
what it holds. `new-repo` creates one, by default beside the wiki in the
vault's folder; the vault's `.gitignore` then names it, so the two histories
stay apart, and Obsidian still shows it. Any other folder on the machine
works too.

Folders of sources the vault ingests from are not links: ingest stages from
any path and the vault remembers the folders it staged from, so an ingest
with no path picks up what is new. Pages under `materials/`, from when links
could be plain folders, still read; refresh moves the ones that are git
repositories under `repos/` and signals the rest until they are initialized
or unlinked.

## Why a repository is a page

Before, `repos` held paths. The graph could not show a path, and two projects
that used one repository each held a copy of it.

A page per repository gives it one identity. The path lives once; a shared
repository is one node between the projects that use it; `[[` in Obsidian's
property editor lists the pages, so linking from inside Obsidian is a pick
rather than a paste. The page is authored, not generated: the user may rename
it, write notes in it, or create one by hand with `schema: atlas.link.v1`
and a `path`. Refresh derives the facts about the repository (branch,
uncommitted changes, last commit) into `~/.claude-atlas/state/links.json` and
the overview, never into the page.

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

`claude-atlas link NAME TARGET` takes a repository's path or a page name;
`--init` makes a plain folder a repository first, and the interactive forms
ask before doing so. `claude-atlas new-repo NAME REPO [--at DIR]` creates one.
Every field in the interactive screens that takes a path completes it as a
shell does, and the link field also completes page names. In `view`, `l` is a
screen of its own: one box per repository, and `n`, `a`, `e`, `u` act at
once, one backend call each, then the atlas refreshes in the background. The
project editor keeps `related` because a relation is a field of the project
page; a repository is an object of its own.
