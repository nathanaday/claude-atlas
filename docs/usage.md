# Usage

Every command, with examples. `claude-atlas help` prints the short form.

## Two layers

`claude-atlas` on its own opens the whole atlas as one interactive screen
(`claude-atlas view` says the same explicitly). Everything it does is also
one command, so scripts and muscle memory both work:

| In `view` | Command |
|---|---|
| `n` new vault | `new-vault` |
| `a` adopt a vault | `adopt` |
| `e` edit a project, `s` save | `edit NAME --…` |
| `e` then `r` remove | `remove NAME` |
| Enter on a project | `show NAME` |
| `o` open in Obsidian | `open-vault NAME` |
| `c` start Claude Code | `open-claude NAME` |
| `i` ingest sources | `ingest NAME [PATH...]` |
| `l` repositories: create, link, edit, unlink | `new-repo NAME REPO`, `link NAME PATH`, `edit-link PAGE`, `unlink`, `links` |
| `t` tasks, `p` plant, `c` continue | `tasks NAME`, `plant NAME TEXT`, `open-claude NAME --task ID` |
| `T` every project's tasks | `tasks` |
| Related in the editor | `relate NAME OTHER`, `unrelate` |
| `R` refresh | `refresh` |

## Create a vault

With no arguments, `new-vault` asks for a name, a category, a location, and a
one-line purpose:

```bash
claude-atlas new-vault
claude-atlas new-vault sensor-triage --category work --purpose "Sort field sensor faults."
claude-atlas new-vault reading --mode lyt
claude-atlas new-vault ~/Desktop/scratch-vault --category work
```

A new vault goes in the vaults directory, in the folder of its category, so
the folders on disk match the tree: `sensor-triage` in `work` goes to
`~/Documents/Vaults/work/sensor-triage`. To put a vault somewhere else, give a
path instead of a name, or type another location on the add screen. Nothing
depends on where a vault is.

A vault cannot go inside another vault. If a category leads there, choose
another category or a path.

## Edit a project

A project page holds your intent for a vault. Set any field from the command
line; an empty value clears a text field, and `--category ""` moves the page to
the top level.

```bash
claude-atlas edit sensor-triage --priority high --state blocked --blocked-on "field hardware"
claude-atlas edit sensor-triage --review-after 2026-10-01 --done "Every sensor fault has a page."
claude-atlas edit sensor-triage --name "Sensor triage" --purpose "" --category work/field
claude-atlas edit sensor-triage --vault ~/Vaults/sensor-triage --move
```

`--move` moves the vault's folder to the new path. Repositories inside the
vault move with it, and their pages follow.

A new category moves the project page. If the vault sits in its category's
folder, `edit` asks whether to move the vault to the new category's folder
too, and `--move` answers yes. The editor in `view` asks the same when you
save. A vault in any other place stays where it is.

See everything the atlas knows about a project, and remove one from the atlas
(the vault stays on disk):

```bash
claude-atlas show sensor-triage
claude-atlas remove sensor-triage
```

## Work in a vault with Claude Code

Put a source in the inbox and start Claude Code inside the vault:

```bash
cp ~/Downloads/dinov2.pdf ~/Documents/Vaults/work/sensor-triage/inbox/
claude-atlas open-claude sensor-triage
```

In the session, the skills are on the slash menu:

| Skill | What it does |
|---|---|
| `/claude-atlas:wiki` | orient in the vault and route to the right skill |
| `/claude-atlas:task` | list open tasks, move one between statuses, route |
| `/claude-atlas:task-plant` | plant a task from a sentence or the notes in `inbox/tasks/` |
| `/claude-atlas:task-plan` | ask what matters, choose an approach, write the plan |
| `/claude-atlas:task-run` | work the plan and write progress |
| `/claude-atlas:task-finish` | close as done or cancelled and archive |
| `/claude-atlas:wiki-ingest` | read what is in the inbox and write cited pages |
| `/claude-atlas:wiki-query` | answer from the vault, with citations |
| `/claude-atlas:save` | keep an answer or decision as a page |
| `/claude-atlas:wiki-lint` | check the wiki's health |
| `/claude-atlas:wiki-mode` | read or change the filing mode |
| `/claude-atlas:wiki-fold` | roll up log entries |
| `/claude-atlas:canvas` | create and update Obsidian Canvas boards |
| `/claude-atlas:obsidian-bases` | draft Bases `.base` views |
| `/claude-atlas:obsidian-markdown` | Obsidian syntax help |
| `/claude-atlas:think` | a structured review before a consequential change |

Claude shows a preview of every change before it applies it. Each applied
change is one git commit in the vault.

## Tasks

A task is a page in the vault, `wiki/tasks/<Title>.md`, with a status that
moves from `planted` through `planned`, `active`, and `blocked` to `done` or
`cancelled`. Finished tasks move to `wiki/tasks/archive/`. The core keeps the
task ledger and `wiki/tasks/tasks.md` from the pages; a session started in the
vault sees the open tasks at its start, so a task lives across sessions.

Plant a task without ceremony, from anywhere:

```bash
claude-atlas plant sensor-triage "Check the trust dialog on resume"
claude-atlas plant sensor-triage "Write the fault taxonomy" --priority high --workdir ~/code/sensor-triage
```

Or drop a note into the vault's `inbox/tasks/` folder; the next session's
`/claude-atlas:task-plant` turns each note into a task and removes the note.

See what is open, in one vault or in every project:

```bash
claude-atlas tasks sensor-triage
claude-atlas tasks                       # every project, outside a vault
claude-atlas tasks sensor-triage --all   # with the archive
```

Work a task in a session. The skills carry the ceremony: `task` lists and
routes, `task-plan` asks what changes the plan and writes it, `task-run`
works the plan and writes progress at every stopping point, `task-finish`
closes the task and archives it. `open-claude --task` starts the session in
the task's workdir, with the vault selected and `task-run` as the first
message:

```bash
claude-atlas open-claude sensor-triage --task task-20260913-3f2a
claude-atlas open-claude sensor-triage --task "Write the fault"   # a title prefix works
```

In `view`, `t` on a project shows its open tasks as boxes: `p` plants one,
`c` continues the selected task in Claude Code, `o` opens its page in
Obsidian. `T` shows every project's open tasks on one board. The overview
lists open tasks across projects, and signals blocked tasks and stale ones,
active but untouched for 14 days.

`upgrade` brings a vault made by an older version to the current layout. It
adds the task folders, the task index, and the vault's CSS snippet, and it
moves the task index from `wiki/tasks/index.md` to `wiki/tasks/tasks.md`:

```bash
claude-atlas upgrade sensor-triage
claude-atlas upgrade --all
```

The snippet, `.obsidian/snippets/claude-atlas.css`, colors the file explorer
by kind of place: the wiki and each of its folders, `inbox/`, `ideas/`, and
every mounted repository beside the wiki in its own color, so the split
between memory and deliverables shows at a glance. Upgrade enables it in the
vault's appearance settings; reload Obsidian to see it.

## A repo reaches its vault

A session started in a repository that a project links uses that project's
vault: the atlas tools resolve it, and
the session hook says so and lists the open tasks, the ones whose workdir is
that folder first. Nothing is written into the repository. If two projects
link the same folder, pass `vault` to the tools or set `CLAUDE_ATLAS_VAULT`.

## Ingest a folder of sources

Point a project at a file or folder outside its vault. What is new is copied
into the vault's `inbox/`; the originals stay where they are. A file whose
bytes the vault already holds, ingested earlier or still waiting in the inbox,
is skipped, so a folder that grows over time can be ingested again and only its
new files cost anything. The vault remembers the folders it staged from, and an
`ingest` with no path stages what is new in every one of them. Source folders
are not links; nothing about them is recorded in the atlas.

```bash
claude-atlas ingest sensor-triage ~/Papers
claude-atlas ingest sensor-triage ~/Papers/dinov2.pdf
claude-atlas ingest sensor-triage                # every folder ingested before
claude-atlas ingest sensor-triage ~/Papers --dry-run
claude-atlas ingest sensor-triage ~/Papers --no-claude
```

After staging, the command offers to start Claude Code with
`/claude-atlas:wiki-ingest` as its first message, so the review and the
apply happen in the session. `--no-claude` stages and stops. Nothing is
ingested until that session runs the skill; files stay in `inbox/` until then,
and an `ingest` with nothing new still offers to start the session on them.

The first time Claude Code opens a vault it asks whether you trust the folder,
with `No, exit` selected. Choose `Yes`; pressing Enter on the default quits.

## History and undo

```bash
claude-atlas history sensor-triage
claude-atlas undo sensor-triage ingest-20260912-150405-ab12
```

Inside a vault, the vault argument can be omitted:

```bash
cd ~/Documents/Vaults/work/sensor-triage
claude-atlas history
claude-atlas lint
```

## Health check

```bash
claude-atlas lint sensor-triage
claude-atlas lint sensor-triage --json
claude-atlas lint sensor-triage --strict     # exit 1 when there are findings
```

## Filing mode

`generic` files pages by type into `wiki/sources/`, `entities/`, `concepts/`,
`questions/`, and `sessions/`. `lyt` keeps atomic notes in `wiki/notes/` and
navigates them through Maps of Content in `wiki/mocs/`.

```bash
claude-atlas mode sensor-triage
claude-atlas mode sensor-triage lyt
```

Changing the mode affects future pages only.

## Recover

If an operation was interrupted, the vault says so at the next session start.
Restore it:

```bash
claude-atlas recover sensor-triage
```

## Open in Obsidian

Opens a project's vault, or the atlas with no argument. If Obsidian does not
know the folder yet, the command offers to register it; Obsidian quits and
relaunches so it sees the new entry.

```bash
claude-atlas open-vault sensor-triage
claude-atlas open-vault
```

## The atlas

`claude-atlas` with no command, or `claude-atlas view`, is an interactive
tree of every project, three category layers at a time. Space folds or unfolds the branch under the cursor; `-` and `+` fold and
unfold every category. Enter shows everything the atlas knows about a project,
`o` opens its vault in Obsidian, `c` starts Claude Code in it, `i` ingests a
file or folder into it, `l` shows its linked repos and folders, and `e` edits
its page: name, purpose, category, priority, state, what it is blocked on, a
review date, what finished looks like, its vault path, and related projects,
or `r` to remove it from the atlas. `n` creates a vault, `a` adopts
one, and `R` refreshes every vault in the background. Removing never touches
the vault on disk.

Every field that takes a path completes it as a shell does: Tab accepts the
match shown in grey, the arrow keys cycle the others.

```bash
claude-atlas
claude-atlas view
```

`refresh` reads every vault and linked folder and rewrites `Overview.md`,
`Tree.md`, and `categories/`. Run it after editing anything under `tree/`.

```bash
claude-atlas refresh
```

`list` prints every project with its heat, priority, and state.

```bash
claude-atlas list
```

### Mount repositories

A project has two kinds of place. The vault is memory: the wiki, the hot
cache, the tasks. A repository is where the deliverables are made: the code,
the paper, the slides, the report, in whatever structure the work needs. A
link mounts a git repository on a project. Nothing is copied. Each gets a page
in the atlas, `repos/<name>.md`, holding its path; the project page links the
page, so a repository two projects share is one node in the graph. Refresh
reports the branch, uncommitted changes, and last commit, and a commit counts
as touching the project. A session started inside a mounted repository uses
the project's vault.

Create a repository for a project, clone one from GitHub, or mount one that
exists:

```bash
claude-atlas new-repo sensor-triage paper                     # in the vault's folder, beside the wiki
claude-atlas new-repo sensor-triage app --at ~/code/sensor-app
claude-atlas link sensor-triage https://github.com/you/sensor-app        # cloned beside the wiki
claude-atlas link sensor-triage git@github.com:you/sensor-app.git --at ~/code/sensor-app
claude-atlas link sensor-triage ~/code/sensor-triage           # an existing repository
claude-atlas link sensor-triage ~/Documents/cs566-work --init  # a plain folder becomes one first
claude-atlas link field-notes sensor-triage                    # the page repos/sensor-triage.md
claude-atlas links sensor-triage
claude-atlas links                                             # every repository and who uses it
claude-atlas unlink sensor-triage sensor-triage
```

**How changes land.** A repository with a remote gets a change policy, kept
on its page in the atlas as `changes`: `pr` means a session works on a
branch and opens a pull request, never pushing to the default branch;
`commit` means it commits on the current branch. `link` asks when it mounts
or clones a repository with a remote, or takes `--changes pr|commit`; with
no answer the default is `pr` when there is a remote and `commit` when there
is none. A session started inside the repository is told the policy at its
start, and the `repos` tool repeats it. Change it later:

```bash
claude-atlas edit-link sensor-app --changes commit
claude-atlas edit-link sensor-app --remote https://github.com/you/sensor-app
```

A repository created in the vault's folder gets its own git history, and the
vault's `.gitignore` names it, so the vault's commits never include it;
Obsidian still shows it. A plain folder is refused until you agree to
initialize a repository there, which commits what the folder holds. Unlinking
leaves the repository and its page alone; a page no project links shows up as
a signal on the overview until you link it again or delete it.

Rename a page or point it at a repository that moved. Every project that
links the page is rewritten.

```bash
claude-atlas edit-link sensor-datasheets --name "Sensor datasheets"
claude-atlas edit-link Course --path ~/Documents/CS566/Course
```

In `view`, `l` shows the project's repositories, one box per repository with
the page name, the path, what the last refresh found, and the other projects
that share it. `n` creates one: a name, then a location that defaults to the
vault's folder. `a` links one that exists, by path with Tab completion, by
the name of an existing page, or by a URL, which is cloned to a location you
confirm; it asks before initializing git in a plain folder, and asks how
changes should land when the repository has a remote. `e` edits the page's
name, path, remote, and change policy. `u` unlinks after asking. Each
action takes effect at once and refreshes the atlas in the background. In
Obsidian, type `[[` in the `repos` property of a project page and pick a
page.

Pages under `materials/` come from before links were repositories. Refresh
moves the ones whose folder is a git repository under `repos/` and signals the
rest: initialize git there and refresh, or unlink them.

### Relate projects

Record that two projects belong together. The relation lives on one page as
`related: ["[[tree/work/other|other]]"]`; the other page shows it as a
backlink, and the atlas reads both directions.

```bash
claude-atlas relate sensor-triage field-notes
claude-atlas unrelate sensor-triage field-notes
claude-atlas show sensor-triage                     # Related, and Related from
```

In `view`, edit the project (`e`) and add to Related from a list of the other
projects. In Obsidian, type `[[` in the `related` property.

### The graph

Every page in the atlas vault is a node in Obsidian's graph view, so the atlas
draws itself: `Tree.md` links the top-level categories, each page under
`categories/` links its subcategories and projects, each project links the
pages under `repos/` it uses and the projects it relates to. Refresh
regenerates `Tree.md` and `categories/` from the folders under `tree/`; edits
there are lost. `repos/` is yours, like `tree/`.

On the first refresh, when the atlas has no graph settings yet, refresh writes
defaults: the hub pages `Overview`, `About`, and `Reference` filtered out, and
one color per kind of node. To get them back later, delete
`.obsidian/graph.json` in the atlas and refresh. The filter is
`-path:Overview.md -path:About.md -path:Reference.md`; the groups are
`path:tree/`, `path:categories/ OR path:Tree.md`, and `path:repos/`.

Project pages from before this version held plain paths in `repos`. The
first refresh gives each a page and rewrites the entry as a link; it says
which pages it changed.

### Edit the tree by hand

Under `~/Documents/Atlas/tree/`, every folder is a category and every markdown
file is a project. Make, nest, and move them in Obsidian or the shell, then
refresh.

```bash
mv ~/Documents/Atlas/tree/capstone.md ~/Documents/Atlas/tree/university/cs566/
claude-atlas refresh
```

A project page's properties are what the atlas reads: `vault`, `priority`
(high, normal, low, someday), `state` (active, paused, blocked, archived),
`blocked_on`, `review_after`, `purpose`, `definition_of_done`, `repos` (links
to pages under `repos/`), and `related` (links to other project pages). The
body is yours.

## Adopt an existing vault

Turn an Obsidian vault, or a vault made with claude-obsidian, into a
claude-atlas vault and register it. It gains an identity file and git history;
nothing in it is replaced.

```bash
claude-atlas adopt                                   # step by step
claude-atlas adopt ~/Documents/MyKnowledgeVault --category personal
claude-atlas adopt ~/Documents/OldVault --name "Old Vault" --priority someday --mode lyt
```

## Setup and health

```bash
claude-atlas setup
claude-atlas setup --atlas-vault ~/Documents/Atlas --vaults-dir ~/Documents/Vaults --first-vault research
claude-atlas setup --plugin-source ~/SoftwareProjects/claude-atlas   # install the plugin from a checkout
claude-atlas setup --no-plugin
claude-atlas doctor
claude-atlas info
claude-atlas version
```

## Scripts

Apply a plan file without Claude. The file has the shape of the `plan` tool's
arguments; `content_file` may replace `content`.

```bash
claude-atlas apply sensor-triage plan.json
```

## Configuration

`~/.claude-atlas/config.json`:

```json
{
  "schema": "claude-atlas.config.v1",
  "vaults_dir": "/Users/you/Documents/Vaults",
  "atlas_vault": "/Users/you/Documents/Atlas",
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
sets one and refreshes the overview.

| Setting | Effect |
|---|---|
| `claude_code.prompt` | a first message sent on every `open-claude`, for example `/claude-atlas:wiki` |
| `claude_code.args` | flags for `claude`, such as `--model` |
| `claude_code.session_context` | whether the session-start hook hands Claude the vault's `hot.md` |
| `plugin.source` | where `claude plugin marketplace add` gets the plugin: a GitHub slug or a local path |
| `heat.new_days` | how many days after its creation a vault shows as ✨ new whatever its activity; 7 by default, 0 turns it off |
| `--home DIR`, `CLAUDE_ATLAS_HOME` | use a different home instead of `~/.claude-atlas` |
| `-y`, `--yes` | answer yes to every prompt |
