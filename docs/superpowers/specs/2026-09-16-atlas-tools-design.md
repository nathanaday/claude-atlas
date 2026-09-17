# Atlas tools: configuring the atlas from Claude Code

Date: 2026-09-16. Applies to claude-atlas 1.2.0; ships as 1.3.0.
Builds on `docs/v2-design.md` and the clusters design. Nothing here changes
what a vault, a mount, a grant, a cluster, or a repository is.

## Goal

Every configuration task the CLI and the view offer is also possible from a
Claude Code session: create a project or a knowledge base, adopt a vault,
mount and grant, add a cluster member, link a repository, set a change
policy, stage files into an inbox, change a setting. The user asks in words;
a skill asks what it needs, states the change, and calls a tool.

The point is the guided flow. `claude-atlas new-project` asks the same
questions the view asks. A skill can suggest: which knowledge bases to mount
from their scopes, a scope sentence for a new knowledge base, the repository
the user is sitting in. The CLI stays the complete surface and the reference;
the skills make it conversational.

## The rule

CLAUDE.md's "Tool, skill, or hook" applies. The facts and the writes are
tools; the questions, defaults, and order are skills; nothing here needs a
hook, because the identity files are already behind the `PreToolUse` guard.

"Two layers, one backend" becomes three layers. An action lives once, as a
function in `internal/vaults`, `internal/refresh`, `internal/vault`, or
`internal/txn`. The CLI exposes it as one subcommand. The view and the tools
reach it through one struct that `actions.Bind` builds. A new tool gets a
command in the same change, and `docs/usage.md` maps the three.

## The backend: `internal/actions`

`tui.Hooks` and `tui.AddVault` move to a new package `internal/actions`, as
`actions.Atlas` and `actions.AddVault`, with one constructor:

```go
func Bind(h home.Home, cfg *home.Config, c *console.Console) Atlas
```

`Bind` is what `cli.hooks` is today, moved whole: the closures over
`vaults`, `refresh`, `capture`, and `tasks`, and the helpers they call
(`createOrAdopt`, `refreshAll`, `registryEntries`, `planStage`, `stage`,
`plantTask`). The CLI calls `Bind` and keeps none of it. The server
calls `Bind` itself, once per tool call with a config it has just loaded,
because the CLI in another process may change `config.json` between two
calls. `tui` and `mcpserver` import `actions`; neither imports the other,
and `actions` imports neither.

The struct gains one field, `Scan func() (*registry.Index, error)`: a fresh
scan with every entry's state derived and nothing written. Every tool
resolves its names through it, because every command that acts on a vault
scans afresh and `registry.json` is for display only. Two signatures change:
`Refresh` returns the index and each project's changes, and `StagePlan`
takes a list of sources. `AddVault` gains `Access` and `InRepo`, so one
`Create` covers every way the CLI makes a vault.

`vaults.Create` keeps its console preview. `Bind` passes `confirm=false`
for the view already; the server does the same, and the skill is the
preview.

## Tools

Seven tools join the fifteen. Every description is one line, because every
description sits in every session's context. A write tool is a noun and its
`action` field is the verb, matching `mode`, `stub`, and the CLI's own
grouping. Each argument's schema doc names the actions that read it.

A vault is named by `name`, `id`, or `path`. The resolver tries the id, then
the exact name, then the path. Two vaults that share a name are refused with
both paths in the message (`registry.Index.Find`); the skill passes the path
or the id.

The server resolves a vault per call, not at start, so these tools work in a
session started anywhere: in a vault, in a project's repository, or in a
folder the atlas does not know.

### `atlas` (read-only)

```
args:    { refresh?: bool }
returns: { vaults: Entry[], problems: Problem[],
           settings: { vaults_dir, new_days, repo_changes },
           changes?: ProjectChange[] }
```

`Entry` is `registry.Entry` as it is: id, kind, name, path, mode, tags or
scope, access, grants, members, clusters, mounts with `effective`, repos with
their change policy, host, `mounted_by`, and for a vault the atlas cannot
read, `error` and `reason`. Without `refresh` the tool scans and derives and
writes nothing. With `refresh: true` it runs what `claude-atlas refresh` runs:
rewrites `registry.json`, recreates `kb/` symlinks, adopts repositories
waiting under `repos/`, and reports what it adopted in `changes`.

### `vault`

```
action: create | adopt | edit | forget
create: kind, name, path?, mode?, tags?, scope?, access?, in_repo?, mount?, members?
adopt:  path, kind, name?, mode?
edit:   target, name?, tags?, scope?, access?
forget: target
returns: { vault: Entry }  |  { forgotten: path }
```

- `create` defaults `path` to `vaults.PathFor`, `mode` to `generic`, and
  `access` to `open`. `in_repo` is a repository root; the project goes to
  `REPO/atlas/` through `vaults.CreateIn`. `mount` names a knowledge base a
  new project mounts with write, and `members` the knowledge bases a new
  cluster gathers, as the view's add screen does.
- `adopt` takes the folder's name when `name` is empty.
- `edit` renames, retags, rescopes, or changes access through
  `vaults.EditIdentity`; a rename moves the folder and the returned entry
  carries the new path. A field present and empty clears it; a field absent
  is unchanged.
- `forget` is `vaults.Unregister`: it refuses a vault inside the vaults
  directory, because the scan would find it again.
- The refusals are the CLI's: a taken path, a path inside another vault, a
  new kind on edit, a field that does not belong to the kind.

### `mount`

```
action:  mount | unmount | access | grant | revoke
mount:   project, knowledge, access? (write), as?
unmount: project, knowledge (id, name, or mount name)
access:  project, knowledge, access
grant:   knowledge, project, access
revoke:  knowledge, project (name, or a stale grant's id)
returns: { mount: Mount, effective }  |  { grants: Grant[] }
```

Each action calls the `vaults` function the CLI calls. `mount` and `access`
return the mount with its effective access, so the skill can tell the user
when a guarded knowledge base still needs a grant.

### `cluster`

```
action:  add | remove
add:     cluster, knowledge
remove:  cluster, knowledge (id or name)
returns: { members: Ref[] }
```

### `repo`

```
action: link | new | clone | unlink | edit
link:   project, path, init?
new:    project, name, at?
clone:  project, url, at?
unlink: project, name
edit:   project, name, remote?, changes?, path?
returns: { repo: { name, path, remote, changes } }
```

`link`, `new`, and `clone` also return the folder the repository sits in.
The host repository's name is refused by `link`, `new`, and `unlink`, and
`edit` sets its change policy, as the CLI does.

### `settings`

```
args:    { new_days?, repo_changes? }
returns: { vaults_dir, new_days, repo_changes }
```

Sets what is given and returns all three. `vaults_dir` is read-only here;
moving the vaults directory is a terminal task.

### `stage`

```
args:    { project, paths?: string[], dry_run?: bool }
returns: StagePlan, plus StageResult and remembered when applied
```

Empty `paths` means the folders the project staged from before. With
`dry_run` it plans and stops. Otherwise it plans and applies: new files are
copied into `inbox/`, folders are remembered in `.vault-meta/ingest.json`.
This closes the gap where a session could not bring a file from outside the
vault into the inbox: `capture` refuses such a path, and staging had no tool.

## Confirmation

These tools do not use `plan` and `apply`. Nothing they do deletes user data:
`forget`, `unlink`, `unmount`, `revoke`, and cluster `remove` leave every
folder where it is; `create` and `adopt` refuse an existing or occupied path;
identity file changes are `setup` commits in the vault's own git.

One write does widen access: the `mount` tool's `grant` action can give the
session's own project write access to a guarded knowledge base. It moves a
capability the session already had rather than adding one, because a session
with Bash could always run `claude-atlas grant`. The skills' state-then-confirm
gate is what holds it: a grant is stated in one line and waits for yes, like
every other write.

The gate is the skill. Before a write, the skill states the change in one
line and waits for yes, the same confirm the view's screens give. A skill
never writes an identity file, the atlas config, or `registry.json` with
Write or Edit; the guard refuses the identity file, and the others are not in
a vault, so the skills say it in words.

## Skills

Five skills, named like the `wiki` and `task` families: a router and one per
noun. Each is about sixty lines and carries order, defaults, and stopping
points, never a fact the tools return.

### `atlas`

Orient and route. Calls `atlas`, summarizes: how many projects and knowledge
bases, which projects mount what with what effective access, clusters and
their members, repositories and their policies, problems by reason. Sends
the user to `atlas-project`, `atlas-knowledge`, `atlas-mount`, or
`atlas-repo`. Owns the two settings through `settings` and a refresh through
`atlas` with `refresh: true`. For `doctor`, `upgrade`, `recover`, `setup`,
`open-vault`, and `open-claude`, it names the terminal command; those stay
in the CLI.

### `atlas-project`

Create a project, or change one. In order:

1. Name. Suggest one from the folder the session is in when it is a git
   repository and no project holds it.
2. Location. Default the vaults directory; offer `in_repo` when the session
   is in a repository's top level, and name what that means (`REPO/atlas/`,
   the project's git is the repository's).
3. Tags, optional.
4. Knowledge bases to mount, suggested from the scopes `atlas` returned. One
   mount at creation through `create`'s `mount`; more through `mount`
   afterwards.
5. A repository now or later: link the one the session is in, clone a URL, or
   create one.

Then one line that states the whole change, yes, `vault create`, then `mount`
and `repo` as chosen. Report the path and how to start a session there
(`claude-atlas open-claude NAME`, or `cd` and `claude`). Also: rename, retag,
forget, through `vault edit` and `vault forget`.

### `atlas-knowledge`

Create a knowledge base or a cluster, or change one. A cluster is a knowledge
base with members, so one skill. In order: name; location; a scope the skill
helps write, two sentences, what it holds and what it does not, because the
ingest skill chooses a destination by reading it; `open` or `guarded`, with
what guarded means for the projects that mount it; and for a cluster, the
members, passed as `members` on `vault create`. One line, yes, `vault
create`. Also: edit scope and access, add and drop members.

### `atlas-mount`

The access graph after creation. Mount and unmount, read or write, grant and
revoke, cluster membership. Reads `atlas` first and explains `effective`: the
mount's own request, the knowledge base's access, and the grant, and which
one to change. This is where `wiki-ingest` sends the user when a mount is
read-only, instead of a terminal command.

### `atlas-repo`

Link, clone, create, unlink, and change how changes land. Explains `pr`
against `commit` and the atlas default, and what the session hook and the
`repos` tool will say afterwards.

## What the existing skills change

- `wiki`: "no vault here" hands off to `atlas-project` or `atlas-knowledge`
  instead of naming terminal commands.
- `wiki-ingest`: the read-only-mount case hands off to `atlas-mount`; the
  two `claude-atlas` commands it names today go. A source outside the vault
  goes through `stage`, so "ask the user to save the page into `inbox/`"
  becomes "stage it".
- The session-start hook is unchanged.

## Docs and versions

- CLAUDE.md: "Two layers, one backend" becomes "Three layers, one backend"
  and names `internal/actions`; the layout table gains the package; the
  constraints list says the tools bind the same struct as the view.
- `docs/usage.md`: the key-to-command table gains a tool column.
- `docs/core-design.md`: the tools table gains the seven.
- `plugin.json` and `marketplace.json` go to 1.3.0, because skills change.

## Tests

- `mcpserver`: in-process over the in-memory transport, with a temp atlas
  home and real vaults. For each tool: the happy path and the refusals the
  CLI has (taken path, inside a vault, a vault inside the vaults directory
  on `forget`, the host repository's name, a duplicate name that lists both
  ids). `stage` with `dry_run` writes nothing.
- `actions`: one test that `Bind` sets every field, so a field added to
  the struct cannot ship unbound.
- `tui` tests change only their import.
- Skills: by hand with `claude -p` inside a vault, in a project's repository,
  and in a folder the atlas does not know, as CLAUDE.md prescribes.

## What is left for later

- `doctor` as a tool. It is a hundred lines of installation checks whose
  output is for a person, and nothing a skill decides depends on it yet.
- Deciding which knowledge base a source belongs to in code. The scopes are
  free text; the ingest skill reads them. Real findings first.
- The `search` tool, unchanged from the open questions.

## Decisions

- Nouns with an `action` field, not one tool per verb. Seven descriptions in
  every session instead of seventeen, and the skill carries the procedure.
- No `plan`/`apply` for atlas changes. Nothing deletes; the skill's one-line
  statement is the preview.
- `tui.Hooks` and its builder move out of `tui` and `cli` rather than the
  server importing `tui` or binding `vaults` a second time. The struct is
  the contract that keeps three layers on one function, and `Bind` is the
  one place it is built.
- `atlas` without `refresh` writes nothing. A read tool that wrote the
  registry would be the second place a scan leads to a write.
- `doctor`, `upgrade`, `recover`, `setup`, and the launchers stay CLI. They
  are installation and repair, not configuration.
