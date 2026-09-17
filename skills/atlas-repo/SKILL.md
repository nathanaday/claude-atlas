---
name: atlas-repo
description: "Change a project's repositories: link a folder, clone a URL, create a new one, unlink one, set the remote or how changes land (pr or commit). Use for link repo, add a repository, clone into the project, new repo, unlink, change policy, use pull requests, commit directly, where do deliverables go."
---

# A project's repositories

Tools: `atlas`, `repo` on the atlas MCP server. A repository is where a
project's deliverables go, always a git repository; memory stays in the
vault. A repository under `<project>/repos/` belongs to that project by its
folder alone; one elsewhere is recorded in the atlas config.

Call `atlas` first and read the project's `repos` and the `repo_changes`
setting.

## Actions

| The user wants | Call |
|---|---|
| Link a folder that is a git repository | `repo` with `action: link`, `project`, `path`; `init: true` when it is a plain folder, which makes one commit of what it holds |
| Clone a URL into the project | `repo` with `action: clone`, `url`; `at` for a folder other than `repos/` |
| Start a new repository | `repo` with `action: new`, `name`; `at` as above |
| Drop one from the project | `repo` with `action: unlink`, `name`; the folder stays, and one still under `repos/` cannot be unlinked, because the folder decides |
| Set the remote, the folder, or the policy | `repo` with `action: edit`, `name`, and `remote`, `path`, or `changes` |

## The change policy

`changes` is how a session lands work in that repository. `commit`: on the
current branch. `pr`: a branch and a pull request. Every repository records
it; the atlas default is the `repo_changes` setting. When the user is unsure,
ask who reviews the work: nobody, `commit`; someone, `pr`. The session hook
and the `repos` tool tell every later session which applies.

## Confirm

One line, then yes:

> Link `~/code/triage` to `sensor-triage` as `triage`, changes: commit?

After the call, report the repository's name, path, remote, and policy from
the tool's result. A link of a plain folder without `init` is refused; ask
before passing `init`, because it makes a commit. A project at `REPO/atlas/`
already has its host repository first; `link`, `new`, and `unlink` refuse
its name, and `edit` sets its policy.

Never run git in a repository to link it; the tool does what is needed.
