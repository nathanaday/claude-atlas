---
name: wiki-lint
description: "Run the deterministic, read-only health check on the knowledge base. Use for lint, vault health, audit wiki health, find orphans, find dead links, frontmatter audit, wiki audit. Reports links, orphans, index gaps, frontmatter, empty sections, and ledger problems; it does not repair files."
---

# Lint the wiki

The `lint` tool is the source of truth. It observes; it never writes. It runs
on the knowledge base in scope: the session's own, or a project's.

## Run

Call `lint` (optionally with `exclude` globs such as `wiki/scratch/*`). The
report lists, with exact paths and lines:

| Category | Meaning |
|---|---|
| `dead_links` | wikilinks, embeds, and markdown links whose target, heading, or block is missing |
| `ambiguous_targets` | a basename that matches more than one page |
| `duplicate_basenames` | two pages with the same name in different folders |
| `orphans` | pages nothing links to (links from `log.md` do not count) |
| `unindexed_pages` | pages missing from `index.md` and every MOC |
| `missing_frontmatter` | pages lacking required properties |
| `empty_sections` | headings with nothing under them |
| `stale_index_entries` | index links that do not resolve |
| `read_errors` | pages that could not be parsed, including invalid frontmatter YAML |
| `ledger_errors` | source records whose files or pages are missing |
| `kind_errors` | a folder from a v2 project vault, such as `wiki/tasks/`: move its pages under `wiki/` or remove it |

Report only what the tool found. It does not judge prose, style, or
contradictions. A project's task pages are not linted here; the `tasks` tool
reports a task page it cannot read.

A `wanted_pages` entry, a page a link names but nobody has written, is not a
finding; the `stub` tool seeds it.

## Explain

1. Preserve the tool's paths, lines, targets, and counts.
2. Group by impact: broken navigation, ambiguous resolution, metadata quality,
   then maintainability.
3. Say when an orphan may be intentional and when an ambiguous basename needs a
   folder-qualified link. Do not infer intent from a finding alone.

Return the explanation in chat. Do not write a report into the knowledge base.

## Repair is a separate operation

Never fix findings unasked. When the user picks findings to repair:

1. Read each affected page.
2. Draft only the selected changes. Deleting or merging pages needs explicit
   consent.
3. Build one plan of kind `repair`, following
   [operations.md](../wiki/references/operations.md).
4. Show the preview, then apply after the user agrees.
5. Run `lint` again and compare the relevant findings.

The `wiki-lint` agent can run and interpret the report on your behalf; it never
repairs.
