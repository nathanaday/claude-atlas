---
name: task-plant
description: "Plant a task from a sentence or from the notes in inbox/tasks/: a page with status planted, in the user's words, no questions asked. Use for add a task, note a todo, plant this, remember to, open end, later, inbox tasks, task notes."
---

# Plant a task

Planting costs nothing. The idea goes on a page as the user said it; the
ceremony comes later, if ever. Tools: `status`, `inbox`, `plant`.

Tasks live in a project. In a knowledge base session this skill does not
apply; the hook's first line names the projects that mount it.

## From a sentence

1. Take the title from the user's words: short, imperative, under 80
   characters. Keep the full text for the page.
2. Call `plant` with `title` and `text`. Add `priority` only when the user
   said one, `workdir` when they named a folder or repository, `due` when
   they gave a date.
3. Report the page path and the id. Ask nothing. Do not plan it.

## From the notes in inbox/tasks/

1. Call `inbox`; the files with `area: tasks` are task notes. Read each with
   Read.
2. One note may hold several ideas. Plant each idea as its own task: the
   note's own words as `text`, a title of your own when the note has no
   heading. Pass `from` with the note's path on the last plant for that
   note, so the note is removed once every idea from it has a page.
3. Report every page planted and which notes were removed.

Task notes are not sources: do not capture them, and do not run the ingest
skill on them. Notes elsewhere in `inbox/` are sources for `wiki-ingest`.

The text on the page is the user's; never rewrite it.
