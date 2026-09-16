# Sources and provenance

Keep the source, the page, and the claim distinguishable.

## The source ledger

`wiki/meta/ledgers/source-ledger.json` holds one record per captured source:

```json
"src-3f9c1e2a7b8d4c6e0f12": {
  "title": "DINOv2",
  "origin": { "kind": "file", "locator": ".raw/captured/3f9c…e1.pdf" },
  "content_sha256": "3f9c…e1",
  "content_kind": "pdf",
  "authority": "primary",
  "review_status": "active",
  "captured_at": "2026-09-12",
  "ingested_at": "2026-09-12",
  "pages": ["wiki/sources/DINOv2.md"],
  "via": { "id": "proj-cs566", "name": "cs566" }
}
```

`via` names the project a source came through, and appears only on a record
captured into a knowledge base through a mount. It is provenance, not a link.

The core writes the ledger. `capture` creates records; the `sources` field of a
plan updates them:

```json
"sources": [
  { "id": "src-3f9c1e2a7b8d4c6e0f12", "ingested": true, "pages": ["wiki/sources/DINOv2.md"], "authority": "primary" }
]
```

`ingested: true` sets `ingested_at` and marks the source active. `pages` lists
the wiki pages that came from it. Apply removes a listed page that no longer
exists, so a plan that moves or renames a page lists only the new path. `authority` is one of `official`, `primary`,
`secondary`, `community`, `synthetic`, or `unknown`.

## Source rules

- Capture before you read: a source in `inbox/` becomes durable only as a
  content-addressed copy under `.raw/captured/`. Read that copy.
- Never edit or replace a captured file. A changed source is a new capture.
- Pasted text has no file. Quote it in the page you write and say it was
  supplied in conversation; mark its authority `synthetic` or `unknown`.
- A URL the user gives is a locator, not evidence, until its content is in the
  vault. Do not fetch it unless the user asks and the host allows it.

## Claim rules

Pages carry claims; the ledger carries sources. On a page:

- Cite the source page for every material claim with a wikilink, and the
  locator inside the source (page, section, timestamp) when it exists.
- Keep the source's statements apart from your synthesis.
- Preserve contradictions between sources; do not pick a winner silently.
- Unsupported is a valid state. Write "no source in the vault supports this"
  rather than inventing a quotation, page number, date, or confidence.
- A `question` page's frontmatter `assessment` is `accepted`, `provisional`,
  `contested`, or `unsupported`. Accepted needs at least one active,
  non-synthetic source in the ledger.
