---
name: wiki-mode
description: "Read or change the knowledge base's filing mode and see where a new page of a type belongs: generic (typed folders) or lyt (atomic notes and Maps of Content). Use for wiki mode, what is my vault mode, set vault mode, switch to LYT, use generic, methodology routing. Does not move existing notes."
---

# Filing mode

The mode decides where a new page goes in the knowledge base. It changes
nothing about evidence or existing files. Tools: `mode`, `route`, `apply`.
The skill runs in a knowledge base session.

| Mode | New pages | Navigation |
|---|---|---|
| `generic` (default) | `wiki/sources/`, `entities/`, `concepts/` by type | `wiki/index.md` |
| `lyt` | `wiki/notes/`, one idea per note, whatever the type | `wiki/mocs/*.md`; `wiki/index.md` is the home map |

## Read and route

Call `mode` with no arguments for the current mode and the page types it files.
Call `route` with a `type` and `title` to see the path a new page would take, the
skeleton it starts from, and whether that page already exists. A calling skill
may choose a more specific destination when the user names one, but every write
still goes through `plan`.

## Change the mode

1. Confirm the target mode with the user: `generic` or `lyt`.
2. Call `mode` with `set`. It returns a plan that replaces `.claude-atlas.json`
   and nothing else.
3. Show the old mode, the new mode, and the changed path.
4. Call `apply` with the plan id.

The change affects future pages only. It never creates folders, moves notes,
rewrites links, or migrates content. If the user wants existing pages
reorganized, plan that as a separate `markdown` operation with a complete move
map, and warn that moved pages need their links checked with `wiki-lint`.

## LYT conventions

- A note holds one idea and fits on a screen; split what grows past that.
- A note's `mocs:` property names the maps that reach it.
- A MOC is a navigation hub, not a container; notes stay flat in `wiki/notes/`.
- Every new note joins at least one MOC in the same plan.
