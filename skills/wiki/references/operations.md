# Operations

Use this reference whenever a skill will change a knowledge base.

## Contract

One logical change is one operation: one `plan`, one preview the user sees,
one `apply`, one git commit. Parallel workers read and draft; only the
orchestrator plans and applies.

The tools are on the atlas MCP server, named
`mcp__plugin_claude-atlas_atlas__<tool>`. Every tool acts on the one knowledge
base in scope: the session's own, or the project's. None takes a vault
argument.

| Tool | Use |
|---|---|
| `status` | the place: a project with its knowledge base, or a knowledge base with its projects; mode, inbox, git state, warnings |
| `inbox` | files waiting in the knowledge base's `inbox/`, with hashes and capture state; in a project session, also `notes`, the notes that wait to become threads |
| `capture` | copy inbox files into `.raw/captured/` and the source ledger; a project session records the project as provenance |
| `route` | where a new page of a type belongs, whether it exists, and a skeleton |
| `plan` | validate writes and hold them; returns `plan_id`, preview, warnings |
| `apply` | commit a held plan |
| `undo` | revert an applied operation |
| `history` | recent operations |
| `lint` | the health check |
| `mode` | read or prepare a change of filing mode |
| `stub` | seed a page for every wanted link, or for `titles[]` (each `title` and `type`); `type` sets the default for titles that name none |
| `stage` | copy files from outside into `inbox/`, or write a project's snapshot there |
| `threads`, `thread`, `phase` | a project's threads and phases; see [threads.md](threads.md) |

## Workflow

1. Call `status`. Stop if no knowledge base is in scope or it needs recovery.
2. Read every target page with Read. Draft complete new content; a write
   replaces the whole file.
3. Call `plan`:

```json
{
  "kind": "save",
  "summary": "Save the false-alarm mitigation as a concept page",
  "writes": [
    { "path": "wiki/concepts/Vehicle false alarms.md", "mode": "create", "content": "---\n..." },
    { "path": "wiki/index.md", "mode": "replace", "content": "---\n...", "base_sha256": "<sha256 of the index you read>" },
    { "path": "wiki/hot.md", "mode": "replace", "content": "---\n..." }
  ],
  "sources": []
}
```

   `mode` is `create`, `replace`, or `delete`. `base_sha256` is optional: when
   given it must match the file as you read it; when omitted the plan pins the
   file as it is now. Either way, apply refuses if the file changes afterwards.

4. Show the user the preview and warnings. Warnings name links that do not
   resolve, empty sections, and new pages that no index or MOC links to. Fix
   what you can with a new plan, or explain why the warning is acceptable.
5. Call `apply` with the `plan_id`. Report the operation id and changed paths.

A plan is single-use, and the newest plan for a knowledge base replaces older
ones. If `apply` says the plan is gone, plan again.

## Kinds and their scope

The kind bounds what a plan may write. The core rejects anything outside it.

| Kind | May write |
|---|---|
| `ingest` | `wiki/**`; may also `delete` a file under `inbox/` once it is captured |
| `save`, `markdown`, `repair`, `fold` | `wiki/**` |
| `stub` | `wiki/**`, only through the `stub` tool; `plan` refuses this kind |
| `canvas` | `wiki/canvases/**/*.canvas` and `wiki/canvases/canvases.md` |
| `base` | `wiki/**/*.base` |
| `config` | only through the `mode` tool |

Never writable: `wiki/log.md` (the core writes the entry from your summary),
`wiki/meta/ledgers/source-ledger.json` (use the `sources` field), `ideas/`
(the user's), `.raw/`, `.git/`, `.vault-meta/`, `.obsidian/`, and
`.claude-atlas.json`.

## Content rules the core enforces

- A wiki page starts with YAML frontmatter carrying `title`, `type`, `status`,
  `created`, `updated`, and `tags`.
- `.json` and `.canvas` files parse as JSON; `.base` files parse as YAML.
- A create must not exist; a replace or delete must exist.
- At most 256 writes per plan; 64 MiB per file.

## Coupled writes

- Save: the note, the index or MOC, the hot cache.
- Ingest: pages, the index or MOC, the hot cache, the `sources` entries, the
  inbox removal, and the overview only when the big picture changed.
- Fold: the fold page and the index.
- Canvas: the canvas, plus its catalog only when the catalog changes.
- Repair: the approved fixes only.
- Query: nothing. Persistence is a separate `save`.

## Failure behavior

- A `conflict` error means a target changed after you read it or after the
  plan was made. Read it again and plan again.
- A validation error means nothing was written.
- An interrupted apply is restored from git by the next `status` or by
  `claude-atlas recover`. The user runs recovery; tell them when it is
  needed.
- Hand edits the user made in Obsidian are committed as `manual` operations
  before yours runs. They are never lost and never mixed into your commit;
  `apply`'s result names that commit in `manual_commit` when one ran ("N
  pages changed by hand").

## Undo

`undo` reverts one operation as a new commit. It fails when a later operation
overlaps the same lines; then propose a `repair` plan instead.
