
> [!info] Generated page
> `claude-atlas` writes this page on every refresh so it matches the installed version (%VERSION%). Each command sits in its own block so one click copies it.

## Managing Vaults Quickstart

>[!tip] You can also use the Obsidian file explorer in this vault
>Use the `tree/` directory to organize your projects. 
>- Every markdown file in that directory becomes a project by that name
>- Nest named folders to categorize them
>- Call the `refresh` command when you're done to commit it

**Create a vault**

> Walks you through it: name, category (pick one from your tree or type a new one), purpose, confirm.

```bash
claude-atlas new-vault
```

> The same without prompts. It lands in the vaults directory; missing category folders are created.

```bash
claude-atlas new-vault my-project --category university/cs566
```

**Refresh [[Overview]]**

> Run it after editing anything under `tree/`. It rewrites [[Overview]], [[Tree]], and `categories/`.

```bash
claude-atlas refresh
```

**Open a vault in Obsidian**

> Opens the atlas with no argument, or a project's vault by name. If Obsidian does not know the folder yet, it offers to register it; Obsidian quits and relaunches so it sees the new entry.

```bash
claude-atlas open-vault
```

```bash
claude-atlas open-vault my-project
```

**View the atlas**

> `claude-atlas` alone opens it; `view` says so explicitly. Everything in one screen. Navigate your projects as a tree; Space folds a branch, `-` and `+` fold and unfold everything. Enter shows everything the atlas knows about a project; `o` opens its vault in Obsidian; `c` starts Claude Code in it; `i` ingests a file or folder into it; `t` shows its tasks, where `p` plants one, `c` continues one in Claude Code, and `o` opens its page; `l` shows its repositories, where `n` creates one, `a` links one, `e` edits, and `u` unlinks; `e` edits it (or removes it with `r`); `T` boards every project's tasks; `n` creates a vault; `a` adopts one; `R` refreshes. Paths complete with Tab.

```bash
claude-atlas
```

**List and show projects**

```bash
claude-atlas list
```

```bash
claude-atlas show my-project
```

**Edit a project**

> Every field the `e` key edits, from the command line. An empty value clears a text field.

```bash
claude-atlas edit my-project --priority high --state blocked --blocked-on "field hardware"
```

```bash
claude-atlas edit my-project --review-after 2026-10-01 --done "Every fault has a page."
```

```bash
claude-atlas edit my-project --category work/field
```

**Remove a project**

> The vault stays on disk.

```bash
claude-atlas remove my-project
```

**Info**

> Show relevant file paths, version, etc.

```bash
claude-atlas info
```

**Settings**

> Show the settings, or change how many days a vault counts as ✨ new after its creation (7 by default; 0 turns it off).

```bash
claude-atlas config
```

```bash
claude-atlas config new-days 14
```

## Working with Claude Code

> Start Claude Code inside a project's vault. The plugin's session hook hands Claude the vault's recent context (`wiki/hot.md`) at the start, and its skills are on the slash menu.

```bash
claude-atlas open-claude my-project
```

> Or stage a file or folder from anywhere and start the ingest in one step. Only files the vault has not seen are copied, so a growing folder can be ingested again and again; the vault remembers the folder, and `ingest` with no path stages what is new in every folder it remembers.

```bash
claude-atlas ingest my-project ~/Papers
```

```bash
claude-atlas ingest my-project
```

> The workflow, once inside: put a source in `inbox/`, ingest it, ask the vault questions, keep what matters.

```text
/claude-atlas:wiki-ingest
```

```text
/claude-atlas:wiki-query
```

```text
/claude-atlas:save
```

```text
/claude-atlas:wiki-lint
```

> Every change Claude makes is a plan you review, then one git commit in the vault. Take one back from the terminal:

```bash
claude-atlas history my-project
```

```bash
claude-atlas undo my-project ingest-20260912-150405-ab12
```

> To invoke a skill on every launch, set `claude_code.prompt` in config.json, for example to `/claude-atlas:wiki`. Set `claude_code.session_context` to `false` to keep the hook silent.

## Tasks

> Plant a task in a project's vault: a page with status planted, in your words. Add `--priority`, `--workdir`, or `--due` when you know them.

```bash
claude-atlas plant my-project "Check the trust dialog on resume"
```

> List open tasks: a project's, or every project's when run outside a vault with no name. `--all` includes the archive.

```bash
claude-atlas tasks my-project
```

```bash
claude-atlas tasks
```

> Continue a task in Claude Code: the session starts in the task's workdir with the vault selected and `/claude-atlas:task-run` as its first message. A title prefix works in place of the id.

```bash
claude-atlas open-claude my-project --task task-20260913-3f2a
```

> Give a vault made before tasks existed the folders and the index.

```bash
claude-atlas upgrade --all
```

## Mounting repositories

> A link is a mounted git repository: where a project's deliverables are made, with the vault as the memory behind it. Create one beside the wiki in the vault's folder (ignored by the vault's own git), or anywhere with `--at`.

```bash
claude-atlas new-repo my-project paper
```

```bash
claude-atlas new-repo my-project app --at ~/code/app
```

> Mount a repository that exists. A plain folder is refused until you agree to initialize a repository there; `--init` agrees up front.

```bash
claude-atlas link my-project ~/code/my-project
```

```bash
claude-atlas link my-project ~/Documents/cs566-work --init
```

> Mount a repository another project already uses, by the name of its page.

```bash
claude-atlas link other-project my-project
```

> Show a project's repositories and what the last refresh found in them, or every repository and the projects that use it.

```bash
claude-atlas links my-project
```

```bash
claude-atlas links
```

> Remove a link. The repository and its page are untouched.

```bash
claude-atlas unlink my-project my-project
```

> Rename a page or point it at a repository that moved. Every project that links it is rewritten.

```bash
claude-atlas edit-link my-project --name "My project" --path ~/code/my-project
```

## Relating projects

> Record that two projects belong together. One page holds the link; the other shows it as a backlink, and `show` lists both directions.

```bash
claude-atlas relate my-project other-project
```

```bash
claude-atlas unrelate my-project other-project
```

## Inside a vault

> These take a project name or a path; inside a vault they need no argument.

> Run the wiki health check: dead links, orphans, pages missing from the index, empty sections.

```bash
claude-atlas lint my-project
```

> Show or change the filing mode: `generic` files pages by type; `lyt` keeps atomic notes under Maps of Content.

```bash
claude-atlas mode my-project lyt
```

> Restore a vault after an interrupted operation.

```bash
claude-atlas recover my-project
```

## Setup and health

Install the plugin into Claude Code, create the atlas, and create a first vault. Safe to run again; finished steps are skipped.

```bash
claude-atlas setup
```

Check the installation, the plugin, git, and reach every vault.

```bash
claude-atlas doctor
```

```bash
claude-atlas version
```

## Advanced

Set purpose and priority when creating a vault.

```bash
claude-atlas new-vault sensor-triage --purpose "Sort field sensor faults." --priority high
```

Create a vault in LYT mode.

```bash
claude-atlas new-vault reading --mode lyt
```

Create a vault at an explicit path instead of the vaults directory.

```bash
claude-atlas new-vault ~/Desktop/scratch-vault
```

Bring in a vault that already exists, including one made by claude-obsidian. It gains an identity file and git history; nothing in it is replaced. With no path it asks step by step.

```bash
claude-atlas adopt ~/Documents/OldVault --name "Old Vault" --category archive --priority someday
```

Move a project by moving its page.

```bash
mv %ATLAS%/tree/capstone.md %ATLAS%/tree/university/cs566/
```

Run setup with every location chosen up front.

```bash
claude-atlas setup --atlas-vault %ATLAS% --vaults-dir %VAULTS% --first-vault research
```

Install the plugin from a local checkout while developing it.

```bash
claude-atlas setup --plugin-source ~/SoftwareProjects/claude-atlas
```

Run setup without touching Claude Code plugins.

```bash
claude-atlas setup --no-plugin
```

Answer yes to every prompt, for scripts.

```bash
claude-atlas -y setup
```

Use a different home directory for one command.

```bash
claude-atlas --home ~/other-atlas info
```

| Flag or setting | Effect |
|:--|:--|
| `--home DIR` | Use a different home instead of `~/.claude-atlas`. |
| `-y`, `--yes` | Answer yes to every prompt. |
| `CLAUDE_ATLAS_HOME` | Same as `--home`, as an environment variable. |
| `plugin.source` in config.json | Where `claude plugin marketplace add` gets the plugin: a GitHub slug or a local path. |
