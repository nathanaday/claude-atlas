# Frontmatter conventions

Preserve an existing knowledge base's valid property vocabulary. For a new page, use flat
YAML properties, block lists, and explicit evidence fields where they apply. The
core refuses a wiki page whose frontmatter lacks `title`, `type`, `status`,
`created`, `updated`, or `tags`.

## Common properties

```yaml
---
type: concept
title: "Human-readable title"
created: 2026-09-12
updated: 2026-09-12
status: developing
tags:
  - concept
aliases:
  - Alternative name
related:
  - "[[Related page]]"
sources:
  - "[[Source page]]"
---
```

Types: `source`, `entity`, `concept`, `comparison`, `overview`, `meta`,
`fold`, and in lyt mode `note` and `moc`. The `route` tool returns a skeleton
with the right properties for a type. A project's thread and `phase` pages
live outside the knowledge base; their properties are in [threads.md](threads.md).

Statuses commonly progress `seed`, `developing`, `evergreen`; anything can be
`contested`, `deprecated`, or `archived`. Keep the values a knowledge base
already uses.

## Source page properties

```yaml
source_type: paper
author: ""
date_published: 2023-04-14
url: ""
source_id: src-3f9c1e2a7b8d4c6e0f12
authority: primary
```

`source_id` ties the page to its ledger record. Leave a value empty rather than
inventing it.

## Other type-specific properties

```yaml
# entity
entity_type: organization
first_mentioned: "[[Source page]]"

# entity describing a project (describe)
entity_type: project
project: b3e0f5a2-9c14-4d6e-8a7b-2f1e0c9d8b7a   # the project's id
commit: fc70d93a…               # the commit the page was written from, in full; omitted for a folder that is not a repository

# note (lyt)
mocs:
  - "[[Map of Content]]"
```

## Rules

1. Keep properties flat; no nested mappings.
2. Dates are `YYYY-MM-DD`.
3. Multi-value properties are block lists.
4. Quote wikilinks inside YAML.
5. Update `updated` only when the content or assessed state changes.
6. Preserve unknown valid properties when editing a page.
7. Quote numeric-only tags, for example `- "2026"`.
