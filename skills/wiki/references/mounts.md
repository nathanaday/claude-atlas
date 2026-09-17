# Mounts

Read this when the project mounts a knowledge base, or the session is in a
knowledge base.

## What a mount is

A knowledge base a project reads or writes through `kb/<name>`, a symlink to
the knowledge base's `wiki/`. The atlas creates the symlink; `refresh`
recreates a missing or wrong one. A knowledge base never links a project and
never records who mounts it; the atlas computes that list.

## How links resolve

`[[Title]]` resolves to the project's own page first, then to a mount. A name
that matches pages in two mounts is ambiguous: link
`[[kb/<name>/concepts/Title]]` to name the one you mean. A name that matches
the project and a mount is a duplicate; `lint` reports it, and the skill
links to whichever page the change is actually about.

## Access

A knowledge base sets `access`: `open` lets every project that mounts it
write; `guarded` lets only a project named in `grants` with `write` write,
and every other project reads. A project's mount sets its own `access`,
`read` or `write` (`write` is the default). The effective access is the
lesser of the two. A `plan` targeting a mount the project may not write to
is refused: "`<project> mounts <kb> read-only`".

## The `mounts` tool

Lists a project's mounts. Each entry carries `id`, `name`, `path` (the
knowledge base's real path), `link` (`kb/<name>`), `access` (the mount's own
setting), `effective` (the access that actually applies), `scope` (the
knowledge base's one-line scope), `pages`, `error` when the mount could not
be resolved, and `through` (the cluster this mount came from, empty for a
mount the project recorded itself).

## Clusters

A cluster is a knowledge base that gathers others as members. A project
mounts the cluster and reaches every member; the `mounts` tool names the
cluster in `through`. Each member is written exactly like any mount: read
its `path`, write through its `link`, check its own `access`. The cluster's
own wiki holds what is about the domain as a whole, not any one member.

## `route` across mounts

`route`, called in a project, answers for the project and for every mount:
`mounts[]` carries `name`, `vault` (the knowledge base's root), `effective`,
`path` (where a new page would go there), `match` (an existing page by title
or alias), and `error`. A match anywhere means link to it instead of
creating a page (the `next` sentence). Read [operations.md](operations.md)
for the rest of the `route` contract.

## Writing into a knowledge base

From a project session:

- `capture` with `vault` set to the knowledge base's root and `paths` from
  the project's own `inbox/`. The record carries `via`: the project's id and
  name (see [provenance.md](provenance.md)).
- `plan` and `apply` with `vault` set to the knowledge base's root. The
  access check runs at `plan`; `apply` does not check again.
- `stub`, with a title's `target` set to the mount name. The stub commits in
  the knowledge base and its path comes back as `kb/<name>/<path inside the
  knowledge base's wiki>`, readable through the symlink; the operation's
  `vault` in the result still names the knowledge base's own root.

Never write under `kb/` with Write or Edit; the guard hook denies it.

## The CLI commands

`mount PROJECT KB [--read] [--as NAME]` links a knowledge base into a
project. `unmount PROJECT KB|NAME` removes the link; the knowledge base is
untouched. `grant KB PROJECT --read|--write` and `revoke KB PROJECT|ID`
change a guarded knowledge base's `grants`. None of these run from inside a
session; tell the user to run them in a terminal.
