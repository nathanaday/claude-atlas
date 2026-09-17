---
name: atlas
description: "Orient in the whole atlas and route: every knowledge base and the projects that use it, what is wrong, the settings. Use for /atlas, my vaults, list knowledge bases, what projects do I have, which knowledge bases, refresh the atlas, atlas settings, set new-days, what is wrong with the atlas."
---

# The atlas

Tools: `atlas` and `settings` on the atlas MCP server. Both work from any
session: in a knowledge base, in a project, or in a folder the atlas does
not know.

## See what exists

1. Call `atlas`. It returns every knowledge base with its path, scope, mode,
   state, and the projects that use it; every project with its path,
   description, knowledge base, open task counts, and whether a page
   describes it; the folders the atlas cannot read, with a reason; and the
   settings.
2. Show a compact picture: each knowledge base, then the projects under it,
   then the projects that use no knowledge base. Name each problem entry and
   its reason. Keep it short; the user asked for orientation, not a dump.
3. Say what needs attention: a project whose knowledge base is not on this
   machine, a project no page describes, a registered folder that is gone,
   a blocked or stale task.

## Route

| The user wants | Skill |
|---|---|
| Make this folder a project; link, unlink, rename, or forget one | `atlas-project` |
| Create a knowledge base; change its scope or mode | `atlas-knowledge` |
| Work inside a knowledge base or a project | `wiki` |
| See or change tasks across projects | `task` |

## Settings and refresh

- `settings` with `new_days` sets it and returns all; with no arguments it
  reads. Say what the value means before changing it: how long a knowledge
  base or project counts as new in the view.
- `atlas` with `refresh: true` reads everything again and rewrites the
  registry. Run it after the user moved a folder by hand; a project heals
  its own path when a session starts in it, so refresh is for the view, not
  for the projects.

## What stays in the terminal

`claude-atlas doctor` (the installation check), `upgrade`, `recover`,
`setup`, `relocate`, `open-vault`, and `open-claude` are commands, not
tools. Name the command; do not run it through Bash unless the user asks.

Every write here is reversible or leaves the folder alone, so no plan
preview exists; the skill that writes states the change in one line and
waits for yes. Never edit `.claude-atlas.json`, `atlas/project.json`,
`~/.claude-atlas/config.json`, or `registry.json` with Write or Edit.
