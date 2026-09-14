# claude-atlas

Knowledge vaults for Claude Code, and one view across all of them.

[![License: MIT](https://img.shields.io/badge/license-MIT-2563eb.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8.svg?logo=go&logoColor=white)](go.mod)
[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-7c3aed.svg)](.claude-plugin/plugin.json)
[![Version](https://img.shields.io/badge/version-0.4.2-d97745.svg)](.claude-plugin/plugin.json)

## About

What Claude learns in one session is gone by the next, and reading piles up
faster than notes get written. claude-atlas gives Claude Code an Obsidian vault
it can fill and consult: drop a source into the inbox, get linked pages that
cite it, ask the vault later. Every change is a plan you see first and one git
commit you can undo. The atlas is the page that shows all your vaults at once.

- **Ingest from an inbox.** Files in, cited pages out; sources kept immutable.
- **Ask the vault.** Answers name their evidence, or say what is missing.
- **Review, then commit.** Preview every change; undo reverts it. Your own
  Obsidian edits are committed first and never touched.
- **Tasks that outlive a session.** Plant an idea in a word, plan it when
  ready, and every session starts knowing what is open. A repo linked to the
  project reaches the vault and its tasks with no file added to the repo.
- **One view across vaults.** Heat, open threads, unfinished work, and your
  declared priority, side by side. Projects, the repos and folders they
  share, and the tree itself draw as one graph in Obsidian.
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
claude-atlas plugin into Claude Code, creates the atlas vault, and creates your
first knowledge vault. Run it again at any time; finished steps are skipped.

## Usage

Create a vault, put a source in its inbox, and start Claude Code inside it:

```bash
claude-atlas new-vault sensor-triage --category work
cp ~/Downloads/dinov2.pdf ~/Documents/Vaults/sensor-triage/inbox/
claude-atlas open-claude sensor-triage
```

In the session, `/claude-atlas:wiki-ingest` reads the inbox and writes cited
pages; `/claude-atlas:wiki-query` answers from the vault. Claude shows a
preview before every change, and `claude-atlas undo` takes one back.

See every vault at once. The tree does everything the commands do: `o` opens
a vault in Obsidian, `c` starts Claude Code in it, `i` ingests sources, `t`
shows its tasks and plants new ones, `l` shows and edits its linked repos and
folders, `e` edits its project page, `T` boards every project's tasks, `n`
creates a vault, `a` adopts one, `R` refreshes:

```bash
claude-atlas
```

Every command, the slash menu, linking repos, adopting an existing vault, and
configuration: [docs/usage.md](docs/usage.md).

## Inside a vault

```
sensor-triage/
├── inbox/                    sources waiting to be ingested
│   └── tasks/                task notes waiting to be planted
├── ideas/                    your scratch notes, outside the wiki
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

Two filing modes: `generic` files pages by type into the folders above; `lyt`
keeps atomic notes in `wiki/notes/` and navigates them through Maps of Content.
Change it with `claude-atlas mode sensor-triage lyt`.

## The atlas

```
~/Documents/Atlas/            an Obsidian vault; open it like any other
├── Overview.md               every project, its heat, threads, and signals
├── Tree.md                   the root of the graph
├── About.md                  orientation
├── Reference.md              every command with examples
├── tree/                     yours: folders are categories, files are projects
├── repos/                    one page per linked git repository
├── materials/                one page per linked folder of sources
└── categories/               generated: one page per folder under tree/
```

Each project page under `tree/` holds your intent: purpose, priority, state,
what it is blocked on, the repos and folders it works with, and the projects
it belongs with. Everything else is derived from the vault on every refresh,
so the overview never goes stale. The most useful line on it is where the two
disagree: a project marked `high` whose vault has been cold for weeks.

Every page is a node, so the graph view shows the whole ecosystem: categories
to projects, projects to the repos and folders they share.

## Conventions

Full reasoning in [docs/core-design.md](docs/core-design.md).

- One operation, one commit. Claude plans, you review, the core commits.
  Claude's own file tools are refused inside the wiki.
- Your edits come first. Hand edits are committed before any operation, so
  undo never touches them.
- The core owns what it can derive: the log, the source ledger, the history,
  the health check. Claude writes pages.
- The atlas never writes into a vault, and no vault knows the atlas exists.

## Documentation

- Usage — [docs/usage.md](docs/usage.md)
- Design and decisions — [docs/core-design.md](docs/core-design.md), [docs/atlas-design.md](docs/atlas-design.md), [docs/tasks-design.md](docs/tasks-design.md)
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
