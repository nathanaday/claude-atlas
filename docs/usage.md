# Usage

Every command, with examples. `claude-atlas help` prints the short form.

## Three layers

`claude-atlas` on its own opens the whole atlas as one screen (`claude-atlas
view` says the same explicitly). Everything it does is also one command, and
one tool from a Claude Code session, so scripts, muscle memory, and the
`atlas` skills all work:

| In `view` | Command | Tool, from a Claude Code session |
|---|---|---|
| Enter on an entry | `show NAME` | `atlas`, `status` |
| `o` open a knowledge base in Obsidian | `open-vault KB` | — |
| `c` start Claude Code in a knowledge base or a project | `open-claude NAME [--task ID]` | — |
| `p` plant a task on a project | `plant PROJECT TEXT` | `plant` |
| `R` refresh | `refresh` | `atlas` with `refresh` |
| `←` `→` switch tabs; `h` shows every key; `q` quits | — | — |
| — | `new-knowledge PATH`, `adopt PATH`, `edit KB`, `remove KB` | `vault` |
| — | `init`, `link KB`, `unlink`, `forget PROJECT` | `project` |
| — | `describe PROJECT` | `stage`, then the `describe` skill |
| — | `ingest KB [PATH...]` | `stage`, then the `wiki-ingest` skill |
| — | `tasks [PROJECT]`, `task PROJECT ID …`, `phase PROJECT …` | `tasks`, `task`, `phase` |
| — | `config new-days N` | `settings` |
| — | `stub KB [TITLE...]` | `stub` |
| — | `lint`, `history`, `undo`, `mode`, `recover`, `upgrade`, `apply` | `lint`, `history`, `undo`, `mode` |
| — | `setup`, `doctor`, `info`, `version` | — |

## Two things

A **knowledge base** is an Obsidian vault with an inbox, sources, entities,
and concepts. Every change to it is one reviewed operation and one git
commit. A **project** is a folder named `atlas/` inside your work, holding
tasks, phases, and an inbox for task notes. It has no git of its own. A
project uses one knowledge base; a knowledge base serves many projects.

## Create a knowledge base

```bash
claude-atlas new-knowledge ~/Vaults/product-x --scope "The thermal fire-detection product line: cameras, firmware, alarm pipeline, false-alarm sources and mitigations."
cd ~/Vaults && claude-atlas new-knowledge reading --mode lyt
claude-atlas new-knowledge ~/Desktop/notes --name Notes
```

A knowledge base goes where you say. A path is used as given; a bare name is
a folder in the current directory, as `init` treats its path. The atlas
lists the folder in its config; nothing else depends on where it is.
`new-knowledge` refuses a folder that exists, and a folder inside another
knowledge base.

A knowledge base may go inside a project, for a project that keeps its
knowledge to itself. Its history goes into the git repository that holds
it: inside a project that is a repository, every operation is a commit in
the project's repository, on its current branch, and touches only the
knowledge base's folder. A knowledge base in no repository gets one of its
own. `new-knowledge` refuses a folder the holding repository ignores,
because no commit could record it. The hint after it names `link` for the
project that holds it.

The scope is one or two sentences: what the knowledge base holds and what it
does not. A project session reads it to know what belongs there. Split a
knowledge base only at a trust boundary, work and personal, private and
shared; few and large beats many and small.

## Edit, show, and forget a knowledge base

```bash
claude-atlas edit product-x --name "Product X" --scope "The thermal fire-detection product line, end to end."
claude-atlas show product-x
claude-atlas remove product-x
```

Each edit writes the identity file and commits it in the knowledge base as a
`setup` operation. **A new name moves the folder.** The leaf changes and the
parent stays; the folder takes the name cleaned for a path, so `Product X:
Notes` files as `Product X- Notes` while the name keeps the colon. A taken
folder refuses the whole edit.

`remove` drops a knowledge base from the config; the folder stays. A session
started in the folder lists it again.

## Make a project

```bash
cd ~/code/webapp
claude-atlas init                                   # name: the folder's; asks for a description and a knowledge base
claude-atlas init --name "Web App" --knowledge product-x --description "The customer-facing web application."
claude-atlas init --no-knowledge                    # ask nothing
claude-atlas init ~/code/notes --no-git             # another folder, and no repository
```

`init` writes `atlas/` in the current folder, with `project.json`, `tasks/`,
`tasks/archive/`, `phases/`, and `inbox/`, and lists the folder in the atlas
config. When the folder is in no git repository, `init` runs `git init` there
on `main` and commits nothing; `--no-git` skips that. It leaves a repository
as it is, and it makes no repository in a folder inside another one. The
repository tracks `atlas/` like any other folder, on the branch you are on. In a terminal with no flags it asks for the description
and offers the knowledge bases the atlas knows, with none as a choice. It
refuses a folder that already has `atlas/` without a `project.json`, and a
folder inside another project or inside a knowledge base.

Then, from inside the work:

```bash
claude-atlas link product-x                         # set the knowledge base
claude-atlas unlink                                 # clear it
claude-atlas link product-x --project webapp        # from anywhere
claude-atlas forget webapp                          # drop it from the config; atlas/ stays
claude-atlas describe webapp                        # stage a snapshot for the describe skill
```

Deleting `atlas/` is how a project ends.

**A moved project heals itself.** The config holds the work folder's path.
Move the folder, then start a session in it: the hook finds the project's id
listed at another path and rewrites the entry. A clone on another machine is
added the same way. Until then the view shows the project as missing.

**Describe the project.** A project is invisible to its knowledge base until
a page describes it. `describe` writes a snapshot of the work into the
knowledge base's inbox, then offers to start Claude Code with
`/claude-atlas:describe`, which reads the snapshot and the work and writes
`wiki/entities/<name>.md`: what the project is, how it is built and laid
out, what it has delivered, the concepts it introduces. The snapshot holds
the work's CLAUDE.md and README, its files (folders only past 2000), the
first heading of every markdown file under `docs/`, and, once a page exists,
the git log since that page's commit. The session hook says when the page is
missing or has fallen behind, and `task-finish` offers the update when a
task lands.

## Tasks and phases

A task is a page, `atlas/tasks/<Title>.md`, with a status that moves from
`planted` through `planned`, `active`, and `blocked` to `done` or
`cancelled`. Finished tasks move to `atlas/tasks/archive/`. The core renders
`atlas/tasks/tasks.md` from the pages, grouped by phase; a session started in
the work sees the open tasks at its start, so a task lives across sessions.

Plant a task without ceremony, from anywhere:

```bash
claude-atlas plant webapp "Filter vehicle false alarms"
claude-atlas plant webapp "Write the fault taxonomy" --priority high --phase "Alarm quality" --due 2026-10-01
```

Or drop a note into the project's `atlas/inbox/` folder; the next session's
`/claude-atlas:task-plant` turns each note into a task and removes the note.

A phase is a named slice of the timeline: a page under `atlas/phases/` with a
goal and an order. Tasks name their phase; a phase never lists its tasks.

```bash
claude-atlas phase webapp create "Alarm quality" --goal "False alarms under 1 per camera-day." --order 1
claude-atlas phase webapp rename "Alarm quality" --to "Alarm precision"
claude-atlas phase webapp reorder "Alarm precision" --order 2
claude-atlas phase webapp remove "Alarm precision"      # refused while a task names it
```

See what is open, change a task, and finish one:

```bash
claude-atlas tasks webapp
claude-atlas tasks                                  # every project
claude-atlas task webapp task-20260917-3f2a --status blocked
claude-atlas task webapp "Filter vehicle" --phase "Alarm quality" --priority high
claude-atlas task webapp task-20260917-3f2a --status done   # moves the page to archive/
```

The status is the truth and the folder follows it. The tools change the
frontmatter; the Plan, Progress, and Outcome sections are prose Claude writes
with its ordinary editing tools, and you may edit any page by hand in any
editor. Only `tasks.md` and `project.json` are off limits to the model.

Work a task in a session. The skills carry the ceremony: `task` lists,
routes, and reviews, `task-plan` asks what changes the plan and writes it,
`task-run` works the plan and writes progress at every stopping point,
`task-finish` closes the task, archives it, and offers the knowledge base
what the work taught. `open-claude --task` starts the session in the work
with `task-run` as the first message:

```bash
claude-atlas open-claude webapp --task task-20260917-3f2a
claude-atlas open-claude webapp --task "Filter vehicle"      # a title prefix works
```

`show` counts a project's tasks and signals the blocked ones and the stale
ones, active but untouched for 14 days.

## Work in a session

A session started anywhere inside a project's work is the project's session,
except inside a knowledge base in the work, which is the knowledge base's.
Its start says which project this is, which knowledge base it uses, whether
a page describes it there, and what tasks are open:

```text
claude-atlas: project webapp at ~/code/webapp (git, main)
Knowledge: product-x · The thermal fire-detection product line … · 140 pages · ~/Vaults/product-x
This project has no page in product-x; the describe skill writes it.
Search the knowledge base (the wiki-query skill) before answering from the code alone. Change wiki pages only through the atlas MCP tools (plan, then apply).
Open tasks: 4 (active 1, blocked 0, planned 1, planted 2) in 2 phases. Continue one with the task-run skill; see them all with the tasks tool.
- [active] Filter vehicle false alarms (task-20260917-3f2a) · Alarm quality · high · updated 2026-09-17
```

A session started in the knowledge base sees every project that uses it:

```text
claude-atlas: knowledge base product-x (generic mode) at ~/Vaults/product-x
Projects: webapp (~/code/webapp, 4 open tasks), firmware (~/code/fw, 0 open tasks). Plant into one with the task tools; read its work through its path.
Not yet described here: firmware; the describe skill writes the page.
Inbox: 2 sources waiting; the wiki-ingest skill files them.
```

In the session, the skills are on the slash menu:

| Skill | What it does |
|---|---|
| `/claude-atlas:wiki` | orient and route to the right skill |
| `/claude-atlas:work` | take a change from a sentence to a task with a plan; from a knowledge base, one per project it touches |
| `/claude-atlas:task` | list open tasks by phase, move one, create or change a phase, review the list |
| `/claude-atlas:task-plant` | plant a task from a sentence, the notes in `atlas/inbox/`, or into any project from its knowledge base |
| `/claude-atlas:task-plan` | ask what matters, choose an approach, write the plan |
| `/claude-atlas:task-run` | work the plan and write progress |
| `/claude-atlas:task-finish` | close as done or cancelled, archive, offer the knowledge base what was learned |
| `/claude-atlas:describe` | describe a project in its knowledge base from a snapshot, or bring its page up to date |
| `/claude-atlas:wiki-ingest` | read what is in the inbox and write cited pages |
| `/claude-atlas:wiki-query` | answer from the knowledge base, with citations |
| `/claude-atlas:save` | keep an answer or decision as a page |
| `/claude-atlas:wiki-lint` | check the knowledge base's health |
| `/claude-atlas:wiki-mode` | read or change the filing mode |
| `/claude-atlas:wiki-fold` | roll up log entries |
| `/claude-atlas:canvas` | create and update Obsidian Canvas boards |
| `/claude-atlas:obsidian-bases` | draft Bases `.base` views |
| `/claude-atlas:obsidian-markdown` | Obsidian syntax help |
| `/claude-atlas:think` | a structured review before a consequential change |
| `/claude-atlas:atlas` | every knowledge base and project, refresh, settings |
| `/claude-atlas:atlas-project` | make this folder a project; link, unlink, rename, forget |
| `/claude-atlas:atlas-knowledge` | create a knowledge base, change its scope, adopt a vault |

Claude shows a preview of every change to the knowledge base before it
applies it. Each applied change is one git commit there. In a project
session, the wiki tools act on the project's knowledge base, and a source
captured from there records the project as provenance. In a knowledge base
session, `plant`, `tasks`, `task`, `phase`, and `stage` take `project` to
reach any project that uses it.

## Ingest sources

Put a source in the knowledge base's inbox and start Claude Code there:

```bash
cp ~/Downloads/paper.pdf ~/Vaults/product-x/inbox/
claude-atlas open-claude product-x
```

Or point the atlas at a file or folder outside the knowledge base. What is
new is copied into `inbox/`; the originals stay where they are. A file whose
bytes the knowledge base already holds is skipped, so a folder that grows
over time can be ingested again and only its new files cost anything. The
knowledge base remembers the folders it staged from, and an `ingest` with no
path stages what is new in every one of them.

```bash
claude-atlas ingest product-x ~/Papers
claude-atlas ingest product-x ~/Papers/paper.pdf
claude-atlas ingest product-x                # every folder ingested before
claude-atlas ingest product-x ~/Papers --dry-run
claude-atlas ingest product-x ~/Papers --no-claude
```

After staging, the command offers to start Claude Code with
`/claude-atlas:wiki-ingest` as its first message, so the review and the
apply happen in the session. `--no-claude` stages and stops. Nothing is
ingested until that session runs the skill.

The first time Claude Code opens a folder it asks whether you trust it, with
`No, exit` selected. Choose `Yes`; pressing Enter on the default quits.

## History and undo

```bash
claude-atlas history product-x
claude-atlas undo product-x ingest-20260912-150405-ab12
```

Inside a knowledge base or a project, the name can be omitted; a project
resolves to its knowledge base:

```bash
cd ~/Vaults/product-x
claude-atlas history
claude-atlas lint
```

## Health check

```bash
claude-atlas lint product-x
claude-atlas lint product-x --json
claude-atlas lint product-x --strict     # exit 1 when there are findings
```

## Stubs and wanted pages

A **wanted page** is a title one or more pages link to that nobody has
written yet. Lint reports it, and a session's start line names a few and
counts the rest:

```text
Stubs: 2 pages to fill (Backpropagation, Loss Landscape). Wanted: 1 linked page does not exist yet (Contrastive Learning). Fill or stub them with the wiki-lint skill.
```

`stub` creates a seed page for each: frontmatter and the section headings its
type usually carries, left for a session or a person to fill in.

```bash
claude-atlas stub product-x
claude-atlas stub product-x "Backpropagation" "Loss Landscape" --type concept
```

With no title, `stub` seeds every wanted page and every empty page a link
already points to. `--type` sets the type for a title that names none:
concept or entity; in `lyt` mode note or moc as well. The default is concept,
or note in `lyt` mode.

## Filing mode

`generic` files pages by type into `wiki/sources/`, `entities/`, and
`concepts/`. `lyt` keeps atomic notes in `wiki/notes/` and navigates them
through Maps of Content in `wiki/mocs/`.

```bash
claude-atlas mode product-x
claude-atlas mode product-x lyt
```

Changing the mode affects future pages only.

## Recover and upgrade

If an operation was interrupted, the knowledge base says so at the next
session start. Restore it:

```bash
claude-atlas recover product-x
```

`upgrade` brings a knowledge base made by an older version to the current
layout: it raises a v2 identity file to v3, dropping access and grants, and
adds the template files the vault lacks, `inbox/` and `ideas/` among them.

```bash
claude-atlas upgrade product-x
claude-atlas upgrade --all
```

## Open in Obsidian

Opens a knowledge base by name or path, or the one you are in with no
argument. If Obsidian does not know the folder yet, the command offers to
register it; Obsidian quits and relaunches so it sees the new entry.

```bash
claude-atlas open-vault product-x
claude-atlas open-vault
```

## The atlas

`claude-atlas` with no command, or `claude-atlas view`, is one screen with
two tabs. The Knowledge tab lists every knowledge base with its scope, page
count, inbox count, and the projects that use it. The Projects tab lists
every project grouped by knowledge base, with its path, open task count,
current phase, and a mark when the path is missing. Enter expands an entry
in place with what `show` prints. The view is for seeing what exists and
getting there; creating and changing things is the CLI's and the session's
job.

| Key | What it does |
|---|---|
| `←` `→` | previous tab, next tab |
| `↑` `↓` | move |
| Enter | expand the entry under the cursor, or collapse it |
| `o` | open the knowledge base in Obsidian |
| `c` | start Claude Code in the knowledge base or the project |
| `p` | plant a task on the project |
| `R` | refresh in the background |
| `h` | show every key; again to hide them |
| `q` | quit |

```bash
claude-atlas
claude-atlas view
```

### How the atlas finds things

The atlas never searches your disk. The config lists every knowledge base's
folder under `knowledge` and every project's work folder under `projects`,
and the scan reads the identity file in each listed folder. `new-knowledge`
and `adopt` write a knowledge base's entry and `remove` drops it; `init`
writes a project's entry and `forget` drops it. A session started in a
knowledge base or a project the config does not list adds it, and one
started in a folder that moved heals the entry by id. Every command scans
afresh before it acts.

`refresh` runs the scan, reads each entry, and rewrites
`~/.claude-atlas/state/registry.json`: every knowledge base with its page
count, heat, inbox count, and projects; every project with its knowledge
base, task counts, phases, the page that describes it, and what git says
about the work. Everything in that file is derived, so deleting it costs one
refresh.

```bash
claude-atlas refresh
```

`list` prints every entry: kind, heat, name, and path. `show` prints
everything the atlas holds about one, with the signals that need attention.

```bash
claude-atlas list
claude-atlas show webapp
```

A command names a knowledge base or a project by its name, by its id or an id
prefix of eight characters or more, or by its path. Two entries with one name
make the command ask for the path or the id instead.

## Adopt an existing vault

Turn a folder into a knowledge base: an Obsidian vault, a vault made with
claude-obsidian, or a knowledge base from v1 or v2. It gains an identity file
and git history; nothing in it is replaced. The config lists it, so the
atlas finds it again.

```bash
claude-atlas adopt ~/Documents/MyKnowledgeVault
claude-atlas adopt ~/Documents/OldVault --name "Old Vault" --mode lyt --scope "…"
```

A v2 project vault is refused. v3 projects are folders in the work: run
`init` there, and delete the old vault by hand once nothing in it is wanted.

## Setup and health

```bash
claude-atlas setup
claude-atlas setup --first-vault ~/Vaults/notes
claude-atlas setup --plugin-source ~/projects/software/claude-atlas   # install the plugin from a checkout
claude-atlas setup --no-plugin
claude-atlas doctor
claude-atlas info
claude-atlas version
```

Setup shows its plan and asks before it acts: the atlas home, the plugin in
Claude Code, and your first knowledge base, at the path you give or in a
folder named after it in the current directory. `doctor` checks git, Claude
Code, the plugin's version against the binary's, and every knowledge base and
project the config lists, naming the ones whose folder is
gone or whose knowledge base is not on this machine.

## Scripts

Apply a plan file without Claude. The file holds the `plan` tool's arguments;
`content_file` may replace `content`.

```bash
claude-atlas apply product-x plan.json
```

## Configuration

`~/.claude-atlas/config.json`:

```json
{
  "schema": "claude-atlas.config.v3",
  "knowledge": ["/Users/you/Vaults/product-x", "/Users/you/elsewhere/papers"],
  "projects": ["/Users/you/code/webapp", "/Users/you/code/fw"],
  "plugin": {
    "id": "claude-atlas@nathanaday-claude-atlas",
    "source": "nathanaday/claude-atlas"
  },
  "claude_code": {
    "command": "claude",
    "session_context": true
  },
  "heat": {
    "new_days": 7
  }
}
```

`claude-atlas config` prints the settings; `claude-atlas config new-days 14`
sets one and refreshes.

| Setting | Effect |
|---|---|
| `knowledge` | every knowledge base's folder. `new-knowledge`, `adopt`, `remove`, a rename that moves a folder, and the session hook keep this list |
| `projects` | every project's work folder. `init`, `forget`, and the session hook keep this list |
| `claude_code.prompt` | a first message sent on every `open-claude`, for example `/claude-atlas:wiki` |
| `claude_code.args` | flags for `claude`, such as `--model` |
| `claude_code.session_context` | whether the session-start hook hands Claude a knowledge base's `hot.md` |
| `plugin.source` | where `claude plugin marketplace add` gets the plugin: a GitHub slug or a local path |
| `heat.new_days` | how many days after its creation an entry shows as new whatever its activity; 7 by default, 0 turns it off |
| `--home DIR`, `CLAUDE_ATLAS_HOME` | use a different home instead of `~/.claude-atlas` |
| `-y`, `--yes` | answer yes to every prompt |

These are the only paths the atlas stores. Everything else it shows comes from
the scan or from `state/registry.json`, which `refresh` rewrites in full.
