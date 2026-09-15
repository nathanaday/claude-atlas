---
name: canvas
description: Create, inspect, and update Obsidian JSON Canvas boards with text, file, link, group, and edge nodes. Use for canvas status, canvas lists, visual maps, zones, spatial layouts, adding vault notes or media to a .canvas file, create canvas, add to canvas, put this on the canvas.
---

# Canvas

A canvas is a vault-scoped JSON Canvas document under `wiki/canvases/`. Reads
are read-only; every change goes through one plan of kind `canvas`.

## Syntax source

Prefer a separately installed `kepano/obsidian-skills` `json-canvas` skill for
format details. Otherwise read [references/canvas-spec.md](references/canvas-spec.md).
The open standard is [JSON Canvas 1.0](https://jsoncanvas.org/spec/1.0/).

## Scope

- Call `status` first. Store boards under `wiki/canvases/`; use
  `wiki/canvases/main.canvas` only when the user did not name a board.
- `wiki/canvases/canvases.md` is an optional catalog. Update it only when a canvas
  is created, renamed, or removed.
- Use vault-relative paths in `file` and `background`; reject absolute paths,
  `..`, and `~`.
- Reference only files already inside the vault. Do not download or copy assets
  here; a source enters through `wiki-ingest`.
- Use `link` nodes for HTTPS URLs only. Creating the node makes no request, but
  Obsidian may fetch preview metadata from that host when it renders; say so.
  Otherwise use a text node containing the URL.

## Read-only operations

For status or list requests, read the `.canvas` files and report node counts,
group labels, broken edge endpoints, and missing file targets. If the default
canvas is absent, say so and offer a creation preview; do not create it during
a status request.

## Draft a change

1. Read the whole canvas and any catalog target.
2. Preserve unknown JSON fields and existing array order. The node array is
   bottom-to-top z-order.
3. Draft the requested nodes and edges:
   - every node and edge gets a unique id; prefer a random 16-character
     lowercase hexadecimal id and check it is unused;
   - integer `x`, `y`, `width`, and `height` on every node;
   - `text`, `file`, `link`, or `group` exactly as JSON Canvas defines them;
   - every `fromNode` and `toNode` names an existing or new node.
4. Position new content deliberately. A group is a visual container, not a
   parent. Use 20 px inner padding and 40 px gaps, wrap to a new row when
   needed, and preview any group expansion.
5. Serialize valid UTF-8 JSON with top-level `nodes` and `edges` arrays.

Minimal nodes:

```json
{
  "nodes": [
    { "id": "6f0ad84f44ce9c17", "type": "text", "text": "# Research map\n\nOrientation.", "x": 0, "y": 0, "width": 400, "height": 180 },
    { "id": "a1b2c3d4e5f67890", "type": "file", "file": "wiki/concepts/Contextual Retrieval.md", "x": 460, "y": 0, "width": 400, "height": 240 }
  ],
  "edges": []
}
```

Use `file` plus optional `subpath: "#Heading"` for notes, images, PDFs, and
other vault files. Use `url` only on a `link` node. Escape line breaks in JSON
strings as `\n`.

## Preview and apply

Validate first: JSON parses and uses supported node types; ids are unique and
edge endpoints exist; dimensions are positive integers; file and background
paths are vault-relative and present; new nodes do not overflow their group.

Build one plan of kind `canvas` with the canvas and, when it changes, the
catalog; see [operations.md](../wiki/references/operations.md). Removals,
renames, and replacements need explicit consent after the preview. Report the
operation id, changed paths, board name, node ids, and final positions.
