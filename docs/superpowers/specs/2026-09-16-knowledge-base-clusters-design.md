# Knowledge base clusters

Date: 2026-09-16. Applies to claude-atlas 1.1.0; ships as 1.2.0.
Builds on `docs/v2-design.md`, which this document extends. Nothing here
changes what a project or a knowledge base already is.

## Goal

A project that needs five knowledge bases mounts one thing instead of five.
The one thing is a cluster: a knowledge base that lists other knowledge bases
as members. Mounting the cluster reaches every member. Adding a member later
reaches every project that mounts the cluster, with nothing to revisit.

The point is not saving five commands. A session in the project asks a
question without naming which knowledge base holds the answer. The cluster
names the domain, the members name the parts, and the skills choose the part
from the scopes. The cluster also has a wiki of its own, which is where
routing notes live now and where pages about the overlap between two members
will live later.

## The entity

A cluster is a knowledge base with a `members` list. A knowledge base with no
members is what exists today. Nothing else distinguishes the two.

```json
{
  "schema": "claude-atlas.vault.v2",
  "id": "1a2b3c4d-…",
  "kind": "knowledge",
  "name": "p3",
  "mode": "generic",
  "created": "2026-09-16",
  "scope": "The moviTHERM ecosystem: the software, the people, the business.",
  "access": "open",
  "members": [
    { "id": "6f1d…", "name": "p3-software" },
    { "id": "a2c4…", "name": "p3-people" }
  ]
}
```

- A cluster is a vault. It has a folder, a git history, an identity file, and
  a `wiki/`. The scan finds it like any other vault, so it travels to another
  machine and needs no entry in the atlas config.
- `members` holds ids and names, like `grants` and `mounts`. The id is the
  truth; the name is for people and for a message when the scan cannot find
  the id. `vault.Member` is `{ID, Name string}`.
- A member does not know it belongs to a cluster, the same way a knowledge
  base never learns who mounts it. The atlas computes that from the clusters'
  identity files.
- `scope` on a cluster describes the whole domain. `scope` on each member
  says what that member covers. The two together are what the ingest skill
  reads to choose a member.
- A cluster's own `wiki/` holds pages like any knowledge base: sources,
  entities, concepts. Today it is where a note about which member covers what
  belongs.

### What the engine refuses

`vault.UpdateConfig` sees one identity file, so it refuses what that file can
answer on its own, before anything is written:

- `members` on a project;
- a member listed twice;
- a cluster as its own member;
- a member with no id.

Whether a member is a knowledge base, and whether it is itself a cluster, are
facts about another vault. `vaults.AddMember` holds the scan, so it refuses
those:

- a member that is not a knowledge base;
- a member that is itself a cluster, so a cluster never nests;
- a member the scan does not hold at all.

A member the scan cannot find is not refused. It is recorded and reported, in
the same way a mount whose knowledge base is gone is reported. A member that
is deleted or moved out of reach leaves an entry the scan cannot resolve;
`list`, `show`, and `doctor` name it, the view marks it, and
`cluster remove NAME ID` drops it by id.

## What the scan resolves

`registry.Entry` gains two fields, both derived:

```go
// Members are a cluster's knowledge bases, resolved against the scan. Error is
// set for a member the scan does not hold.
Members []Ref `json:"members,omitempty"`
// Clusters names the clusters that hold this knowledge base as a member.
Clusters []Ref `json:"clusters,omitempty"`
```

`registry.Mount` gains one field:

```go
// Through is the cluster a mount comes through; "" for a mount the project
// recorded itself. A mount with Through is derived and cannot be unmounted on
// its own.
Through string `json:"through,omitempty"`
```

When the scan resolves a project's mounts and a mount names a cluster, it
appends one derived mount per member after the cluster's own mount, with
`Through` set to the cluster's name. Every part of the atlas that walks
`Entry.Mounts` therefore sees the members without knowing clusters exist:
`EnsureMounts`, lint, the `mounts` tool, the session hook, the view's
connectors, and the two-tier resolver.

Two rules settle the edges:

- **An explicit mount wins.** When a project records a mount for a knowledge
  base that a cluster also reaches, the derived mount is dropped. The project
  asked for that knowledge base by name, so its access and its mount name
  stand.
- **A name collision takes the cluster's name as a prefix.** A derived mount
  is named after its member. When that name is already taken by another mount
  in the same project, the derived mount is named `<cluster>-<member>`. When
  that is taken too, the member's id's first eight characters follow.

## Access

Each vault decides for itself. A member's effective access is
`registry.Effective(request, that member's grant for the project)`, where
`request` is the access the project's cluster mount asked for. The cluster's
own effective access is computed from the cluster's own grant.

One guarded member with no grant is read-only while the rest of the cluster
allows write. Nothing about joining a cluster changes what a knowledge base
grants, and nothing about a cluster's grants reaches its members.

## Symlinks

Mounting a cluster records one mount, the cluster, in the project's identity
file. The symlinks under `kb/` are local state that `mount`, `refresh`, and
`doctor` maintain, so the members appear beside the cluster:

```
kb/p3           -> …/p3/wiki            the cluster's own pages
kb/p3-software  -> …/p3-software/wiki
kb/p3-people    -> …/p3-people/wiki
```

`vaults.EnsureMounts` already creates a link per mount and removes a link no
mount names, so it needs no change once the scan expands the mounts. A member
added to the cluster appears at the next `refresh`; a member removed has its
link removed by the same pass.

## Commands

```bash
claude-atlas new-cluster p3 --scope "The moviTHERM ecosystem."
claude-atlas cluster add p3 p3-software
claude-atlas cluster add p3 p3-people
claude-atlas cluster remove p3 p3-people
claude-atlas mount vision-algorithms p3
claude-atlas unmount vision-algorithms p3
```

- `new-cluster NAME` creates a knowledge base with an empty `members` list. It
  takes `--scope`, `--access`, and `--mode`, the options `new-knowledge` takes.
  With no name, in a terminal, it opens the add screen.
- `cluster add NAME KB` and `cluster remove NAME KB` edit the list through
  `vault.UpdateConfig`, so the change is a `setup` operation in the cluster's
  own history. `cluster NAME` with no verb prints the members.
- Any knowledge base becomes a cluster by gaining a member, and stops being
  one by losing its last. There is no separate conversion.
- `mount` and `unmount` do not change. A cluster is a knowledge base, so they
  already take it.
- `unmount PROJECT MEMBER` where the member comes through a cluster is
  refused: "p3-software comes through cluster p3; unmount p3 instead".
- `show` prints a cluster's members, and prints the clusters that hold a
  knowledge base.

## The view

**The Knowledge tab.** A cluster's box carries its name, its access, and
`cluster` in place of the page count on the second line, with the member count
beside it. The connector column, collapsed, reads
`◀╌╌╌╌ 2 projects · 3 members`. Expanded, it lists the projects that mount it
and then the members, each member in the knowledge color with `member`:

```
╭────────────────────────────────╮
│ 🔥 p3   open                   │◀╌╌╌╌ vision-algorithms   write
│ cluster · 3 members            │      p3-software   member
│                                │      p3-people   member
╰────────────────────────────────╯
```

A knowledge base that belongs to a cluster shows `in p3` on its expanded
block, so the relation reads from both sides.

**The Projects tab.** The board is the one place that does not draw a derived
mount as its own row. A project that mounts a cluster with five members would
otherwise stand six lines tall. The cluster takes one connector and the
members follow on one dim line under it:

```
│ 🔥 vision-algorithms           │╌╌╌╌▶ p3   write · cluster
│ touched today · 3 tasks open   │      through p3: p3-software, p3-people
```

The member list is clipped to the column like any connector label, ending in
an ellipsis when it does not fit. Everything else that reads `Entry.Mounts`
treats a member as the mount it is.

**Keys.** `M` on a knowledge base opens its members screen: the members as
boxes, `a` adds one from a picker of the knowledge bases that are not members
and not clusters, `x` removes the one under the cursor after asking. The
screen refuses to open on a project. The mount picker that `a` opens on a
project's mounts screen lists clusters with their member count, so mounting a
cluster is the same two keystrokes as mounting a knowledge base.

The add screen's `Mounts` step lists clusters alongside knowledge bases.

## Tools, the hook, and lint

- The `mounts` tool reports `through` on a member row, so a session knows a
  knowledge base came through a cluster and which one.
- `route` needs no change. A project's mounts already include the members, so
  a title resolves across them and a duplicate across two members is reported
  the way a duplicate across two mounts is today.
- `plan` and `apply` check the effective access of the vault they write, which
  is the member's own. No change.
- `capture` records `via`, the project a source came through. A source
  captured into a member through a cluster records the project, not the
  cluster. The cluster is how the project reached it, not where it came from.
- The session hook's `Knowledge:` lines name a member with its cluster:
  `p3-software (write, through p3)`.
- Lint resolves links through every symlink under `kb/`, members included.
  `doctor` reports a member the scan cannot find, and a member that is itself
  a cluster, which only a hand-edited file can produce.

## Skills

- `skills/wiki/references/mounts.md` gains a section on clusters: what one is,
  that a member is reached like any mount, and that the cluster's own wiki is
  where a note about the members belongs.
- `wiki-ingest` chooses the knowledge base for a source by reading the scopes.
  The step gains one sentence: when the mounts include a cluster, the members
  are the candidates and the cluster's own wiki takes what is about the domain
  as a whole rather than one part.
- `wiki-query` searches every mount already. It gains one sentence: name the
  member a passage came from in the citation, not the cluster.

## What is left for later

- **Bridge pages.** A page in the cluster's wiki about the overlap between two
  members, created and maintained by a skill. The cluster's own wiki exists
  for this; nothing is built yet.
- **Daisy chaining.** A cluster as a member of another cluster. It needs cycle
  detection and a rule for folding access across levels.
- **Intelligent routing.** A tool that picks the member for a question rather
  than a skill reading scopes.
- **`promote`.** Moving a page from one member to another, or from a member up
  into the cluster.

## Decisions

| Question | Decision |
|---|---|
| A new vault kind, or a knowledge base with members | a knowledge base with members; no third kind |
| Where a cluster is defined | its own identity file, found by the scan; never the atlas config |
| What a project records | the cluster, by id; the members are derived |
| Access across a cluster | each member decides for itself, as if mounted directly |
| Where the member symlinks go | flat under `kb/`, beside the cluster's own |
| A name a mount already uses | the derived mount takes `<cluster>-<member>` |
| A knowledge base mounted directly and through a cluster | the explicit mount wins |
| A cluster inside a cluster | refused for now |
| Unmounting a member | refused; the cluster is what the project mounted |
