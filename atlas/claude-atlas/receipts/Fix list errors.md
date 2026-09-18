---
type: receipt
thread: thr-20260917-6214
title: "Fix list errors"
outcome: completed
created: 2026-09-17
---

> [!receipt] Fix list errors · completed
> [Stub](<../stubs/Fix list errors.md>) → Spec → Plan → **Receipt**
> `thr-20260917-6214` · [Thread](<../threads/archive/Fix list errors.md>) · filed 2026-09-17

All three problems are fixed.

1. **Stale registry.** `registry.Read` now wraps `ErrStale` when the state file has another schema or is not JSON. `refresh.Entries` rebuilds the file in that case, as it already did for a missing file. `list`, the view, and the tools all read through `Entries`, so none of them stops on an old file.
2. **Problems shown twice.** `list` prints the entries it can read as a table, then each unreadable entry once, as `✗ name  path: reason`. The name column in `list` and `doctor` takes its width from the longest name, so `autonomous-vehicle-sandbox` no longer pushes its path out of line.
3. **`off` versus `new`.** `refresh.setHeat` counts the creation date as a touch when there is no other sign of work. A new folder with no git and no open tasks is now `new`, and it goes `cold` after months with no activity. Before, it had no heat at all, and `list` printed `off`.

Tests: `TestEntriesRebuildsAStaleRegistry`, `TestListRebuildsAStaleRegistry`, `TestCreationTouchesAProject`, and updated list assertions in `TestRefreshAndDoctorReportProblems` and `TestDoctorAndForgetSeeAMissingProject`.
