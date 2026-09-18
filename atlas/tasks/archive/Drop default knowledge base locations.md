---
type: task
title: "Drop default knowledge base locations"
status: done
priority: normal
phase: ""
due: ""
created: 2026-09-17
updated: 2026-09-17
task_id: task-20260917-5e5a
---

# Drop default knowledge base locations

## Idea

Remove awareness of ~/Vaults and other default locations for knowledge bases. Now a project can be registered anywhere, and 'claude-atlas init' makes it tracked.

## Outcome

The atlas has no default location, and it never searches the disk. The config lists every knowledge base under `knowledge`, as it lists every project under `projects`.

Decisions (asked 2026-09-17): a bare name given to `new-knowledge` is a folder in the current directory. `relocate` is removed. An old `vaults_dir` is dropped, not migrated.

- `home.Config` loses `VaultsDir`, `Inside`, and `DefaultVaults`; `Default()` takes no argument. A config that still carries `vaults_dir` loads, and its next save drops the field.
- `registry.Scan` reads only the listed paths. A listed folder with no identity file becomes an entry with `ReasonNotVault`, which says to run `adopt` or `remove`.
- `vaults.ResolvePath(arg)` returns an absolute path; a bare name is `./NAME`. `vaults.Register` always lists the knowledge base. `vaults.Unregister` forgets any knowledge base, so `CheckForget` and `PathFor` are gone. `vaults.CheckNewPath` also refuses a folder inside a project.
- `vaults.RegisterKnowledge` heals a knowledge base the way `RegisterProject` heals a project, and both share `healPaths`. `place.Resolve` calls it when a session starts in a knowledge base, and the session-start hook reports what it did.
- Removed: `relocate` (command, `vaults.PlanRelocate`, `vaults.ApplyRelocate`, and their tests), `obsidian.Registry.Under`, `claudecode.ProjectsUnder`, `claudecode.ConfigFile`, and `console.Size`. Nothing else used them.
- `setup` drops `--vaults-dir`. `--first-vault` takes a path; a bare name is a folder in the current directory. The `vault` tool's create needs an absolute or `~` path, because a session's folder is usually a project. `settings` loses `vaults_dir`. `config`, `info`, and `doctor` stop printing a vaults dir.
- `forget` on a listed knowledge base path, and `remove` on a listed project path, name the right command.
- Updated: `README.md`, `docs/usage.md`, `docs/v3-design.md`, `CLAUDE.md`, and the `atlas` and `atlas-knowledge` skills.

Tests: `TestResolvePath`, `TestRegisterAndUnregister`, `TestRegisterKnowledgeHeals`, `TestRenameRewritesTheConfigEntry`, `TestCreateRefusesATakenPathAndAVaultInsideAVaultOrAProject`, `TestScanNamesAListedFolderThatIsNoKnowledgeBase`, `TestNewKnowledgeTakesABareNameAsAFolderHere`, `TestSessionStartHealsAKnowledgeBase`, plus the fixtures that now list their knowledge bases. Checked end to end in a scratch home: setup, a bare-name knowledge base, init, a moved knowledge base healed by a session, remove, and doctor.

Not done: the plugin version is still 2.0.0. The skill changes from this task and the init task reach Claude Code only after a version bump and `claude plugin update`.
