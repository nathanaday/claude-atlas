# v2 Phase 6: Skills, Agents, Docs, the Counts Line, Stubs Polish, 1.0.0 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The plugin's skills and agents teach Claude the v2 model (kinds, mounts, access, `via`, `stub` targets, lint through mounts); the session hook prints the stubs and wanted counts; the older design docs and the README say what is true now; the stubs work's deferred findings are settled; the plugin and the binary go to 1.0.0; an end-to-end check runs against the built binary.

**Architecture:** Prose tasks change Markdown only (`skills/`, `agents/`, `README.md`, `docs/`) against the facts in `docs/v2-design.md` and the code; the audit at the path below lists every drift item with line numbers. One code task adds the counts line to `internal/hooks`; one code task settles the stubs findings in `internal/lint`, `internal/txn`, `internal/vault`, `internal/mcpserver`. The version bump is the last commit before the end-to-end check, so the check runs the version that ships.

**Tech Stack:** Go 1.24, the Claude Code plugin format (skills, agents, hooks), Markdown.

**Spec:** `docs/v2-design.md` — "Skills", "Sessions", "Knowledge enters through a project", "Tools", "Lint", "Phases" item 6, "Left for later". The drift audit: `/private/tmp/claude-501/-Users-nathanaday-SoftwareProjects-claude-atlas/fbe99cf6-28f0-4b4e-accf-32fed6c08c49/scratchpad/phase6-audit.md` (line numbers are from `main` after phase 3; re-find each item by its text).

## Global Constraints

- A skill or doc states only what the code does; every tool name, argument, field, command, path, and message is checked against `internal/mcpserver/server.go` (`MCP()`, the `*Args`/`*Out` types), `internal/cli/cli.go` (`usage`), `internal/hooks/hooks.go`, and `internal/lint/lint.go`.
- The plugin's installed copy changes only when `.claude-plugin/plugin.json` and `marketplace.json` go up; both say `1.0.0` at the end of this phase, and the README badge with them. `make install` stamps the binary from `plugin.json`.
- Every write path goes through `txn.Prepare`/`Apply`; lint is read-only; the hook is read-only.
- Dependencies: `gopkg.in/yaml.v3`, the MCP `go-sdk` v1.4.0, Bubble Tea, Bubbles, Lip Gloss. Nothing else.
- Tests never touch a real `~/.claude-atlas`, never install a plugin, skip when git is missing.
- Commits: `git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit`; no `Co-Authored-By`; subject `area: what changed`; stage by name; never stage `.superpowers/`.
- Prose (skills, docs, comments, messages): short plain sentences, active voice, the same term for the same thing (a knowledge base is *mounted*; a repository is *linked*; the *host repository* is the one a project lives in), no metaphors; never "flag" (except a CLI flag), "genuine", "honest", "shape", "load bearing", "judgement call", "earned its keep", "worth flagging". Skills are instructions to Claude: imperative, one action per sentence.

---

### Task 1: The session hook prints the stubs and wanted counts

**Files:**
- Modify: `internal/hooks/hooks.go`, `internal/hooks/hooks_test.go`

**Interfaces:**
- Consumes: `lint.Run(root, lint.Options{AsOf, Mounts})`, `report.Stubs []lint.Stub{Path, Empty, …}`, `report.WantedPages []lint.WantedPage{Title, …}`, `vault.PageTitle(path)`, the hook's `entry *registry.Entry` (the project's resolved mounts: `name → m.Path` for mounts with `m.Error == ""`, the same map the server builds).
- Produces: one line, printed in a project session after the search sentence and in a knowledge base session after the first line, only when there is something to say:

```text
Stubs: 2 pages to fill (Backpropagation, Vanishing Gradient). Wanted: 1 linked page does not exist yet (Optimizer).
```

Rules: the `Stubs:` part appears when `len(report.Stubs) > 0`, the `Wanted:` part when `len(report.WantedPages) > 0`; one part alone stands as a sentence; the list holds up to three names in the report's order, then `, …`; a stub's name is `vault.PageTitle(s.Path)`; singular and plural: `1 page to fill`, `2 pages to fill`, `1 linked page does not exist yet`, `2 linked pages do not exist yet`. A knowledge base session passes no mounts. When lint fails, print nothing. The line ends with `Fill or stub them with the wiki-lint skill.`

- [ ] **Step 1: Write the failing test**

`hooks_test.go` `TestSessionStartCountsStubsAndWantedPages`: a project (no atlas config needed) with `wiki/concepts/Training.md` linking `[[Optimizer]]` and `[[Backpropagation]]`, `wiki/concepts/Backpropagation.md` a seed page (`vault.Skeleton("concept", "Backpropagation", now)`), and no `Optimizer` page; `SessionStart` output contains `Stubs: 1 page to fill (Backpropagation). Wanted: 1 linked page does not exist yet (Optimizer). Fill or stub them with the wiki-lint skill.`; a fresh project prints no `Stubs:` and no `Wanted:`; a project with four wanted pages prints three names then `, …`. Extend `TestSessionStartListsMountsAndMountedBy` (phase 3): a project page linking a page that exists only in the mounted knowledge base prints no `Wanted:` line (the mounts reach lint).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/hooks/ -run TestSessionStartCountsStubsAndWantedPages`
Expected: FAIL: no `Stubs:` line.

- [ ] **Step 3: Write the code**

A `countsLine(v, mounts, now) string` in `hooks.go`; call it at the two places named above. Reuse `plural`.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go vet ./... && gofmt -l internal/ && go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/hooks.go internal/hooks/hooks_test.go
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "hooks: the session start counts the stubs and the wanted pages"
```

---

### Task 2: The `wiki` skill routes by kind; `references/mounts.md`; the shared references

**Files:**
- Create: `skills/wiki/references/mounts.md`
- Modify: `skills/wiki/SKILL.md`, `skills/wiki/references/operations.md`, `skills/wiki/references/provenance.md`, `skills/wiki/references/frontmatter.md`, `skills/wiki/references/tasks.md`

**Content requirements** (each is a fact from the spec or the code; the audit gives the current lines):

`skills/wiki/SKILL.md`:
- "Find the vault": the session hook's first line names the kind (`claude-atlas project: …` or `claude-atlas knowledge base: … mounted by …`); the `status` tool returns `kind`, `id`, and for a project `mounts`, for a knowledge base `access` and `mounted_by`.
- The layout paragraph: a project has `inbox/`, `inbox/tasks/`, `ideas/`, `wiki/tasks/`, `wiki/questions/`, `wiki/sessions/`, `kb/<name>` (a mounted knowledge base's `wiki/`, read through the symlink), `repos/`; a knowledge base has `wiki/` with sources, entities, concepts only.
- "Route the request": two tables or one table with a kind column. In a project: ingest, save, query, tasks, mode, fold, lint, canvas, bases, think as today, plus "mount, unmount, grant, revoke: the CLI (`claude-atlas mount …`), not a tool". In a knowledge base session: maintenance only — lint, repair, fold, stub, mode, canvas, bases; ingest and save are refused ("knowledge enters through a project that mounts it: run this in <one of the projects the hook named>"); the skill says which projects mount it (the hook's first line).
- The search sentence matches the hook: "Search the project and its knowledge bases (the wiki-query skill) before answering from the code alone."
- "Conditional references" adds `references/mounts.md` ("when the project mounts a knowledge base, or the session is in a knowledge base").
- Never write under `kb/` (the guard denies it); write a knowledge base page through `plan` with `vault: <its root>` from the project session.

`skills/wiki/references/mounts.md` (new, under 80 lines): what a mount is (a knowledge base a project reads or writes through `kb/<name>`, a symlink to the knowledge base's `wiki/`); how links resolve (`[[Title]]` resolves to the project's own page first, then to one mount; a name in two mounts is ambiguous — link `[[kb/<name>/concepts/Title]]`; a name in the project and a mount is a duplicate finding); access (`open`/`guarded`, grants, effective `read`/`write`; a read mount refuses writes with "<project> mounts <kb> read-only"); the `mounts` tool's fields (`id`, `name`, `path`, `link`, `access`, `effective`, `scope`, `pages`, `error`); the `route` tool's `mounts[]` (`name`, `vault`, `effective`, `path`, `match`, `error`) and its `next` sentence; writing into a knowledge base (`capture` with `vault: <kb root>` and the project's inbox paths; `plan`/`apply` with `vault: <kb root>`; `stub` with `titles[].target: "<mount name>"`; a stub's path comes back as `kb/<name>/…`); the CLI commands that change mounts and grants.

`skills/wiki/references/operations.md`: the tool table gains `mounts` (read-only) and `stub` (`titles[].title`, `.type`, `.target`; `type`; `vault`); the kinds table gains `stub`; `task` is refused in a knowledge base; `plan`/`apply` on a knowledge base run only from a project session whose mount is effectively `write`, or as maintenance kinds in the knowledge base's own session; "Never writable" adds `kb/` and `repos/`; the `apply` result's `manual_commit` sentence says "changed by hand".

`skills/wiki/references/provenance.md`: the ledger record example gains `"via": {"id": "…", "name": "…"}` with one sentence: a source captured into a knowledge base records the project it came through.

`skills/wiki/references/frontmatter.md`: `question` and `session` exist only in a project.

`skills/wiki/references/tasks.md`: tasks exist only in a project; the tools refuse them in a knowledge base.

- [ ] **Step 1: Read the audit's sections for these six files and the spec's "Skills", "Sessions", "Mounts", "Access", "Tools" sections.**

- [ ] **Step 2: Write the changes.** Check each tool field against `internal/mcpserver/server.go` (`MountInfo`, `RouteOut`, `MountRoute`, `StubArgs`, `StubTitle`, `Status`) and each message against the code.

- [ ] **Step 3: Self-review.** `grep -n -i -E "worth flagging|\bflag|genuine|honest|shape|load bearing|judgement call|earned its keep|new-vault|Overview.md|Tree.md|About.md|Reference.md" skills/wiki/SKILL.md skills/wiki/references/*.md` → only CLI-flag hits.

- [ ] **Step 4: Commit**

```bash
git add skills/wiki/SKILL.md skills/wiki/references/mounts.md skills/wiki/references/operations.md skills/wiki/references/provenance.md skills/wiki/references/frontmatter.md skills/wiki/references/tasks.md
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "skills: the wiki skill routes by kind; a mounts reference; the shared references say what v2 does"
```

---

### Task 3: The `wiki-ingest` skill and agent follow the v2 flow

**Files:**
- Modify: `skills/wiki-ingest/SKILL.md`, `agents/wiki-ingest.md`

**Content requirements** (the spec's "Knowledge enters through a project" section is the source; read it first, then the current skill):

`skills/wiki-ingest/SKILL.md`:
- The session must be a project's; in a knowledge base session, stop and name the projects that mount it (from the hook's first line).
- The flow, in order: (1) `inbox` lists the files; (2) for each candidate title the worker proposes, `route` with the type and title — a `match` anywhere (the project or a mount) means link to it, not create; a mount entry with `path` set means the knowledge base files that type and the mount is writable; (3) choose the target vault: the knowledge base whose `scope` (from the `mounts` tool) covers the source, else the project; when in doubt, ask the user once with the scopes listed; (4) `capture` with `vault: <the knowledge base's root>` (from `mounts[].path`'s parent, or `status`) and the inbox paths — the ledger record carries `via`; for the project's own sources, `capture` as today; (5) one operation per vault: `plan` with `vault: <kb root>`, kind `ingest`, the pages and the `sources` ledger updates, then `apply`; then the project's own `plan` (kind `ingest`) with its pages, questions, and the inbox deletes; a read mount refuses — say so and stop; (6) the worker brief (`agents/wiki-ingest.md`) carries the mounts' real paths so it can read the knowledge base's existing pages through them.
- "Build one plan" becomes "One plan per vault"; the inbox deletes belong to the project's plan.
- Keep the skill's existing sections on page quality, citations, and the log.

`agents/wiki-ingest.md`: the Inputs list gains "the mounts: name, real path of the knowledge base's `wiki/`, effective access, scope" and the rule "read a knowledge base's pages through the real path; propose a page for the knowledge base only when its scope covers the source and the mount is writable".

- [ ] **Step 1: Read the spec section and the two files; list each sentence that contradicts the spec (the audit names them).**
- [ ] **Step 2: Rewrite.** Check every tool argument and field against `internal/mcpserver/server.go` (`CaptureArgs`, `PlanArgs`, `RouteArgs`/`RouteOut`, `MountsOut`).
- [ ] **Step 3: Self-review** with the banned-word grep on both files.
- [ ] **Step 4: Commit**

```bash
git add skills/wiki-ingest/SKILL.md agents/wiki-ingest.md
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "skills: wiki-ingest routes across mounts, captures into the knowledge base, and plans one operation per vault"
```

---

### Task 4: `wiki-query`, `save`, `wiki-lint`, and the task skills

**Files:**
- Modify: `skills/wiki-query/SKILL.md`, `skills/save/SKILL.md`, `skills/wiki-lint/SKILL.md`, `skills/task/SKILL.md`, `skills/task-plant/SKILL.md`, `skills/task-plan/SKILL.md`, `skills/task-run/SKILL.md`, `skills/task-finish/SKILL.md`

**Content requirements:**
- `wiki-query`: in a project, read the project's `wiki/` and every mount's (`kb/<name>/`, or the real path from the `mounts` tool); a citation names the vault with the page (`[[Page#Heading]]` in the project; `[[kb/<name>/concepts/Page#Heading]]` or "in <knowledge base>: [[Page]]" for a mount — choose one form and use it throughout); in a knowledge base session, read only that vault.
- `save`: a project session only (in a knowledge base session, stop and name the projects that mount it); a decision that belongs to a knowledge base goes through `plan` with `vault: <kb root>` when the mount is effectively `write`; a `route` `match` in a mount means append there, not create in the project.
- `wiki-lint`: the category table gains `kind_errors`, `task_errors`, `mount_errors` (one line each; read `internal/lint/lint.go`'s Markdown section headings for the names); lint runs per vault; in a project, a link that resolves through a mount is not wanted and a name in the project and a mount is a duplicate; a `mount_errors` entry means "run `claude-atlas refresh`"; the `stub` tool with `titles[].target` seeds a wanted page in a mount; the counts line at session start (Task 1) points here.
- The five task skills: one sentence each near the top: "Tasks live in a project. In a knowledge base session this skill does not apply; the hook's first line names the projects that mount it."

- [ ] **Step 1: Read the audit's sections for these files.**
- [ ] **Step 2: Write the changes**, checking names against the code (`lint.go`'s category names; `StubTitle`).
- [ ] **Step 3: Self-review** with the banned-word grep.
- [ ] **Step 4: Commit**

```bash
git add skills/wiki-query/SKILL.md skills/save/SKILL.md skills/wiki-lint/SKILL.md skills/task/SKILL.md skills/task-plant/SKILL.md skills/task-plan/SKILL.md skills/task-run/SKILL.md skills/task-finish/SKILL.md
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "skills: query reads every mount, save is a project skill, lint knows the v2 categories, tasks live in a project"
```

---

### Task 5: The stubs findings, settled

**Files:**
- Modify: `internal/lint/lint.go`, `internal/lint/lint_test.go`, `internal/txn/stub.go`, `internal/txn/stub_test.go`, `internal/vault/vault.go`, `internal/vault/vault_test.go`, `internal/mcpserver/server.go`, `docs/stubs-design.md`

**The findings** (from the stubs plan's deferred list, carried in the audit's "Stubs plan leftovers"), each with its ruling:

1. `lint.go` `sortFindings` uses `sort.Slice` where the file uses `sort.SliceStable` → use `SliceStable`.
2. `lint.go` `bodyEmpty` parses the frontmatter a second time → pass the parsed page.
3. `lint.go` `nearIndex.match` recomputes each candidate's digits per call → precompute in `newNearIndex`.
4. `lint.go` the `.md`/`.canvas`/`.base` exclusion in `wantedTitle` is untested and not in the spec → keep it, add a test (`[[note.md]]` is not a wanted page), and one sentence in `docs/stubs-design.md`.
5. `lint.go` no test that a seed stub with incomplete frontmatter appears in `missing_frontmatter` → add it.
6. `stub.go` with no titles, one unstubbable candidate aborts the request → skip it and report it: `StubResult.Skipped []Skipped{Title, Reason}` (`json:"skipped,omitempty"`); with explicit titles, an unstubbable one still refuses.
7. `stub.go` `pageNamed` ignores the walk error and keeps walking after a match → return on the first match; surface the error.
8. `stub.go` two empty linked files with the same name in different folders → refuse: "two empty pages are named X: a, b; keep one".
9. `stub.go` a repeated title with a different type is skipped silently → refuse: "X is given twice with different types".
10. `stub.go` calls `vault.Skeleton` where `route.Skeleton` holds the same text → use `route.Skeleton`.
11. `stub.go` the refusal for an unnameable link names the file → name the link text: "the link text X cannot be a file name; rename the link, then stub it".
12. `stub.go` text typed into an empty file between lint's read and `Prepare`'s hash becomes the base → `Prepare` computes the base hash from the file at plan time already; add the test that a change after `StubRequest` and before `Apply` conflicts (if it does not, make `StubRequest` record `BaseSHA256` for the replace write from the bytes lint read).
13. `server.go` the `stub` tool description is one long sentence → two or three short ones; the `type` argument's schema text lists the lyt types too.
14. `vault.go` `mergeSettings` replaces an invalid `app.json` with only the merged keys → when the file is not valid JSON, leave it alone and return an error naming it.
15. Tests: a lyt-mode end-to-end stub; a summary with more than three titles; undo and a conflict on the in-place replace; the refusal reasons in `TestStubKind`.

- [ ] **Step 1: For each finding, write the test first where one is named, run it, then the code.**
- [ ] **Step 2: `docs/stubs-design.md`**: the status line; `StubRequest`'s signature (`titles, defaultType, mounts, now`); the `type` argument; the `Skipped` field; the extension exclusion; remove `Overview.md` and `Reference.md` mentions (v2 removed them).
- [ ] **Step 3: Run** `go build ./... && go vet ./... && gofmt -l internal/ && go test -count=1 ./...`.
- [ ] **Step 4: Commit** (one commit; the subject names the packages):

```bash
git add internal/lint/lint.go internal/lint/lint_test.go internal/txn/stub.go internal/txn/stub_test.go internal/vault/vault.go internal/vault/vault_test.go internal/mcpserver/server.go docs/stubs-design.md
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "lint, txn, vault, mcpserver: the stubs review's deferred findings"
```

---

### Task 6: README, the older design docs, CLAUDE.md, usage.md, and 1.0.0

**Files:**
- Modify: `README.md`, `docs/core-design.md`, `docs/atlas-design.md`, `docs/tasks-design.md`, `docs/usage.md`, `CLAUDE.md`, `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`, `docs/v2-design.md`

**Content requirements:**
- `README.md`: the badge says 1.0.0; "About" and "Usage" say a vault is a project or a knowledge base and a project mounts knowledge bases; "Inside a vault" layout diagram gains `kb/<name>/` and says which folders a knowledge base lacks; "The atlas" names `mount`, `unmount`, `grant`, `revoke`, `access`/`scope`/`grants`, `new-project --in`, the `mounts` tool; the command list matches `usage`.
- `docs/core-design.md`: `new-vault` → `new-project`/`new-knowledge`; the sentence about `~/Documents/Atlas` goes; `About`/`Reference` templates go; the reserved-paths list gains `kb/` and `repos/`; a two-line note at the top: "v2 (`v2-design.md`) supersedes what this document says about the atlas and about a single vault kind; the engine rules below stand."
- `docs/atlas-design.md`: the status line says the document is historical: "Superseded by `v2-design.md`. Kept for the reasoning behind the first atlas; nothing below describes the code now." No other edits.
- `docs/tasks-design.md`: no change beyond its status line if it already says which sections are superseded (check).
- `docs/usage.md`: a "Stubs and wanted pages" subsection (the `claude-atlas stub VAULT [TITLE...] [--type T]` command, the `stub` tool, `titles[].target`, the counts line at session start); the command table has `stub`.
- `CLAUDE.md`: the sources-of-truth v2 row "(phases 1–6 built)"; the "Build and test" section's version note says 1.0.0 is the first v2 release; the skills' contracts row unchanged.
- `.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json`: `1.0.0` (three places).
- `docs/v2-design.md`: status "Phases 1–6 are built. Phase 7 (migration by hand) is not."; "Left for later" drops the `wiki-lint` category line (done) and the counts-line item (done).

- [ ] **Step 1: Read the audit's sections for these files.**
- [ ] **Step 2: Write the changes**; `grep -rn "0\.7\.0" --include='*.json' --include='*.md' . | grep -v superpowers` shows nothing afterwards; `grep -rn "new-vault\|Overview.md\|Tree.md\|About.md\|Reference.md" README.md docs/core-design.md docs/usage.md CLAUDE.md` shows nothing.
- [ ] **Step 3: `make build`** succeeds and `build/claude-atlas version` (or `--version`; check `cli.go`) prints `1.0.0`.
- [ ] **Step 4: Commit**

```bash
git add README.md docs/core-design.md docs/atlas-design.md docs/tasks-design.md docs/usage.md CLAUDE.md .claude-plugin/plugin.json .claude-plugin/marketplace.json docs/v2-design.md
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "docs: v2 in the README and the older designs; the plugin and the binary go to 1.0.0"
```

---

### Task 7: The end-to-end check against the built binary

**Files:**
- Modify: `docs/v2-design.md` (the "Migration" section's check record)

**The check** (run with `make build`, `CLAUDE_ATLAS_HOME` pointed at a temp folder — confirm the variable's name in `internal/home/home.go` (`home.EnvHome`) — and a temp vaults directory; never the real `~/.claude-atlas`):

1. `setup` non-interactively if it supports it (read `wizard`), else write the config by hand with `vaults_dir` set.
2. `new-knowledge ai-ml --scope "Machine learning"`; `new-project cs566 --tags usc`; `mount cs566 ai-ml`; `show cs566` lists the mount `write`; `edit ai-ml --access guarded`; `show cs566` says `read`; `grant ai-ml cs566 --write`; `show cs566` says `write`.
3. In `ai-ml`, an operation through the MCP server: start `build/claude-atlas mcp` as a subprocess from `cs566`'s root and drive it with a small Go program or the SDK's stdio client (write it under the workspace, not the repo) — or, if that costs more than an hour, use the in-process tests as the evidence and say so. The steps: `status` in `cs566` shows the mount; `capture` with `vault: <ai-ml root>` of a file placed in `cs566/inbox/`; `plan` kind `ingest` into `ai-ml` with one source page and one concept page; `apply`; `lint` in `cs566` after adding a project page that links the concept — no wanted page; `stub` with a `target`; `route` for the concept's title shows `mounts[0].match`.
4. `refresh` after deleting the symlink recreates it; `doctor` exits 0.
5. `new-project notes --in <a scratch git repository>`; `show notes` lists the host; `repos notes`; a `plan`/`apply` through the server in that project commits with the pathspec (`git log --stat` in the host shows only `atlas/` paths).
6. `claude -p` inside `cs566` with `--plugin-dir <checkout>` and `--allowedTools "mcp__plugin_claude-atlas_atlas__*,Read,Grep,Glob,Skill"`, asking it to run the wiki-lint skill and report the counts, if `claude` is on PATH and non-interactive use works; otherwise record "not run".
7. The Obsidian follow-symlink check stays manual; record "not yet run" if the user has not run it.

- [ ] **Step 1: Run the check**, saving the transcript to the plan's workspace (`.superpowers/sdd/<plan>/e2e-transcript.md`).
- [ ] **Step 2: Record** the result in `docs/v2-design.md`'s "Migration" section: one paragraph, the date, what ran, what did not, and any defect found (a defect found here is fixed in this task when it is small, or recorded in "Left for later" with the failing step).
- [ ] **Step 3: Commit**

```bash
git add docs/v2-design.md
git -c user.name=nathanaday -c user.email=nraday1221@gmail.com commit -m "docs: the end-to-end check's record"
```

---

## Self-review

**Spec coverage (phase 6):** skills and agents (Tasks 2, 3, 4, and the `wiki-ingest` agent's mounts in Task 3); README, CLAUDE.md, usage.md (Task 6); 1.0.0 (Task 6); the stubs leftovers assigned to phase 6 — the session-start counts line (Task 1), the skills (Tasks 2–4), the final review of the deferred findings (Task 5), the end-to-end check (Task 7, which the stubs plan's Task 10 also owed).

**Placeholders:** the prose tasks state content requirements, not the prose; each requirement is a fact the implementer checks against the code. Task 5's rulings are stated per finding.

**Type consistency:** `StubResult.Skipped` (Task 5) is documented in `docs/stubs-design.md` (Task 5) and mentioned by the `wiki-lint` skill only as "the result lists what it skipped" (Task 4 runs before Task 5; the skill's sentence is generic). The counts line (Task 1) is what `usage.md` (Task 6) and the `wiki-lint` skill (Task 4) describe.
