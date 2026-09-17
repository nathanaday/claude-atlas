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
- A **project** is a folder named `atlas/` inside your work, a repository or
  a folder of documents. It holds the project's tasks, grouped in phases, and
  nothing else. It has no git of its own; your repository tracks it like any
  other folder. A project uses one knowledge base. A knowledge base serves
  as many projects as you like.

A session started anywhere inside the work is the project's session: it sees
the open tasks, searches the knowledge base before it answers from the code,
and writes progress on the task page as it goes. A session started in the
knowledge base sees every project that uses it: plant work into any of them,
ask what is going on across all of them, and file what you learned.

- **Ingest from an inbox.** Files in, cited pages out; sources kept immutable.
- **Ask the knowledge base.** Answers name their evidence, or say what is
  missing.
- **Review, then commit.** Preview every change; undo reverts it. Your own
  Obsidian edits are committed first and never touched.
- **Tasks beside the code.** Plant an idea in a word, group tasks into phases,
  plan one when ready, and every session starts knowing what is open. The
  task pages are plain markdown in your repository.
- **The knowledge base knows your projects.** One page per project says what
  it is, how it is built, and what it has delivered. Finishing a task offers
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
claude-atlas new-knowledge product-x --scope "The thermal fire-detection product line."
cd ~/code/webapp
claude-atlas init --knowledge product-x
claude-atlas plant webapp "Filter vehicle false alarms" --priority high
claude
```

`init` writes `atlas/` beside your code and tells the atlas the project
exists. The session in `~/code/webapp` starts with the open task in view.
`/claude-atlas:describe` writes the project's page in the knowledge base;
`/claude-atlas:task-plan` and `/claude-atlas:task-run` carry the task through;
`/claude-atlas:task-finish` archives it and offers the knowledge base what the
work taught.

Put a source in the knowledge base's inbox and open a session there:

```bash
cp ~/Downloads/paper.pdf ~/Vaults/product-x/inbox/
claude-atlas open-claude product-x
```

`/claude-atlas:wiki-ingest` reads the inbox and writes cited pages;
`/claude-atlas:wiki-query` answers from them. Claude shows a preview before
every change, and `claude-atlas undo` takes one back. From here, `work`
names the projects a change touches and plants a task in each.

See everything at once:

```bash
claude-atlas
```

Two tabs, Knowledge and Projects. Enter expands an entry, `o` opens a
knowledge base in Obsidian, `c` starts Claude Code in either, `p` plants a
task, `R` refreshes.

Every command, the slash menu, and configuration:
[docs/usage.md](docs/usage.md).

## Inside a project

```
webapp/                       your repository, or a folder of documents
├── ...                       your work, untouched
└── atlas/
    ├── project.json          the project's identity and its knowledge base
    ├── tasks/
    │   ├── tasks.md          generated: every task, grouped by phase
    │   ├── archive/          done and cancelled
    │   └── <Title>.md        one page per task
    ├── phases/
    │   └── <Title>.md        a goal and an order; tasks name their phase
    └── inbox/                task notes you drop in by hand
```

A task page carries its status, priority, phase, and due date in frontmatter,
and four sections: Idea, Plan, Progress, Outcome. The tools change the
frontmatter and move a finished page to the archive; Claude writes the
sections. The user edits any page by hand.

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
project carries `atlas/project.json`. Neither holds a path. The atlas keeps
the paths in one place:

```
~/.claude-atlas/
├── config.json               the vaults directory, knowledge bases outside it,
│                             every project's work folder, the plugin, heat
└── state/registry.json       derived: every knowledge base with its projects,
                              every project with its task counts and its page
```

The atlas scans the vaults directory for knowledge bases and lists every
project, because projects live wherever your work lives. Move a repository
and the next session inside it heals its entry by id. `claude-atlas refresh`
rewrites the registry in full, so nothing in it goes stale.

## Conventions

Full reasoning in [docs/core-design.md](docs/core-design.md) and
[docs/v3-design.md](docs/v3-design.md).

- One operation, one commit. Claude plans, you review, the core commits.
  Claude's own file tools are refused inside the wiki.
- Your edits come first. Hand edits are committed before any operation, so
  undo never touches them.
- The core owns what it can derive: the log, the source ledger, the task
  index, the health check. Claude writes pages.
- Ids travel, paths stay. A knowledge base and a project each hold their own
  facts and no path; the atlas holds the paths, and derives everything else.
- Knowledge lives in the knowledge base. A project holds tasks and phases and
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
