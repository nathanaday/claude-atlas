---
name: atlas-mount
description: "Change how a project reaches a knowledge base: mount, unmount, ask for read or write, grant or revoke a project on a guarded knowledge base, add or drop a cluster member. Explains effective access. Use for mount, unmount, read-only mount, make it writable, grant write, revoke, why can't I write to the knowledge base, effective access, add to cluster."
---

# Mounts, grants, and members

Tools: `atlas`, `mount`, `cluster` on the atlas MCP server.

Call `atlas` first and read the project's `mounts` and the knowledge base's
`access`, `grants`, and `mounted_by`. Three things decide what a project may
do in a knowledge base, and `effective` is the lesser of them:

- the mount's own `access`: what the project asks for, `read` or `write`;
- the knowledge base's `access`: `open` grants write to everyone, `guarded`
  grants what each project's grant says, or read with none;
- the grant, on a guarded knowledge base: `read` or `write` for one project.

So a mount that asks for write on a guarded knowledge base reads until the
knowledge base grants it write. When the user asks why a write was refused,
name which of the three is the one to change, and change only that.

## Actions

| The user wants | Call |
|---|---|
| Reach a knowledge base from a project | `mount` with `action: mount`, `project`, `knowledge`; `access: read` for read-only; `as` to name the folder under `kb/` |
| Stop reaching it | `mount` with `action: unmount`; the knowledge base is untouched |
| Ask for read or write | `mount` with `action: access`, `access` |
| Let a project write a guarded knowledge base | `mount` with `action: grant`, `knowledge`, `project`, `access: write` |
| Take that back | `mount` with `action: revoke`; a stale grant's id works when the project is gone |
| Gather a knowledge base in a cluster, or drop it | `cluster` with `action: add` or `remove` |

Mounting a cluster reaches every member; `atlas` shows such a mount with
`through` set to the cluster's name, and unmounting means unmounting the
cluster.

## Confirm

State the change in one line and wait for yes:

> Mount `ai-ml` on `sensor-triage` for writing? It is guarded, so the project reads until `ai-ml` grants it write; I can do that next.

After the call, report the mount's `effective` access, or the grants. If it
is still below what the user wanted, say which of the three to change and
offer to. On a grant for an `open` knowledge base, say it changes nothing
until the knowledge base is guarded.

An unmount, a revoke, and a member removal leave every folder alone. The
project's `kb/<name>` link goes with the mount; a folder there that holds
files is refused, and the tool says so.
