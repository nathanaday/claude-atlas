# claude-atlas

Knowledge vaults for Claude Code, and one view across all of them.

[![License: MIT](https://img.shields.io/badge/license-MIT-2563eb.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8.svg?logo=go&logoColor=white)](go.mod)
[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-7c3aed.svg)](.claude-plugin/plugin.json)
[![Version](https://img.shields.io/badge/version-1.0.0-d97745.svg)](.claude-plugin/plugin.json)

## About

What Claude learns in one session is gone by the next, and reading piles up
faster than notes get written. claude-atlas gives Claude Code an Obsidian vault
it can fill and consult: drop a source into the inbox, get linked pages that
cite it, ask the vault later. A vault is a project, where the work is, or a
knowledge base, which one or more projects mount and share. Every change is a
plan you see first and one git commit you can undo. `claude-atlas` on its own
shows every vault on one screen.

- **Ingest from an inbox.** Files in, cited pages out; sources kept immutable.
- **Ask the vault.** Answers name their evidence, or say what is missing.
- **Review, then commit.** Preview every change; undo reverts it. Your own
  Obsidian edits are committed first and never touched.
- **Tasks that outlive a session.** Plant an idea in a word, plan it when
  ready, and every session starts knowing what is open.
- **Deliverables in a repository, memory in the vault.** Link a git
  repository to a project, or create one, for the code, papers, and decks;
  a session started inside it reaches the vault and its tasks with no file
  added to the repo.
- **One view across vaults.** Every vault on one screen: heat, open threads,
  unfinished work, open tasks, and each project's repositories, side by side.
- **Native Obsidian.** Plain Markdown, wikilinks, Canvas boards, Bases views.


https://github.com/user-attachments/assets/1697fa0e-59fd-4f14-95e6-d259c15c8a33


## Quickstart

### Prerequisites

- [Claude Code](https://claude.com/claude-code), with `claude` on your PATH
- [Obsidian](https://obsidian.md)
- git
- Go 1.24 or newer, to build the binary

### Install

```bash
git clone https://github.com/nathanaday/claude-atlas.git
cd claude-atlas
go install ./cmd/claude-atlas
```

The binary lands in `$(go env GOPATH)/bin`, usually `~/go/bin`. Add that
directory to your PATH if it is not there.

### Setup

```bash
claude-atlas setup
```

Setup shows its plan and asks before it does anything. It installs the
claude-atlas plugin into Claude Code and creates your first project. Run it
again at any time; finished steps are skipped.

## Usage

Create a project, put a source in its inbox, and start Claude Code inside it.
Mount a knowledge base on the project to share what it learns with other
projects:

```bash
claude-atlas new-project sensor-triage
claude-atlas new-knowledge sensor-lab
claude-atlas mount sensor-triage sensor-lab
cp ~/Downloads/dinov2.pdf ~/Documents/Vaults/projects/sensor-triage/inbox/
claude-atlas open-claude sensor-triage
```

In the session, `/claude-atlas:wiki-ingest` reads the inbox and writes cited
pages; `/claude-atlas:wiki-query` answers from the vault. Claude shows a
preview before every change, and `claude-atlas undo` takes one back.

See every vault at once. The view does everything the commands do: `o` opens
a vault in Obsidian, `c` starts Claude Code in it, `i` ingests sources, `t`
shows a project's tasks and plants new ones, `l` shows and edits its
repositories, `m` mounts a knowledge base on a project or manages its grants,
`e` edits the vault's name, tags, or scope, `T` boards every project's tasks,
`n` and `N` create a project or a knowledge base, `a` adopts a folder, `R`
refreshes:

```bash
claude-atlas
```

Every command, the slash menu, linking repositories, adopting an existing
vault, and configuration: [docs/usage.md](docs/usage.md).

## The wiki and the repository

Two places hold a project, and they are different in kind.

The **vault** is memory and thinking: the wiki pages Claude writes and cites,
the hot cache a session starts from, the tasks and their history. It is
structured, reviewed, and committed one operation at a time, and it follows
the wiki's layout.

A **repository** is where the deliverables go: the code, the paper, the
slides, the homework, the report. It is a plain git repository with its own
history and whatever structure the work needs, and it owes nothing to the
wiki's layout. Linking it to a project connects the two: a session started
inside it reaches the vault and its tasks, the atlas reports its branch and
last commit, and a task's work happens there. A course project keeps its
papers and decks in one; a codebase is one, with the wiki as the knowledge
behind it.

Every link is a git repository. Create one in the project's `repos/` folder,
ignored by the vault's own git, or anywhere on the machine; clone one from
GitHub; or link one that exists. A repository with a remote carries a choice
of how a session lands its changes, pull requests or commits, and the session
is told at its start. Folders of sources you ingest from are not linked; the
vault remembers where it staged from.

## Inside a vault

A project:

```
sensor-triage/
├── inbox/                    sources waiting to be ingested
│   └── tasks/                task notes waiting to be planted
├── ideas/                    your scratch notes, outside the wiki
├── repos/                    linked repositories, each with its own git
├── kb/<name>/                a mounted knowledge base, a symlink to its wiki/
├── .raw/captured/            immutable copies of ingested sources
├── .git/                     one commit per operation
└── wiki/
    ├── index.md              the catalog
    ├── log.md                what happened, newest first
    ├── hot.md                recent context, handed to Claude at session start
    ├── overview.md           the stable big picture
    ├── sources/ entities/ concepts/ questions/ sessions/
    ├── tasks/                one page per open task, archive/ for finished ones
    └── meta/ledgers/         source-ledger.json, task-ledger.json
```

A knowledge base holds what several projects read, the sources, entities, and
concepts of one domain. It is the same vault without `inbox/`, `ideas/`,
`repos/`, `kb/`, `wiki/questions/`, `wiki/sessions/`, and `wiki/tasks/`.

Two filing modes: `generic` files pages by type into the folders above; `lyt`
keeps atomic notes in `wiki/notes/` and navigates them through Maps of Content.
Change it with `claude-atlas mode sensor-triage lyt`.

## The atlas

Every vault carries its own identity file, `.claude-atlas.json`: its id, its
kind (`project` or `knowledge`), its name, its filing mode, and its tags. The
atlas keeps no list of your vaults. It scans the vaults directory for identity
files, adds the paths you keep elsewhere, and derives the rest:

```
~/.claude-atlas/
├── config.json               the vaults directory, the vaults outside it,
│                             repository paths, the plugin, Claude Code, heat
└── state/registry.json       derived: every vault with its heat, page count,
                              unfinished work, open tasks, and repositories
```

`claude-atlas refresh` rewrites the registry in full, so nothing in it goes
stale, and `claude-atlas view` shows it: projects grouped by their first tag,
knowledge bases beside them, and any folder the scan could not read under
`problems`.

A project reaches a knowledge base through a mount, `kb/<name>` in the
project: `claude-atlas mount PROJECT KB` links it, `unmount` drops it. A
knowledge base sets `access`, `open` or `guarded`; `grant` and `revoke` name
which projects may write a guarded one. The `mounts` tool reports a project's
mounts with their effective access. `new-project NAME --in REPO` puts a
project inside an existing git repository instead of giving it a history of
its own.

## Conventions

Full reasoning in [docs/core-design.md](docs/core-design.md).

- One operation, one commit. Claude plans, you review, the core commits.
  Claude's own file tools are refused inside the wiki.
- Your edits come first. Hand edits are committed before any operation, so
  undo never touches them.
- The core owns what it can derive: the log, the source ledger, the history,
  the health check. Claude writes pages.
- Ids travel, paths stay. A vault holds its own facts and no path; the atlas
  holds the few paths it cannot compute, and derives everything else.

## Documentation

- Usage — [docs/usage.md](docs/usage.md)
- Design and decisions — [docs/core-design.md](docs/core-design.md), [docs/v2-design.md](docs/v2-design.md), [docs/tasks-design.md](docs/tasks-design.md)
- Skills — [skills/](skills/), one `SKILL.md` per skill
- Plugin manifest — [.claude-plugin/plugin.json](.claude-plugin/plugin.json)

External:

- Obsidian — https://obsidian.md
- Claude Code plugins — https://code.claude.com/docs/en/plugins
- MCP Go SDK — https://github.com/modelcontextprotocol/go-sdk

## Credits

The vault layout, the inbox workflow, and the skills derive from
[claude-obsidian](https://github.com/AgriciDaniel/claude-obsidian) by
AgriciDaniel (MIT), which follows
[Andrej Karpathy's LLM Wiki pattern](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f).
Obsidian syntax references draw on
[kepano/obsidian-skills](https://github.com/kepano/obsidian-skills).
claude-atlas is released under the [MIT License](LICENSE).
