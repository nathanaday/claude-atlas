# claude-atlas

A knowledge base for Claude Code, and projects that live in your work.

[![License: MIT](https://img.shields.io/badge/license-MIT-2563eb.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8.svg?logo=go&logoColor=white)](go.mod)
[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-7c3aed.svg)](.claude-plugin/plugin.json)
[![Version](https://img.shields.io/badge/version-2.0.0-d97745.svg)](.claude-plugin/plugin.json)

## About

What Claude learns in one session is gone by the next, and reading piles up
faster than notes get written. claude-atlas gives Claude Code an Obsidian
vault it can fill and consult, and a place beside your code to keep the work
in order.

Two things, and only two:

- A **knowledge base** is an Obsidian vault. Drop a source in its inbox and
  get linked pages that cite it. Ask it later. It holds the durable facts of
  one domain: your product line, your research, your personal projects. You
  open it, read it, and edit it like any vault. Every change Claude makes is
  a plan you see first and one git commit you can undo.
- A **project** is a folder `atlas/<name>/` inside your work, a repository
  or a folder of documents. It holds the project's threads, grouped in phases,
  and nothing else. The folder takes the project's name, so each project you
  open in Obsidian shows its own name. It has no git of its own; your
  repository tracks it like any other folder. A project uses one knowledge base. A knowledge base serves
  as many projects as you like.

A session started anywhere inside the work is the project's session: it sees
the open threads, searches the knowledge base before it answers from the code,
and writes progress in the thread's plan as it goes. A session started in the
knowledge base sees every project that uses it: open a thread in any of them,
ask what is going on across all of them, and file what you learned.

- **Ingest from an inbox.** Files in, cited pages out; sources kept immutable.
- **Ask the knowledge base.** Answers name their evidence, or say what is
  missing.
- **Review, then commit.** Preview every change; undo reverts it. Your own
  Obsidian edits are committed first and never touched.
- **Threads beside the code.** A thread is one line of work: a bug, a
  feature, a chore. It moves stub → spec → plan → receipt, and each stage is
  a document in its own folder, so the state of every thread is a page you
  can open. Every session starts knowing what is open. The pages are plain
  markdown in your repository, with a colored card per stage in Obsidian.
- **The knowledge base knows your projects.** One page per project says what
  it is, how it is built, and what it has delivered. Completing a thread offers
  to update it.
- **One view.** Every knowledge base and every project on one screen, with a
  key to open the vault in Obsidian or start Claude Code in it.
- **Native Obsidian.** Plain Markdown, wikilinks, Canvas boards, Bases views.

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
claude-atlas plugin into Claude Code and creates your first knowledge base.
Run it again at any time; finished steps are skipped.

## Usage

Create a knowledge base once. Then make each repository or folder you work
in a project that uses it:

```bash
claude-atlas new-knowledge ~/Vaults/product-x --scope "The thermal fire-detection product line."
cd ~/code/webapp
claude-atlas init --knowledge product-x
claude-atlas thread webapp new "Filter vehicle false alarms" --priority high
claude
```

`init` writes `atlas/webapp/` beside your code and tells the atlas the project
exists. `thread ... new` writes the thread's card and its stub. The session
in `~/code/webapp` starts with the open thread in view.
`/claude-atlas:describe` writes the project's page in the knowledge base;
`/claude-atlas:thread-spec`, `/claude-atlas:thread-plan`, and
`/claude-atlas:thread-run` carry the thread through, and each files its
document; `/claude-atlas:thread-receipt` closes it and offers the knowledge
base what the work taught. `claude-atlas threads` lists every open thread by
stage, and `claude-atlas open-claude webapp --thread ID` continues one.

Put a source in the knowledge base's inbox and open a session there:

```bash
cp ~/Downloads/paper.pdf ~/Vaults/product-x/inbox/
claude-atlas open-claude product-x
```

`/claude-atlas:wiki-ingest` reads the inbox and writes cited pages;
`/claude-atlas:wiki-query` answers from them. Claude shows a preview before
every change, and `claude-atlas undo` takes one back. From here, `work`
names the projects a change touches and opens a thread in each.

See everything at once:

```bash
claude-atlas
```

Two tabs, Knowledge and Projects. Enter expands an entry, `o` opens a
knowledge base in Obsidian, `c` starts Claude Code in either, `n` opens a
new thread, `R` refreshes.

Every command, the slash menu, and configuration:
[docs/usage.md](docs/usage.md).

## Inside a project

```
webapp/                       your repository, or a folder of documents
├── ...                       your work, untouched
└── atlas/
    └── webapp/               the project's name; a rename moves the folder
        ├── project.json      the project's identity and its knowledge base
        ├── threads/
        │   ├── threads.md    the board, generated: open threads by stage
        │   ├── <Title>.md    the card of an open thread, generated
        │   └── archive/      the cards of closed threads
        ├── stubs/<Title>.md      where a thread begins, in your words
        ├── specs/<Title>.md      what will be true when it is done, and why
        ├── plans/<Title>.md      how the work will go, then its progress
        ├── receipts/<Title>.md   how it ended: completed or killed
        ├── phases/
        │   └── <Title>.md    a goal and an order; threads name their phase
        └── inbox/            notes you drop in by hand; each becomes a thread
```

A thread's stage is never set: it is the furthest document that exists. The
`thread` tool files a document, and that moves the thread. A stage may be
skipped, and a receipt closes the thread. The card holds the thread's id,
priority, phase, and what it waits on, and it embeds every document, so one
page shows the thread end to end. Each document opens with a callout card in
its stage's color that links the thread's other documents. The tools write
the cards, the board, and those callouts; Claude writes the prose. The user
edits any page by hand.

## Inside a knowledge base

```
product-x/
├── inbox/                    sources waiting to be ingested
├── ideas/                    your scratch notes, outside the wiki
├── .raw/captured/            immutable copies of ingested sources
├── .git/                     one commit per operation
└── wiki/
    ├── index.md              the catalog
    ├── log.md                what happened, newest first
    ├── hot.md                recent context, handed to Claude at session start
    ├── overview.md           the stable big picture
    ├── sources/ entities/ concepts/
    └── meta/ledgers/source-ledger.json
```

Each project has an entity page under `wiki/entities/`. Two filing modes:
`generic` files pages by type into the folders above; `lyt` keeps atomic
notes in `wiki/notes/` and navigates them through Maps of Content. Change it
with `claude-atlas mode product-x lyt`.

## The atlas

A knowledge base carries its own identity file, `.claude-atlas.json`; a
project carries `atlas/<name>/project.json`. Neither holds a path. The atlas
keeps the paths in one place:

```
~/.claude-atlas/
├── config.json               every knowledge base's folder, every project's
│                             work folder, the plugin, heat
└── state/registry.json       derived: every knowledge base with its projects,
                              every project with its thread counts and its page
```

The atlas never searches your disk. It knows a knowledge base or a project
because the config lists its folder, and `new-knowledge`, `adopt`, and
`init` add that entry, so both can live anywhere. Move a folder and the next
session inside it heals its entry by id. `claude-atlas refresh` rewrites the
registry in full, so nothing in it goes stale.

## Conventions

Full reasoning in [docs/core-design.md](docs/core-design.md) and
[docs/v3-design.md](docs/v3-design.md).

- One operation, one commit. Claude plans, you review, the core commits.
  Claude's own file tools are refused inside the wiki.
- Your edits come first. Hand edits are committed before any operation, so
  undo never touches them.
- The core owns what it can derive: the log, the source ledger, the thread
  cards and board, the health check. Claude writes pages.
- Ids travel, paths stay. A knowledge base and a project each hold their own
  facts and no path; the atlas holds the paths, and derives everything else.
- Knowledge lives in the knowledge base. A project holds threads and phases and
  nothing else the atlas reads.

## Documentation

- Usage: [docs/usage.md](docs/usage.md)
- Design and decisions: [docs/core-design.md](docs/core-design.md), [docs/v3-design.md](docs/v3-design.md)
- Skills: [skills/](skills/), one `SKILL.md` per skill
- Plugin manifest: [.claude-plugin/plugin.json](.claude-plugin/plugin.json)

External:

- Obsidian: https://obsidian.md
- Claude Code plugins: https://code.claude.com/docs/en/plugins
- MCP Go SDK: https://github.com/modelcontextprotocol/go-sdk

## Credits

The vault layout, the inbox workflow, and the skills derive from
[claude-obsidian](https://github.com/AgriciDaniel/claude-obsidian) by
AgriciDaniel (MIT), which follows
[Andrej Karpathy's LLM Wiki pattern](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f).
Obsidian syntax references draw on
[kepano/obsidian-skills](https://github.com/kepano/obsidian-skills).
claude-atlas is released under the [MIT License](LICENSE).
