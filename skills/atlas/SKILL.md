---
name: atlas
description: "Orient in the whole atlas and route: every project and knowledge base, how they connect, what is wrong, the settings. Use for /atlas, my vaults, list vaults, what projects do I have, which knowledge bases, refresh the atlas, atlas settings, set new-days, default change policy, what is wrong with the atlas."
---

# The atlas

Tools: `atlas` and `settings` on the atlas MCP server. Both work from any
session: in a vault, in a project's repository, or in a folder the atlas
does not know.

## See what exists

1. Call `atlas`. It returns every vault with its kind, path, tags or scope,
   access, mounts with `effective` access, repositories with their change
   policy, members, the clusters that hold it, who mounts it, and its state;
   the folders the atlas cannot read, with a reason; and the settings.
2. Show a compact picture: projects with their mounts and repositories, then
   knowledge bases with who mounts them, then clusters with their members.
   Name each problem entry and its reason. Keep it short; the user asked for
   orientation, not a dump.
3. Say what needs attention: a mount whose `effective` is below its
   `access`, a member the scan lost, a repository with no folder.

## Route

| The user wants | Skill |
|---|---|
| Create, rename, retag, or forget a project | `atlas-project` |
| Create a knowledge base or a cluster; change scope, access, or members | `atlas-knowledge` |
| Mount, unmount, read or write, grant, revoke | `atlas-mount` |
| Link, clone, create, or unlink a repository; change how changes land | `atlas-repo` |
| Work inside one vault | `wiki` |

## Settings and refresh

- `settings` with `new_days` or `repo_changes` sets one and returns all;
  with no arguments it reads. Say what a value means before changing it:
  `new_days` is how long a vault counts as new in the view; `repo_changes`
  is the policy a newly linked repository takes, `commit` (on the current
  branch) or `pr` (a branch and a pull request).
- `atlas` with `refresh: true` reads every vault again, rewrites the
  registry, recreates each project's `kb/` links, and adopts repositories
  waiting under `repos/`. Run it after the user moved a folder by hand or
  cloned a repository into `repos/` themselves; report `changes`.

## What stays in the terminal

`claude-atlas doctor` (the installation check), `upgrade`, `recover`,
`setup`, `open-vault`, and `open-claude` are commands, not tools. Name the
command; do not run it through Bash unless the user asks.

Every write here is reversible or leaves the folder alone, so no plan preview
exists; the skill that writes states the change in one line and waits for
yes. Never edit `.claude-atlas.json`, `~/.claude-atlas/config.json`, or
`registry.json` with Write or Edit.
