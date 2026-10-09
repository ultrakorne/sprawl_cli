# Items — Design

## Overview

Items are ordered children of a task and the unit that carries completion,
a note, assignment, item state, and PR number. Permission is inherited from
the parent task. Tasks themselves have no assignee.

## Reading items

`item <id>` returns one item with its note and a parent-task stub in JSON.
Text shows the item alone. `task <id>` and `queue` render the same item cells;
queue groups items beneath their task and omits constant completion/state cells.

The shared table shows completion, a bare item ID, state glyph, PR number,
note-presence marker, typed assignee, and title. The PR links to GitHub when
the parent project has a repository URL. Assignment is `user:<id>` or
`agent_key:<id>`, with `-` for unassigned. Reads need no actor-roster request.

`task <id>` and `queue` show whether a note exists without fetching its body.
`--full` fetches and expands bodies beneath their rows; `item <id>` always
includes its note. Notes align beneath the ID column and preserve authored
line breaks, wrapping only when terminal width is known.

## Writing items

`item add <task_id>` appends an item; the server assigns position. It accepts
`--title`, `--notes`, `--assignee`, and `--from-json <path|->`.
`item update <id>` edits title, note, or assignment and also accepts `--unassign`.
Explicit flags override JSON attributes.

A note belongs to its item. `--notes "text"` replaces it, `--notes ""` clears
it, and `--notes -` reads its entire body from stdin. Combining `--notes -`
with `--from-json -` is a local error because both consume the same input.

### Assignment

`item assign <id> <user:id|agent_key:id>` assigns or reassigns one actor;
`item unassign <id>` clears assignment. Discover targets with `workspace actors`
in the same workspace. Discovery returns stable typed IDs with labels and markers.
It requires write access; ordinary item reads remain usable with read access.

An omitted assignment preserves it (creates unassigned), null clears it, and
an object assigns one actor. Explicit assignment flags override JSON, and
`--assignee` conflicts with `--unassign`. IDs range from 1 to 2147483647.
Assignment preserves completion, state, notes, and PR number.

`queue --assignee me` lists the calling agent key's own incomplete assigned items across
states, and `events watch` streams its assignment changes; see
[assignment-events](../assignment-events/INDEX.md). Each item carries an
`assignment_revision` that counts assignee changes.

The server rechecks eligibility during writes. `invalid_assignee` is a failure
with guidance to refresh discovery; assigned creation is atomic. A stored pair
can be stale after actor revocation. JSON preserves it; text displays its typed
ID without asserting eligibility.

### Other actions

`check` and `uncheck` set completion explicitly, avoiding a read/modify race.
Checking clears item state; PR number and assignment survive. State and PR
writes use their dedicated commands, covered in [item-state-and-pr](../item-state-and-pr/INDEX.md).

`item delete` is a hard delete with no recovery path. A 404 is idempotent
success with `existed: false`; permission refusals remain failures. The server
recomputes parent-task completion.

Every item write returns the normalized item envelope in JSON or a one-line
confirmation with its returned assignment in text. PATCH echoes the saved note;
compact create/completion/state responses omit note bodies. Server validation
and scope errors retain the shared format-aware error handling.
