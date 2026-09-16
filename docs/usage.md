# Usage

Every command, with examples. `claude-atlas help` prints the short form.

## Two layers

`claude-atlas` on its own opens the whole atlas as one interactive screen
(`claude-atlas view` says the same explicitly). Everything it does is also
one command, so scripts and muscle memory both work:

| In `view` | Command |
|---|---|
| `n` new project, `N` new knowledge base | `new-project NAME [--in REPO]`, `new-knowledge NAME` |
| `a` adopt a vault | `adopt PATH` |
| Enter on a vault | `show NAME` |
| `o` open in Obsidian | `open-vault NAME` |
| `c` start Claude Code | `open-claude NAME` |
| `i` ingest sources, on a project | `ingest NAME [PATH...]` |
| `t` tasks on a project, `p` plant, `c` continue | `tasks NAME`, `plant NAME TEXT`, `open-claude NAME --task ID` |
| `T` every project's tasks | `tasks` |
| `l` repositories on a project: `n` new, `a` link, `e` edit, `u` unlink | `new-repo NAME REPO`, `link NAME PATH`, `edit-repo NAME REPO`, `unlink NAME REPO`, `repos [NAME]` |
| `e` edit a vault, `s` save | `edit NAME --…` |
| `e` then `r` forget | `remove NAME` |
| `m` mounts: on a project `a` mount, `u` unmount; on a knowledge base `w` `r` grant, `x` revoke, `a` grant by name | `mount PROJECT KB [--read] [--as NAME]`, `unmount PROJECT KB\|NAME`, `grant KB PROJECT --write\|--read`, `revoke KB PROJECT\|ID` |
| `R` refresh | `refresh` |
| Space folds a folder, `-` and `+` fold and unfold all | — |
| — (no `view` key; run from a `lint` finding) | `stub VAULT [TITLE...] [--type T]` |

## Create a vault

A vault has a kind. A **project** is where the work is: an inbox, tasks,
questions, ideas, and repositories. A **knowledge base** holds sources,
entities, and concepts, and has no inbox and no tasks. The kind never changes.

```bash
claude-atlas new-project sensor-triage
claude-atlas new-project cs566 --tags usc,fall
claude-atlas new-knowledge ai-ml --scope "Machine learning: models, training, evaluation, agents."
claude-atlas new-knowledge field-optics --access guarded
claude-atlas new-project reading --mode lyt
claude-atlas new-project ~/Desktop/scratch --name Scratch
```

With no name, in a terminal, both commands open a screen that asks for the
kind, a name, a filing mode, the tags or the scope, and a location.

A new vault goes in the vaults directory, under `projects/` or `knowledge/`:
`sensor-triage` goes to `~/Documents/Vaults/projects/sensor-triage`, `ai-ml`
to `~/Documents/Vaults/knowledge/ai-ml`. To put a vault somewhere else, give a
path instead of a name, or type another location on the add screen. The atlas
lists a vault outside the vaults directory in its config, because the scan
cannot find it there. Nothing else depends on where a vault is.

A vault cannot go inside another vault.

A project's tags group it in `view`: the first tag is its folder there.
`--tags usc,fall` puts `cs566` under `projects/usc`.

`--access` states a knowledge base's intent for the projects that mount it:
`open` lets every one of them write, `guarded` only the ones it grants. See
"Mount a knowledge base" below. This is hygiene, not security. Any process on
the machine can read the files.

## Edit a vault

```bash
claude-atlas edit cs566 --name "CS 566" --tags usc,fall
claude-atlas edit cs566 --tags ""
claude-atlas edit ai-ml --scope "Machine learning, end to end." --access guarded
```

A project takes `--name` and `--tags`; a knowledge base takes `--name`,
`--scope`, and `--access`. An empty value clears a field. Each edit writes the
vault's identity file and commits it there as a `setup` operation, so the
change is in the vault's own history.

See everything the atlas knows about a vault, and forget one (the folder stays
on disk):

```bash
claude-atlas show cs566
claude-atlas remove cs566
```

`remove` drops a vault from the config. A vault inside the vaults directory
cannot be forgotten: the scan finds it there, so move or delete the folder. A
registered vault whose folder is gone is still listed, as `missing`, and
`remove` takes its path or its folder's name to forget it.

## Work in a vault with Claude Code

Put a source in the inbox and start Claude Code inside the vault:

```bash
cp ~/Downloads/dinov2.pdf ~/Documents/Vaults/projects/sensor-triage/inbox/
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

In a project, the tools see its mounts. Ingest into a knowledge base still
runs in the project session: `capture` with `vault` set to the knowledge
base's root takes a file from the project's inbox and records `via`, the
project it came through; `plan` and `apply` with that `vault` and kind
`ingest` or `save` need the mount to be effectively `write` — a read mount
refuses with "cs566 mounts ai-ml read-only". A title's `target` set to a
mount's name tells `stub` to seed the page inside that knowledge base; `stub`
with `vault` set to the knowledge base's root stubs its own wanted pages
instead. The `mounts` tool lists a project's mounts with their effective
access and page counts.

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
Obsidian. `T` shows every project's open tasks on one board. `show` counts a
project's tasks and signals the blocked ones and the stale ones, active but
untouched for 14 days.

`upgrade` brings a vault made by an older version to the current layout. It
adds the task folders, the task index, and the vault's CSS snippet, and it
moves the task index from `wiki/tasks/index.md` to `wiki/tasks/tasks.md`:

```bash
claude-atlas upgrade sensor-triage
claude-atlas upgrade --all
```

The snippet, `.obsidian/snippets/claude-atlas.css`, colors the file explorer
by kind of place: the wiki and each of its folders, `inbox/`, `ideas/`, and
every linked repository beside the wiki in its own color, so the split
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
`ingest` with no path stages what is new in every one of them. A source folder
is not a linked repository; the atlas records nothing about it.

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
cd ~/Documents/Vaults/projects/sensor-triage
claude-atlas history
claude-atlas lint
```

## Health check

```bash
claude-atlas lint sensor-triage
claude-atlas lint sensor-triage --json
claude-atlas lint sensor-triage --strict     # exit 1 when there are findings
```

## Stubs and wanted pages

A **wanted page** is a title one or more pages link to that nobody has written
yet. Lint reports it, and a session's start line names a few and counts the
rest:

```text
Stubs: 2 pages to fill (Backpropagation, Loss Landscape). Wanted: 1 linked
page does not exist yet (Contrastive Learning). Fill or stub them with the
wiki-lint skill.
```

`stub` creates a seed page for each: frontmatter and the section headings its
type usually carries, left for a session or a person to fill in.

```bash
claude-atlas stub sensor-triage
claude-atlas stub sensor-triage "Backpropagation" "Loss Landscape" --type concept
```

With no title, `stub` seeds every wanted page and every empty page a link
already points to. `--type` sets the type for a title that names none:
concept or entity; in a project also question or session; in `lyt` mode note
or moc as well. The default is concept, or note in `lyt` mode.

The `stub` tool takes the same arguments in a session. A title's `target`
names a mount: the stub lands in that knowledge base instead of the
project's own wiki, and the tool returns its path through the mount,
`kb/<name>/<path inside the knowledge base>`. A stub never lands in a
read-only mount.

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

Opens a vault by name or path, or the vault you are in with no argument. If
Obsidian does not know the folder yet, the command offers to register it;
Obsidian quits and relaunches so it sees the new entry.

```bash
claude-atlas open-vault sensor-triage
claude-atlas open-vault
```

## The atlas

`claude-atlas` with no command, or `claude-atlas view`, is an interactive tree
of every vault the atlas found. Projects sit under `projects/`, grouped by
their first tag; knowledge bases sit under `knowledge/`; a vault the scan
found but could not read sits under `problems` with the reason. Each vault is a
box with its heat, its name, its page count, how much is unfinished, and how
long it has been idle. The tree shows three folder layers at a time; Enter on a
deeper folder opens it and Esc comes back.

| Key | What it does |
|---|---|
| `↑` `↓` | move |
| Enter | on a vault, everything the atlas knows about it; on a folder, fold or unfold |
| Space | fold the folder under the cursor |
| `-` `+` | fold and unfold every folder |
| `n` `N` | create a project, create a knowledge base |
| `a` | adopt a folder as a vault |
| `o` `c` | open the vault in Obsidian, start Claude Code in it |
| `i` `t` `l` | on a project: ingest sources, tasks, repositories |
| `T` | every project's open tasks on one board |
| `e` | edit the vault; `s` saves, `r` forgets it |
| `m` | mounts: mount and unmount on a project; grants on a knowledge base |
| `R` | refresh in the background |
| `q` | quit |

Forgetting a vault never touches the folder on disk, and it works only for a
vault outside the vaults directory.

Every field that takes a path completes it as a shell does: Tab accepts the
match shown in grey, the arrow keys cycle the others.

```bash
claude-atlas
claude-atlas view
```

### How the atlas finds vaults

The atlas keeps no list of your vaults. It walks the vaults directory for
identity files (`.claude-atlas.json`), at most five levels deep, skipping
folders whose name starts with a dot and folders named `node_modules`, and
never looking inside a vault it has found. To that it adds the paths under
`vaults` in the config, for the vaults you keep elsewhere. Every command scans
afresh before it acts.

`refresh` runs the scan, reads each vault, and rewrites
`~/.claude-atlas/state/registry.json`: kind, id, name, path, mode, page count,
heat, last operation, unfinished work, open tasks, and what git says about each
repository. Everything in that file is derived, so deleting it costs one
refresh.

```bash
claude-atlas refresh
```

`list` prints every vault: kind, heat, name, and path. `show` prints
everything the atlas holds about one, with the signals that need attention.

```bash
claude-atlas list
claude-atlas show sensor-triage
```

A command names a vault by its name, by its id or an id prefix of eight
characters or more, or by its path. Two vaults with one name make the command
ask for the path or the id instead.

### Link repositories

A project has two kinds of place. The vault is memory: the wiki, the hot
cache, the tasks. A repository is where the deliverables are made: the code,
the paper, the slides, the report, in whatever structure the work needs.
Linking a repository to a project copies nothing. The project's identity file
records the repository's name, its remote, and how changes land there; the
atlas config holds the path of a repository that sits outside the project's
own `repos/` folder. `refresh` reports the branch, uncommitted changes, and
last commit, and a commit counts as touching the project. A session started
inside a linked repository uses the project's vault.

Create a repository for a project, clone one from GitHub, or link one that
exists:

```bash
claude-atlas new-repo sensor-triage paper                      # in the project's repos/ folder
claude-atlas new-repo sensor-triage firmware --at ~/code/sensor-firmware
claude-atlas link sensor-triage https://github.com/you/sensor-app        # cloned into repos/
claude-atlas link sensor-triage git@github.com:you/sensor-app.git --at ~/code/sensor-app
claude-atlas link sensor-triage ~/code/sensor-app              # an existing repository
claude-atlas link sensor-triage ~/Documents/cs566-work --init  # a plain folder becomes one first
claude-atlas repos sensor-triage
claude-atlas repos                                             # every project's repositories
claude-atlas unlink sensor-triage sensor-app
```

A repository takes the name of its folder, and a project cannot link two of
one name. It goes under the project's `repos/` folder or outside the vault;
anywhere else inside the vault is refused, because the vault's git would take
it in.

**How changes land.** A repository with a remote gets a change policy, kept in
the project's identity file as `changes`: `pr` means a session works on a
branch and opens a pull request, never pushing to the default branch;
`commit` means it commits on the current branch. `link` asks when it links
or clones a repository with a remote, or takes `--changes pr|commit`; with
no answer the default is `pr` when there is a remote and `commit` when there
is none. A session started inside the repository is told the policy at its
start, and the `repos` tool repeats it. Change it later, or point the project
at a repository that moved:

```bash
claude-atlas edit-repo sensor-triage sensor-app --changes commit
claude-atlas edit-repo sensor-triage sensor-app --remote https://github.com/you/sensor-app
claude-atlas edit-repo sensor-triage sensor-app --path ~/code/sensor-app
```

A repository created in the project's folder gets its own git history, and the
vault's `.gitignore` names it, so the vault's commits never include it;
Obsidian still shows it. A plain folder is refused until you agree to
initialize a repository there, which commits what the folder holds. Unlinking
leaves the folder alone.

In `view`, `l` on a project shows its repositories, one box each with the
name, the path, how changes land, and what the last refresh found. `n` creates
one: a name, then a location that defaults to the project's `repos/` folder.
`a` links one that exists, by path with Tab completion, or by a URL, which is
cloned to a location you confirm; it asks before initializing git in a plain
folder, and asks how changes should land when the repository has a remote. `e`
edits the remote, the folder, and the change policy. `u` unlinks after asking.
Each action takes effect at once and refreshes in the background.

### Mount a knowledge base

A project reaches a knowledge base through a mount: `kb/<name>` in the
project, a symlink to the knowledge base's `wiki/`. `mount`, `unmount`,
`grant`, and `revoke` manage it:

```bash
claude-atlas mount cs566 ai-ml
claude-atlas mount cs566 ai-ml --read
claude-atlas mount cs566 field-optics --as optics   # two knowledge bases of one name
claude-atlas unmount cs566 ai-ml
claude-atlas grant ai-ml cs566 --write
claude-atlas grant field-optics cs566 --read
claude-atlas revoke field-optics cs566
```

`mount` creates the symlink and records the mount, by id, in the project's
identity file. `--read` mounts it read-only; the default is write. `--as`
names the mount folder, for a knowledge base whose name collides with an
existing mount. `unmount` drops the record; the knowledge base is untouched.
`refresh` recreates a missing or wrong symlink from the identity file and the
atlas config; `doctor` reports one that needs it.

A knowledge base sets `access`, `open` or `guarded`, at `new-knowledge` or
with `edit --access`. `open` lets every project that mounts it write; a
`guarded` knowledge base lets only a project `grant` names write, and every
other project reads. The access a project gets is the lesser of the mount's
own access and the grant: a write mount on a guarded knowledge base with no
grant still reads only.

In `view`, `m` on a vault opens its mounts screen: on a project, `a` mounts a
knowledge base and `u` unmounts one; on a knowledge base, `w` grants write,
`r` grants read, `x` revokes a grant, and `a` grants a project by name.
When a grant's project is gone, `revoke KB PROJECT` cannot find it by name;
`revoke KB ID` drops it by its id instead.

A `[[link]]` in a project page resolves to a knowledge base page through the
mount, in Obsidian, in the graph, in backlinks, and in lint.

### A project inside a repository

`new-project NAME --in REPO` creates the project at `REPO/atlas/`, tracked by
the repository's own git instead of a history of its own. Every wiki commit
records only files under `atlas/`. It lands on the current branch. Under a
`changes: pr` policy, set with `edit-repo NAME REPO --changes pr`, the wiki
rides the same pull request as the code. `link` and `unlink` do not apply to
the host repository: it is already the project's first repository, and neither
command changes it.

`adopt REPO/atlas --as project` registers a clone: the folder must already hold
the project's identity file. A folder named `atlas` that is not a project yet
is refused, because its pages would join the repository's history; make one
with `new-project NAME --in REPO`.

A session that commits code in the repository should leave the vault out, for
example `git add -- . ':!atlas'`. A page waiting for its operation then stays
out of the code commit.

```bash
claude-atlas new-project --in ~/code/webapp        # the project takes the repository's name
claude-atlas edit-repo webapp webapp --changes pr
claude-atlas adopt ~/code/webapp/atlas --as project   # a clone, on another machine
```

## Adopt an existing vault

Turn a folder into a v2 vault: an Obsidian vault, a vault made with
claude-obsidian, or a vault from v1. It gains an identity file and git
history; nothing in it is replaced. A vault outside the vaults directory is
listed in the config, so the atlas finds it again.

```bash
claude-atlas adopt                                   # step by step
claude-atlas adopt ~/Documents/MyKnowledgeVault --as knowledge
claude-atlas adopt ~/Documents/OldVault --name "Old Vault" --mode lyt
```

`--as` chooses the kind, `project` by default; a vault that already has one
keeps it. Adopting as a knowledge base removes the inbox, the ideas folder,
the tasks, and the task ledger. It commits a baseline of the vault as it is
first, so git holds everything it removes.

## Setup and health

```bash
claude-atlas setup
claude-atlas setup --vaults-dir ~/Documents/Vaults --first-vault research
claude-atlas setup --plugin-source ~/SoftwareProjects/claude-atlas   # install the plugin from a checkout
claude-atlas setup --no-plugin
claude-atlas doctor
claude-atlas info
claude-atlas version
```

Setup shows its plan and asks before it acts: the atlas home, the plugin in
Claude Code, and your first project. `doctor` checks git, Claude Code, the
plugin's version against the binary's, and every vault the scan found. It also
checks a knowledge base's grants: a grant whose project is gone fails, and a
grant on an already open knowledge base is a note, since the grant applies
only once the knowledge base turns guarded.

## Scripts

Apply a plan file without Claude. The file holds the `plan` tool's arguments;
`content_file` may replace `content`.

```bash
claude-atlas apply sensor-triage plan.json
```

## Configuration

`~/.claude-atlas/config.json`:

```json
{
  "schema": "claude-atlas.config.v2",
  "vaults_dir": "/Users/you/Documents/Vaults",
  "vaults": ["/Users/you/SoftwareProjects/foo/atlas"],
  "repos": {
    "b3e0f5a2-0c11-4d8e-9a77-2e6d4f0b1c93/sensor-app": "/Users/you/code/sensor-app"
  },
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
| `vaults_dir` | the folder the scan walks, and where a new vault goes under `projects/` or `knowledge/` |
| `vaults` | vault folders outside `vaults_dir`; the scan cannot find them, so they are listed. `new-project`, `adopt`, and `remove` keep this list |
| `repos` | the path of a repository outside its project's `repos/` folder, keyed by the project's id and the repository's name |
| `claude_code.prompt` | a first message sent on every `open-claude`, for example `/claude-atlas:wiki` |
| `claude_code.args` | flags for `claude`, such as `--model` |
| `claude_code.session_context` | whether the session-start hook hands Claude the vault's `hot.md` |
| `plugin.source` | where `claude plugin marketplace add` gets the plugin: a GitHub slug or a local path |
| `heat.new_days` | how many days after its creation a vault shows as ✨ new whatever its activity; 7 by default, 0 turns it off |
| `--home DIR`, `CLAUDE_ATLAS_HOME` | use a different home instead of `~/.claude-atlas` |
| `-y`, `--yes` | answer yes to every prompt |

These are the only paths the atlas stores. Everything else it shows comes from
the scan or from `state/registry.json`, which `refresh` rewrites in full.
