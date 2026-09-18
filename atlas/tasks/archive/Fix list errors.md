---
type: task
title: "Fix list errors"
status: done
priority: normal
phase: ""
due: ""
created: 2026-09-17
updated: 2026-09-17
task_id: task-20260917-6214
---

# Fix list errors

## Idea

Fix the list errors found on 2026-09-17: (1) list fails with 'unsupported schema claude-atlas.registry.v1; run claude-atlas refresh' when it could rebuild the derived registry itself; (2) each v2 project vault shows twice, as a '? v2' row and again as a '✗' problem line; (3) a new project shows 'off' when its folder has no git and 'new' when it does.

## Outcome

All three problems are fixed.

1. **Stale registry.** `registry.Read` now wraps `ErrStale` when the state file has another schema or is not JSON. `refresh.Entries` rebuilds the file in that case, as it already did for a missing file. `list`, the view, and the tools all read through `Entries`, so none of them stops on an old file.
2. **Problems shown twice.** `list` prints the entries it can read as a table, then each unreadable entry once, as `✗ name  path: reason`. The name column in `list` and `doctor` takes its width from the longest name, so `autonomous-vehicle-sandbox` no longer pushes its path out of line.
3. **`off` versus `new`.** `refresh.setHeat` counts the creation date as a touch when there is no other sign of work. A new folder with no git and no open tasks is now `new`, and it goes `cold` after months with no activity. Before, it had no heat at all, and `list` printed `off`.

Tests: `TestEntriesRebuildsAStaleRegistry`, `TestListRebuildsAStaleRegistry`, `TestCreationTouchesAProject`, and updated list assertions in `TestRefreshAndDoctorReportProblems` and `TestDoctorAndForgetSeeAMissingProject`.
