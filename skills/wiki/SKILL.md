---
name: wiki
description: "Orient in a claude-atlas session and route work to the right skill. Use for /wiki, set up wiki, vault status, what is in this knowledge base, which skill should I use, adopt this vault, Obsidian vault, second brain, persistent wiki, knowledge base setup."
---

# Orientation

Atlas has two things. A **knowledge base** is an Obsidian vault: `inbox/`
for sources you have not processed, `ideas/` for the user's own scratch
notes, `.raw/captured/` for immutable copies of ingested sources, and
`wiki/` for the pages: sources, entities, and concepts. Every change to
`wiki/` is one reviewed operation and one git commit. A **project** is a
folder named `atlas/` inside the user's work, a repository or a folder of
documents. It holds `project.json`, `tasks/`, `phases/`, and `inbox/` for task
notes. It has no git of its own and no engine. A project uses one knowledge
base; a knowledge base serves many projects.

The atlas MCP server (tools named `mcp__plugin_claude-atlas_atlas__<tool>`,
called `status`, `plan`, `apply`, and so on below) is the only write path
into a knowledge base.

## Find the place

Call `status` first. In a project session it reports `kind: project`, the
project's id, name, description, and path, its knowledge base (or why none
resolves), the page that describes it there, and its task counts. In a
knowledge base session it reports `kind: knowledge`, the mode, scope, page
count, files waiting in the inbox, git state, warnings, and the projects that
use it. The session hook's first line already names the place:
`claude-atlas: project …` or `claude-atlas: knowledge base …`.

If `status` fails because the session is in neither, hand off: `atlas-project`
makes the current folder a project, `atlas-knowledge` creates a knowledge base
or adopts an existing Obsidian vault, and `atlas` shows what exists.

Do not create knowledge base files yourself. If `status` warns that an
operation was interrupted, tell the user to run `claude-atlas recover` before
anything else.

## Never write wiki pages directly

Write, Edit, MultiEdit, and NotebookEdit are refused under a knowledge base's
`wiki/` by a hook. Read pages with Read, Grep, and Glob as usual; change them
only through `plan` and `apply`. The core writes `wiki/log.md` and the source
ledger itself; a plan that names either is rejected.

A project's task and phase pages are different: their prose is the model's to
write with Edit. Only `atlas/tasks/tasks.md` and `atlas/project.json` are
refused; the `task`, `phase`, and `project` tools change those.

## Route the request

| Intent | Skill |
|---|---|
| Process files in the knowledge base inbox, or supplied text, into pages | `wiki-ingest` |
| Answer from what the knowledge base already holds | `wiki-query` |
| Keep a specific answer, decision, or insight | `save` |
| Check the knowledge base's health | `wiki-lint` |
| Read or change the filing mode | `wiki-mode` |
| Roll up log entries | `wiki-fold` |
| Describe a project in its knowledge base, or bring its page up to date | `describe` |
| Make a change now: do this, implement, fix this | `work` |
| See, move, review, or route tasks; create or change a phase | `task` |
| Note an idea as a task, or plant the notes in a project's inbox | `task-plant` |
| Decide how to do a task | `task-plan` |
| Work on a task, or resume one | `task-run` |
| Close a task as done or cancelled | `task-finish` |
| Work with an Obsidian Canvas | `canvas` |
| Author a Bases `.base` view | `obsidian-bases` |
| Obsidian syntax questions | `obsidian-markdown` |
| Reason carefully before a consequential change | `think` |
| See every knowledge base and project, refresh, or change a setting | `atlas` |
| Make this folder a project; link, unlink, rename, or forget one | `atlas-project` |
| Create a knowledge base, or change its scope or mode | `atlas-knowledge` |

Query is read-only. Keeping an answer is a separate `save` operation the user
asks for. Never update the hot cache merely because a session ended.

In a project session, the wiki tools act on the project's knowledge base.
In a knowledge base session, the task tools take `project` to reach any
project that uses it; the hook's `Projects:` line names them.

## The operation contract

Read [operations.md](references/operations.md) before any change to a
knowledge base. In short:

1. Read every page you will change and keep its `sha256` from the plan preview
   or compute it; a page that changed since you read it makes apply fail
   closed.
2. Call `plan` with the kind, a one-line summary, and every write as complete
   file content. The core validates paths, frontmatter, JSON, and links, and
   returns a `plan_id`, a preview, and warnings.
3. Show the user the preview: created, updated, and removed paths, and every
   warning. Ask before apply when the change removes or replaces pages.
4. Call `apply` with the `plan_id`. Report the operation id and changed paths.

Every new canonical page joins `wiki/index.md` (generic mode) or a MOC (lyt
mode) in the same plan. Update `wiki/overview.md` only when the stable
high-level picture changed. Keep `wiki/hot.md` under 500 words.

An operation can be undone with the `undo` tool or `claude-atlas undo`; say so
when a user hesitates rather than skipping a review.

## Conditional references

Read only what the request needs:

- [operations.md](references/operations.md) for the plan and apply contract;
- [tasks.md](references/tasks.md) for a project's task and phase pages;
- [provenance.md](references/provenance.md) when a source enters or a claim
  needs support;
- [frontmatter.md](references/frontmatter.md) when defining or adopting page
  properties;
- [modes.md](references/modes.md) for domain scaffolds on top of the mode;
- [css-snippets.md](references/css-snippets.md) for requested visual changes;
- [plugins.md](references/plugins.md) when evaluating optional Obsidian
  plugins.

## Think, verify, grow

Before applying, pause once: observe the current state, verify the evidence,
then choose the smallest reversible operation that satisfies the request.
Afterward, report uncertainty and the next useful improvement without doing
it unasked.
