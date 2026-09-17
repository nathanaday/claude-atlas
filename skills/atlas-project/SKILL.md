---
name: atlas-project
description: "Create a project step by step, with suggestions and defaults: name, location, tags, which knowledge bases to mount, a repository now or later. Also rename, retag, or forget one. Use for new project, create a project, make a project for this repo, project in this repository, rename the project, tag the project, forget this vault."
---

# Create or change a project

Tools: `atlas`, `vault`, `mount`, `repo` on the atlas MCP server. A project
is where work happens: an inbox, tasks, questions, ideas, and repositories.
Its knowledge lives in the knowledge bases it mounts.

Call `atlas` first: the vaults directory, the knowledge bases and their
scopes, and the names already taken.

## Ask, in this order

Ask one question at a time. Offer the default; accept a yes.

1. **Name.** When the session sits in a git repository no project holds,
   suggest the repository's folder name. A name becomes the folder's name.
2. **Location.** Default: `<vaults dir>/projects/<name>`. When the session
   sits at a repository's top level, offer `in_repo`: the project goes to
   `REPO/atlas/`, its history lives in the repository's git, and the
   repository becomes the project's first repository. Otherwise a path the
   user names.
3. **Tags**, optional. One or two words each; the view groups projects by the
   first.
4. **Knowledge bases to mount.** Read each scope from `atlas` and suggest
   the ones that fit what the project is for. One mount goes on `vault`
   `create` as `mount`; more go through `mount` afterwards. A guarded
   knowledge base needs a grant too; say so and offer `atlas-mount`.
5. **A repository**, now or later: the one the session is in, a URL to clone,
   or a new one. Later is fine; `atlas-repo` does it any time.

## Confirm and create

State the whole change in one line, in the user's words:

> Create project `sensor-triage` at `~/Documents/Vaults/projects/sensor-triage`, tags `usc, fall`, mounting `ai-ml` for writing, linking `~/code/triage` (changes: commit)?

On yes: `vault` with `action: create`, `kind: project`, and the answers;
then `mount` for each further knowledge base; then `repo` with `link`,
`clone`, or `new`. Report each tool's result. Then say how to work there:
`claude-atlas open-claude <name>`, or `cd` into the vault and start `claude`.
A session in a linked repository reaches the project too.

If a tool refuses, say why in the tool's words and ask again for that one
answer; do not retry with a guess. A taken path means adopt or another name.

## Change a project

- Rename: `vault` with `action: edit`, `target`, and `name`. The folder moves
  with the name; a project at `REPO/atlas/` keeps its folder. Say so.
- Retag: `edit` with `tags`; an empty list clears them.
- Forget: `vault` with `action: forget`. The folder stays. A project inside
  the vaults directory cannot be forgotten, because the scan finds it there;
  the tool says so, and the answer is to move or delete the folder by hand.

Every question comes before the tool call, and the tool call comes after a
yes. Never make the folder or its files yourself.
