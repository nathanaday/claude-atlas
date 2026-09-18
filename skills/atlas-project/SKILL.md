---
name: atlas-project
description: "Make the current folder a project, or change one: name, description, which knowledge base it uses; link, unlink, rename, describe, forget. Use for new project, init here, make this a project, project for this repo, link the knowledge base, unlink, rename the project, forget this project."
---

# Make or change a project

Tools: `atlas`, `project` on the atlas MCP server. A project is a folder
named `atlas/` inside the user's work, a repository or a folder of
documents. It holds the project's identity, its tasks, its phases, and an
inbox for task notes. It has no git of its own: when the work is a
repository, the repository tracks `atlas/` like any other folder. It uses
one knowledge base.

Call `atlas` first: the knowledge bases and their scopes, and the projects
that exist.

## Make one

Ask one question at a time. Offer the default; accept a yes.

1. **Where.** The current folder, when the session is in the work. Otherwise
   the path the user names. Never a folder inside a knowledge base or inside
   another project.
2. **Name.** Default: the folder's name.
3. **Description.** One sentence saying what the project is. Draft it from the
   folder's README or CLAUDE.md when there is one, and read it back.
4. **Knowledge base.** Read each scope from `atlas` and suggest the one that
   fits. None is a valid answer; the task skills work without one, and
   `link` adds one later.

State the whole change in one line. When the folder is in no git repository,
init makes it one, with no commit; say so in the line:

> Make `~/code/webapp` the project `webapp`, "The customer-facing web application for the fire-detection product", using `product-x`?

On yes: `project` with `action: init`, `work`, `name`, `description`, and
`knowledge`; add `no_git` when the user wants no repository. It writes
`atlas/`, lists the folder in the atlas config, and reports `git`: created,
existing, or enclosed.
Report the result, then offer `describe`, which writes the project's page in
the knowledge base, and say how to work: a session anywhere inside the work
is the project's session.

If the tool refuses, say why in the tool's words and ask again for that one
answer; do not retry with a guess.

## Change one

- Link or unlink: `project` with `action: link` and `knowledge`, or
  `action: unlink`. Linking changes nothing in either folder; it is one
  field in `atlas/project.json`.
- Rename or describe: `project` with `action: edit`, `name` or
  `description`. The folder does not move; the work folder is the user's.
- Forget: `project` with `action: forget`. The folder and its `atlas/`
  stay. Deleting `atlas/` is how a project ends, and that is the user's to
  do by hand.

In a knowledge base session, `work` names the project by name; in a project
session it is implied. Every question comes before the tool call, and the
tool call comes after a yes. Never make the folder or its files yourself.
