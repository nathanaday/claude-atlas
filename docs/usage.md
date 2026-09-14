# Usage

Every command, with examples. `claude-atlas help` prints the short form.

## Two layers

`claude-atlas view` is the whole atlas as one interactive screen. Everything it
does is also one command, so scripts and muscle memory both work:

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
| `R` refresh | `refresh` |
| Repos and Materials in the editor | `link`, `unlink`, `links` |

## Create a vault

With no arguments, `new-vault` asks for a name, a category, and a one-line
purpose:

```bash
claude-atlas new-vault
claude-atlas new-vault sensor-triage --category work --purpose "Sort field sensor faults."
claude-atlas new-vault reading --mode lyt
claude-atlas new-vault ~/Desktop/scratch-vault
```

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

See everything the atlas knows about a project, and remove one from the atlas
(the vault stays on disk):

```bash
claude-atlas show sensor-triage
claude-atlas remove sensor-triage
```

## Work in a vault with Claude Code

Put a source in the inbox and start Claude Code inside the vault:

```bash
cp ~/Downloads/dinov2.pdf ~/Documents/Vaults/sensor-triage/inbox/
claude-atlas open-claude sensor-triage
```

In the session, the skills are on the slash menu:

| Skill | What it does |
|---|---|
| `/claude-atlas:wiki` | orient in the vault and route to the right skill |
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

## Ingest a folder of sources

Point a project at a file or folder outside its vault. What is new is copied
into the vault's `inbox/`; the originals stay where they are. A file whose
bytes the vault already holds, ingested earlier or still waiting in the inbox,
is skipped, so a folder that grows over time can be ingested again and only its
new files cost anything. A folder is linked as material of the project, and an
`ingest` with no path stages what is new in every linked folder.

```bash
claude-atlas ingest sensor-triage ~/Papers
claude-atlas ingest sensor-triage ~/Papers/dinov2.pdf
claude-atlas ingest sensor-triage                # every linked material folder
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
cd ~/Documents/Vaults/sensor-triage
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

`view` is an interactive tree of every project, three category layers at a
time. Space folds or unfolds the branch under the cursor; `-` and `+` fold and
unfold every category. Enter shows everything the atlas knows about a project,
`o` opens its vault in Obsidian, `c` starts Claude Code in it, `i` ingests a
file or folder into it, and `e` edits its page: name, purpose, category, priority, state, what it is blocked on, a
review date, what finished looks like, its vault path, and linked repos and
material, or `r` to remove it from the atlas. `n` creates a vault, `a` adopts
one, and `R` refreshes every vault in the background. Removing never touches
the vault on disk.

```bash
claude-atlas view
```

`refresh` reads every vault and rewrites `Overview.md`. Run it after editing
anything under `tree/`.

```bash
claude-atlas refresh
```

`list` prints every project with its heat, priority, and state.

```bash
claude-atlas list
```

### Link repos and material

Link the folders a project works with: a git repository (detected by its
`.git`) or a folder of static material such as slides, PDFs, and images.
Nothing is copied. Refresh reports the repo's branch, uncommitted changes, and
last commit, and the folder's file count and newest file. A commit or a new
file counts as touching the project.

```bash
claude-atlas link sensor-triage ~/code/sensor-triage
claude-atlas link sensor-triage ~/Documents/sensor-datasheets --kind materials
claude-atlas links sensor-triage
claude-atlas unlink sensor-triage ~/code/sensor-triage
```

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
`blocked_on`, `review_after`, `purpose`, `definition_of_done`, `repos`,
`materials`. The body is yours.

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
  }
}
```

| Setting | Effect |
|---|---|
| `claude_code.prompt` | a first message sent on every `open-claude`, for example `/claude-atlas:wiki` |
| `claude_code.args` | flags for `claude`, such as `--model` |
| `claude_code.session_context` | whether the session-start hook hands Claude the vault's `hot.md` |
| `plugin.source` | where `claude plugin marketplace add` gets the plugin: a GitHub slug or a local path |
| `--home DIR`, `CLAUDE_ATLAS_HOME` | use a different home instead of `~/.claude-atlas` |
| `-y`, `--yes` | answer yes to every prompt |
