---
name: task-plant
description: "Plant a task from a sentence, from the notes in a project's atlas/inbox/, or into any project from its knowledge base: a page with status planted, in the user's words, no questions asked. Use for add a task, note a todo, plant this, remember to, open end, later, inbox tasks, task notes, plant this in project X."
---

# Plant a task

Planting costs nothing. The idea goes on a page as the user said it; the
ceremony comes later, if ever. Tools: `status`, `inbox`, `plant`.

Three doors, one result: a page with status `planted`.

## From a sentence, in a project session

1. Take the title from the user's words: short, imperative, under 80
   characters. Keep the full text for the page.
2. Call `plant` with `title` and `text`. Add `priority` only when the user
   said one, `phase` when they named one (it must exist; `task` creates
   phases), `due` when they gave a date.
3. Report the page path and the id. Ask nothing. Do not plan it.

## From the notes in atlas/inbox/

1. Call `inbox`; in a project session `notes` lists the task notes. Read
   each with Read.
2. One note may hold several ideas. Plant each idea as its own task: the
   note's own words as `text`, a title of your own when the note has no
   heading. Pass `from` with the note's path on the last plant for that
   note, so the note is removed once every idea from it has a page.
3. Report every page planted and which notes were removed.

Task notes are not sources: do not capture them, and do not run the ingest
skill on them.

## From a knowledge base session

The hook's `Projects:` line names the projects that use this knowledge base.
Call `plant` with `project` set to the one the user means, and the same
fields. When the user does not say which project, ask once, offering the
names; a task about the ecosystem as a whole is still a task of one project.
An issue that is knowledge, not work, is a `save`, not a plant.

The text on the page is the user's; never rewrite it. A planted task never
reaches the knowledge base; `task-finish` offers that when the work is done.
