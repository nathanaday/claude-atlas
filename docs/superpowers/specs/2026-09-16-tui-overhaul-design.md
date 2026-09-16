# The view, second design

Date: 2026-09-16. Applies to claude-atlas 1.0.0; ships as 1.1.0.

## Goal

`claude-atlas view` shows the v2 schema on screen: projects on one tab,
knowledge bases on another, and a dashed line from every project to the
knowledge bases it mounts. A new user who does not yet know the two kinds
should read the screen and see how they relate: a project mounts, a
knowledge base is mounted, and the arrow always points at the knowledge base.

The actions do not change. Every key still maps to one CLI command, and the
sub-screens (edit, add, adopt, ingest, repositories, mounts, tasks) stay as
they are. What changes is the main screen: how vaults are listed, how they
are navigated, and what one vault shows when it is selected.

## The screen

```
  Atlas   Projects 6   Knowledge 2   Tasks 4              refreshed 2026-09-16 14:02
  A project holds tasks, questions, and an inbox. It mounts the knowledge bases it reads and writes.

  ╭────────────────────────────────╮
  │ 🔥 vision-algorithms           │╌╌╌╌▶ p3-ecosystem   write
  │ touched today · 3 tasks open   │
  ╰────────────────────────────────╯
  school
  ╭────────────────────────────────╮
  │ 🌤️ cs566-project               │╌╌╌╌▶ personal-kb    write
  │ idle 3d · 1 task open          │
  ╰────────────────────────────────╯
  ╭────────────────────────────────╮
  │ ✨ scratch                     │      no knowledge base mounted
  │ new · no tasks                 │
  ╰────────────────────────────────╯
  (end)

  ↑↓ move · Enter details · o Obsidian · c Claude · i ingest · t tasks · l repos · m mounts · e edit
  ←→ tabs · n new project · N new knowledge base · a adopt · R refresh · q quit
```

Four parts, top to bottom:

1. **The tab bar.** `Atlas`, then one tab per kind with its count: `Projects`,
   `Knowledge`, `Tasks`, and `Problems` only when the scan found a vault it
   could not read. The active tab is bold in its kind's color; the others are
   dim. The refresh stamp sits at the right, as today.
2. **The caption.** One dim sentence that says what the tab holds. It is the
   only prose on the screen and it carries the concept for a new user.
   - Projects: "A project holds tasks, questions, and an inbox. It mounts the
     knowledge bases it reads and writes."
   - Knowledge: "A knowledge base is a wiki. Projects mount it, and sources
     reach it through a project's inbox. A guarded one grants write access
     per project."
   - Tasks: "Every project's open tasks."
   - Problems: "Vaults the atlas found but could not read."
3. **The body.** The tab's list. It scrolls by line and keeps the cursor's
   row visible, as today. `(end)` closes the list, as today.
4. **The footer.** Two dim lines of keys: the first for the vault under the
   cursor (absent when no vault is under it), the second for the tab and the
   atlas. A status or error line follows when there is one, as today.

## Keys

| Key | What it does |
|---|---|
| `←` `→` | previous tab, next tab; the tabs do not wrap |
| `↑` `↓` | move within the tab |
| Enter | expand the vault under the cursor, or collapse it |
| Esc | collapse every expanded vault on the tab; on the Tasks tab, return to Projects; when nothing is open, quit |
| `n` `N` | create a project, create a knowledge base |
| `a` | adopt a folder as a vault; on the Problems tab, adopt the folder under the cursor |
| `o` `c` | open the vault in Obsidian, start Claude Code in it |
| `i` `t` `l` | on a project: ingest sources, its tasks, its repositories |
| `m` | mounts: mount and unmount on a project; grants on a knowledge base |
| `e` | edit the vault; `s` saves, `r` forgets it |
| `T` | the Tasks tab |
| `R` | refresh in the background |
| `q` | quit |

Removed: Space (fold), `-` `+` (fold all), Enter on a folder, and the zoom
into a deeper folder with Esc back. The tree is gone; nothing folds.

The vault keys act on the vault under the cursor, expanded or not. The
separate detail screen is gone; expansion replaces it.

## The Projects tab

Each project is a box in the left column. Line 1: the heat mark and the
name. Line 2: when it was last touched (`touched today`, `idle 3d`, or `new`
for a vault with no operation yet; `not refreshed` when no refresh has run),
then its open task count (`3 tasks open`, `1 task open`, `no tasks`). Nothing
else; the counts of pages, stubs, and wanted pages moved into the expanded
block.

Projects are grouped by their first tag. Untagged projects come first with no
header; then one dim header per tag, tags in alphabetical order; projects in
alphabetical order within a group. A header is not a row: the cursor skips it.

The right column draws the project's mounts, one line each, starting beside
line 1 of the box:

```
  │ 🔥 vision-algorithms           │╌╌╌╌▶ p3-ecosystem   write
  │ touched today · 3 tasks open   │╌╌╌╌▶ personal-kb    read (write not granted)
```

- The label is the mount's knowledge base name in the knowledge color, then
  the effective access. When the grant reduced what the project asked for,
  the reason follows in parentheses.
- A mount whose knowledge base the scan did not find shows its error in the
  error color in place of the name.
- A mount whose symlink is missing or points elsewhere appends
  `· link missing` or `· link wrong` in the error color. `R` recreates it.
- A project with no mounts shows `no knowledge base mounted` in dim text.
- The box grows to fit: its content is `max(2, mounts)` lines, so a project
  with three mounts has a three-line box.

Enter expands the project. The box stays; the detail block renders under
it, indented and across the full width:

```
  ╭────────────────────────────────╮
  │ 🔥 vision-algorithms           │╌╌╌╌▶ p3-ecosystem   write
  │ touched today · 3 tasks open   │
  ╰────────────────────────────────╯
     Path           ~/Documents/Vaults/projects/vision-algorithms
     Id             b3e0f1a2-7c4d-4e1f-9a0b-2d3e4f5a6b7c
     Mode           plain
     Tags           work, thermal
     Repositories   vision-algos     ~/SoftwareProjects/vision-algos · changes: pr
                                     clean · 2 ahead of origin/main
     Vault check    ok
     Last operation ingest 2026-09-15
     Pages          41
     Unfinished     2 stubs · 1 wanted page
     Tasks          3 open: 1 active · 2 planned
     Signals        - 2 files waiting in inbox/
     Refreshed      2026-09-16 14:02
```

The block holds what the detail screen held today, minus the name line (the
box has it) and minus the mounts (the connectors have them). A project the
atlas has not refreshed shows `never refreshed; press R` in place of the
derived rows.

More than one vault may be expanded at once; Enter toggles each. Expansion
survives a refresh and a return from a sub-screen. It is not persisted.

## The Knowledge tab

Each knowledge base is a box in the left column. Line 1: the heat mark, the
name, and its access (`open` or `guarded`). Line 2: the page count and when
it was last touched.

The right column shows how many projects mount it, as one arrow pointing at
the box:

```
  ╭────────────────────────────────╮
  │ 🔥 personal-kb   open          │◀╌╌╌╌ 5 projects
  │ 142 pages · touched today      │
  ╰────────────────────────────────╯
  ╭────────────────────────────────╮
  │ 🌤️ p3-ecosystem   guarded      │◀╌╌╌╌ 1 project · 1 grant stale
  │ 88 pages · idle 2d             │
  ╰────────────────────────────────╯
```

`0 projects` renders as `not mounted by any project`, dim. A stale grant (a
grant whose project the scan did not find) adds `· N grant(s) stale` in the
error color.

Enter expands the knowledge base. The connectors become one line per project
that mounts it, with the project's name in the project color and its
effective access; a stale grant follows as a line of its own; then the detail
block renders under the box:

```
  ╭────────────────────────────────╮
  │ 🌤️ p3-ecosystem   guarded      │◀╌╌╌╌ vision-algorithms   write
  │ 88 pages · idle 2d             │◀╌╌╌╌ cs513-project       read
  │                                │      grant  sensor-lab   write · project not found
  ╰────────────────────────────────╯
     Scope          the moviTHERM/ITL thermal-monitoring ecosystem
     Path           ~/Documents/Vaults/knowledge/p3-ecosystem
     Id             …
     Mode           plain
     Grants         vision-algorithms  write
     Vault check    ok
     …
```

The expanded box grows to fit the connector lines: `max(2, projects + stale
grants)` content lines. Collapsed, it is always two lines.

## The Tasks tab

The tab hosts the tasks board that `T` opens today, for every project, with
its keys unchanged: `↑` `↓`, Enter, `p` plant, `c` continue in Claude Code,
`o` open the page. `←` and `→` switch tabs because the board does not use
them. Esc on this tab returns to the Projects tab. `t` on a project still
opens that project's board as a sub-screen.

## The Problems tab

Present only when the scan found a vault it could not read. Each entry is a
box: line 1 the folder name, line 2 the error in the error color. Enter
expands it to the path, the reason code, and what fixes it, in the words
`list` and `doctor` use today (a v1 identity file: `adopt --as`; a
registered folder that is gone: `remove`). `a` on an entry opens the adopt
screen with the path filled in; `e` opens the editor, where `r` forgets a
registered path.

## Colors and glyphs

Two kind colors, used everywhere in the TUI a vault's name appears: a
project's name in blue (`12`), a knowledge base's name in green (`2`). The
selected box's border takes its kind's color. The connector labels wear the
other kind's color, so on the Projects tab the right column is green and on
the Knowledge tab it is blue. The mounts screen and the tasks board color
names the same way.

The heat marks stay as they are. The connector is `╌╌╌╌▶` on the Projects tab
and `◀╌╌╌╌` on the Knowledge tab; the arrow always points at the knowledge
base.

## Widths

The box column is `clamp(width/2 - 4, 26, 40)` columns wide. The connector
takes six columns. The label column takes the rest and truncates a long
label with `…`. Widths under 60 columns are not a target; the label column
may be too narrow to read there, and nothing breaks.

## What does not change

- The sub-screens: the editor, the add and adopt screens, ingest,
  repositories, mounts, and tasks. They open from the same keys and return
  to the same tab with the cursor on the same vault.
- The hooks (`tui.Hooks`, `tui.Opener`) and the CLI. No new command; the
  usage table's key column changes, the command column does not.
- Refresh, status, error, and busy handling in the view.

## Code

`internal/tui/`:

- `view.go` keeps the model, `Update`, the messages, the sub-screen hosting,
  the footer, and `RunView`. It loses the tree: `folderNode`, `buildTree`,
  `fold`, `foldAll`, the zoom stack, `rowFolder`, `rowFolded`, `maxLayers`,
  `collapsed`, and `viewDetail`. It gains the tab bar, the active tab, and
  `←` `→`.
- `board.go` (new) is one tab's list: which items it holds, the cursor, the
  offset, the expanded set, the rendered lines, and the row spans. It renders
  boxes, group headers, and connectors, and knows the tab's kind. The Problems
  tab is a board with no connectors. The Tasks tab is not a board; it is the
  existing `tasksScreen`.
- `detail.go` (new) renders the expanded block for one entry, from the code
  `viewDetail` holds today, and the connector lines for one entry given every
  item.
- `styles.go` (new) holds the kind colors and the styles the screens share,
  moved out of `editor.go`.

The model's state for the main screen becomes: the items, the active tab, one
board per kind, and the hosted tasks board. A refresh or a sub-screen return
rebuilds the boards from the items and keeps each board's cursor on the same
vault path and its expanded set.

## Tests

`view_test.go` is rewritten around the tabs. Each test drives the model with
`tea.KeyMsg` and reads `View()`:

- the tab bar lists the tabs with counts, and `←` `→` change the active one
  without wrapping; `T` lands on Tasks; Problems appears only with an
  unreadable entry;
- the Projects tab draws a connector per mount with the effective access, the
  reason when reduced, the error for a missing knowledge base, and the
  symlink state; a project with no mounts says so;
- projects group under their first tag, untagged first, and the cursor skips
  headers;
- the Knowledge tab counts the projects, names stale grants, and Enter turns
  the count into one line per project;
- Enter expands and collapses, Esc collapses everything, then quits; two
  vaults may be expanded at once; expansion survives a refresh;
- the expanded block shows the path, id, mode, tags or scope, repositories,
  derived state, tasks, and signals, or the never-refreshed line;
- the footer's first line follows the cursor and names only the keys the
  kind has;
- the empty states per tab;
- the existing open, Claude Code, new, adopt, refresh, and ingest tests,
  adapted to the tabs;
- `a` on the Problems tab opens the adopt screen with the path filled in.

`board_test.go` covers layout without a model: widths, the box growing to fit
connectors, the label truncation, and the row spans that scrolling uses.

## Docs

- `docs/usage.md`, "The atlas": the paragraph and the key table.
- `docs/v2-design.md`, "The atlas": the `view` paragraph and the key table;
  "Left for later" drops nothing and gains `move` (below).
- `CLAUDE.md`: the `internal/tui/` layout line.
- `README.md`: any description of the tree.
- Version 1.1.0 in `plugin.json`, both `marketplace.json`, and the README
  badge.

## Left for later

- A `move NAME PATH` command, so the editor could change a vault's location.
  Today the location is shown read-only; moving a vault is `mv` and then
  `refresh`, which recreates the symlinks.
- The add screen's "in repository" option (`new-project --in`).
- Mounting and unmounting from the connector column itself. `m` does it.
- The host mark on repository rows.

## Decisions

| Question | Decision |
|---|---|
| Tree or tabs | tabs, one per kind; the tree and its folds are removed |
| Where the detail goes | under the box, expanded in place with Enter; no separate screen |
| One or many expanded | many; Enter toggles each, Esc collapses all |
| Knowledge base labels on the Projects tab | plain labels with the access, not boxes; the knowledge base's box is on its own tab |
| Tags | a dim group header on the Projects tab; untagged first |
| Box line 2 | project: touched and open tasks; knowledge base: pages and touched |
| Tasks board | a tab, hosting the existing screen |
| Unreadable vaults | a Problems tab that appears only when needed |
| Colors | blue for projects, green for knowledge bases, everywhere a name appears |
| Location edit | not in this change; `move` is left for later |
