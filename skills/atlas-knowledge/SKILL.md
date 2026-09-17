---
name: atlas-knowledge
description: "Create a knowledge base or a cluster step by step: name, location, a scope written well, open or guarded, members. Also change scope, access, or members. Use for new knowledge base, create a knowledge base, new cluster, make a cluster, knowledge base for papers, change the scope, make it guarded, add a member, drop a member."
---

# Create or change a knowledge base

Tools: `atlas`, `vault`, `cluster` on the atlas MCP server. A knowledge base
holds sources, entities, and concepts, and nothing else: no inbox, no
tasks. Knowledge enters through a project that mounts it. A cluster is a
knowledge base with members; a project that mounts the cluster reaches every
member.

Call `atlas` first: the knowledge bases that exist, their scopes, and the
names already taken.

## Ask, in this order

1. **Name.** It becomes the folder's name.
2. **Location.** Default `<vaults dir>/knowledge/<name>`, or a path.
3. **Scope.** Two sentences: what it holds, and what it does not. The ingest
   skill reads every mounted scope to decide where a source belongs, so a
   scope that overlaps another's sends sources to the wrong place. Draft one
   from the user's words, read the existing scopes back, and adjust until
   each is distinct. For a cluster, the scope names the whole domain; the
   members' scopes name the parts.
4. **Access.** `open` (default): every project that mounts it may write.
   `guarded`: a project writes only with a grant, which `atlas-mount` gives.
   Suggest guarded for a knowledge base several projects share and one
   person curates.
5. **Members**, for a cluster: which knowledge bases it gathers. A member is
   a knowledge base, never a project and never another cluster.

## Confirm and create

One line, then yes:

> Create knowledge base `ai-ml` at `~/Documents/Vaults/knowledge/ai-ml`, guarded, scope "Machine learning: models, training, evaluation, agents. Not the projects that use them."?

On yes: `vault` with `action: create`, `kind: knowledge`, `scope`, `access`,
and for a cluster `members`. Report the result. Then say what comes next:
a project mounts it (`atlas-mount`), and sources reach it through that
project's inbox.

## Change a knowledge base

- Scope or access: `vault` with `action: edit`, `target`, and the field. An
  empty `scope` clears it. Changing to `guarded` cuts every mount without a
  grant to read; list the projects `atlas` shows under `mounted_by` before
  the user says yes.
- Members: `cluster` with `action: add` or `remove`. A removed member's
  knowledge base is untouched; the projects that mount the cluster lose it
  at their next refresh.
- Rename or forget: as in `atlas-project`, with the same tools and refusals.

The tool call comes after a yes. Never make the folder or its files yourself.
