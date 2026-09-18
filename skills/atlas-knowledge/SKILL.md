---
name: atlas-knowledge
description: "Create a knowledge base step by step: name, location, a scope written well, the filing mode. Also change a scope, rename, adopt an existing Obsidian vault, or forget one. Use for new knowledge base, create a knowledge base, knowledge base for papers, change the scope, adopt this vault, forget this knowledge base."
---

# Create or change a knowledge base

Tools: `atlas`, `vault` on the atlas MCP server. A knowledge base holds
sources, entities, and concepts, an inbox for sources to ingest, and the
user's `ideas/`. It is the Obsidian vault the user opens. Projects use it;
a project uses one, a knowledge base serves many.

Call `atlas` first: the knowledge bases that exist, their scopes, and the
names already taken.

## Ask, in this order

1. **Name.** It becomes the folder's name.
2. **Location.** The folder the knowledge base goes in, as an absolute or `~`
   path. There is no default place. Suggest the parent folder of the
   knowledge bases `atlas` lists, when they share one. Never a folder inside
   a project or inside another knowledge base.
3. **Scope.** Two sentences: what it holds, and what it does not. A project
   session and its ingest read the scope to know what belongs here, so a
   scope that overlaps another knowledge base's sends sources to the wrong
   place. Draft one from the user's words, read the existing scopes back,
   and adjust until each is distinct. Split a knowledge base only at a trust
   boundary: work and personal, or one that must stay private and one that
   may be shared. Few and large beats many and small.
4. **Mode.** `generic` files pages by type; `lyt` keeps atomic notes and Maps
   of Content. Default `generic`.

## Confirm and create

One line, then yes:

> Create knowledge base `product-x` at `~/Vaults/product-x`, scope "The thermal fire-detection product line: cameras, firmware, alarm pipeline, false-alarm sources and mitigations. Not personal projects."?

On yes: `vault` with `action: create`, `name`, `path`, `scope`, and `mode`. Report the result. Then say what comes next: `init` in
a work folder makes a project, `link` connects it here, and sources go in
this knowledge base's `inbox/`.

## Adopt an existing vault

An Obsidian vault, a claude-obsidian vault, or a v1 or v2 knowledge base
becomes a knowledge base with `vault` and `action: adopt`, `path`, and a
`scope`. Nothing in it is replaced. A v2 project vault is refused: v3
projects are folders in the work; `atlas-project` makes one with `init`, and
the old vault is deleted by hand once nothing in it is wanted.

## Change a knowledge base

- Scope: `vault` with `action: edit`, `target` (the knowledge base by name,
  id, or path), and `scope`. An empty scope clears it.
- Rename: `vault` with `action: edit`, `target`, and `name`, the new name.
  The folder moves with the name; say so.
- Mode: the `wiki-mode` skill, in the knowledge base's session.
- Forget: `vault` with `action: forget` and `target`. The folder stays, and
  a session started in it lists it again.

The tool call comes after a yes. Never make the folder or its files yourself.
