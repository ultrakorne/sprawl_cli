# Items — Design

## Overview

Items are the ordered children of a task, and the unit the checkbox toggles. Each carries at most one free-form **note**. All permission checks run on the *parent task* — there is no item-level override.

An item also carries an optional **item state** and **PR number**. Those have their own subcommands and their own route, documented in [item-state-and-pr](../item-state-and-pr/INDEX.md); the interaction to know here is that **checking an item clears its state** (and setting a state un-completes the item — the two are mutually exclusive server-side). The PR number is unaffected by completion.

### One renderer, three views

`task <id>`, `item <id>` and `queue` all render the same item row, built by `internal/cli/item_render.go`. Nothing else builds one. The columns a view shows differ; the cells never do.

```
  [x]  ID   STATE  PR    NOTES  TITLE
  ─────────────────────────────────────────────────
  [x]  203  -      #412  o      add the migration
       note: blocked on the backfill
             retry after 2026-08-05
  [ ]  204  ◐      -     -      write the docs
```

- `[x]` — `checkbox()`, colored by completion. Dropped by `queue`, where every row is incomplete by construction.
- `ID` — **bare, no `#`**. The `#` belongs to PR numbers only; carrying it on both was the confusion.
- `STATE` — the **icon**, not the word: the same glyph the TUI shows, from the shared [`internal/icons`](../../../internal/icons/icons.go). `-` when the item has no state. Dropped by `queue`, where the state is the query and lives in the heading.
- `PR` — `#412`, OSC 8 hyperlinked when the chain resolves (item has a number → task has a project → project has a repo URL). Plain when it doesn't; `-` when there's no number.
- `NOTES` — `o` / `-`, driven by `has_notes`. **Present with and without `--full`** — "is there a note here?" is the cheap question, and it's the one the column answers. A marker rather than a word: the header already says NOTES, so spelling "yes" down the column only widens it.
- `TITLE` — last, unpadded. `queue` appends `TASK` and `PROJECT` after it.

`--full` expands each note on the lines below its row, labelled **`note:`** (singular — an item has at most one). An item with no note gets **nothing**; there is no `(no notes)` placeholder anywhere in the CLI.

The note starts at the **`ID` column**, not under `TITLE`. Aligning it with the title looks tidier but squeezes the body into whatever width is left after every other column — on a queue row that can be half the terminal, and a note is the one thing here that is actually prose. Starting at `ID` buys those columns back; the row directly above is still what the note reads as belonging to. It never sits flush against the left margin, though — `ID` is column 0 on the queue, and a note starting there reads as a new block rather than as part of a row. Continuation lines align under the note *body*, not under the label, so a multi-line note reads as one paragraph.

## Components

### `item <id>` (show)
`GET /api/v1/checklist_items/:id`. The detail read: the note is **always** included, so there is no `--full`. Human output is one row of the shared table with the note expanded beneath it, and **no task header of any kind** — you asked about an item, you get the item. The `task` stub (id, title, project) rides in the json/toon payload, where it's the only thing tying an item id back to its task and the thing that resolved `pr_url`.

### `item add <task_id>`
`POST /api/v1/tasks/:task_id/checklist` body `{"checklist_item":{…}}`. Flags: `--title`, `--notes`, `--from-json <path|->`. Server appends and assigns position. This is the one item verb whose argument is a **task** id — the item doesn't exist yet.

### `item update <id>`
`PATCH /api/v1/checklist_items/:id` body `{"checklist_item":{…}}`. `--title` / `--notes` / `--from-json`. **The only way to write a note.**

- `--notes "text"` sets it.
- `--notes -` reads the whole body from stdin (multi-line, piped).
- `--notes ""` clears it.

`--notes -` and `--from-json -` both claim stdin, so combining them is a local error before any HTTP call.

Completion isn't mutable here — use `check` / `uncheck`. Neither are state or PR number: the server ignores both keys on this route, so they get their own (`item state` / `item pr`). The server requires the `checklist_item` envelope naming `title` or `notes` and rejects an empty write with 422, so a dropped write can't masquerade as a 200.

### `item check <id>` / `item uncheck <id>`
Both hit `PATCH /api/v1/checklist_items/:id/completed` with `{"completed": true|false}`. Server is idempotent — no-ops when the state already matches but still echoes the item.

### `item delete <id>`
`DELETE /api/v1/checklist_items/:id`. **Hard delete** — the row is removed from the database, not soft-deleted, and there is no undo. As a server-side side effect the parent task's `completed_at` is recomputed in the same transaction: it flips to "done" if this was the last unchecked item, or clears if no items remain. Server broadcasts `checklist_item_deleted` on PubSub and returns 204 No Content; the CLI emits `{id, deleted: true, existed: true}` (json/toon) or `Deleted item #<id>` (text).

A 404 `not_found` is treated as success with `existed: false`, so retries and typo'd ids are safe and still distinguishable from a real delete. A **403 is not**: the item exists and is out of reach (an id outside a project key's project), so reporting "already gone" would claim a delete that never happened.

### Write output
Every write verb renders a **one-line summary** — no table, no card. The vocabulary matches the TUI's transient messages so both surfaces say the same thing about the same action.

```
$ sprawl item add 119 --title "…"   →  ✓ added item 206  …
$ sprawl item check 203             →  ✓ checked item 203  add the migration
$ sprawl item state 203 review      →  ✓ item 203 → review
$ sprawl item pr 203 412            →  ✓ item 203 → PR #412
```

## Error shapes

Task / item create / update endpoints wrap server-side validation:

- Non-object nested body (e.g. `{"checklist_item": []}`) or a body missing the envelope entirely → 422 `invalid_body`. The CLI always wraps attrs in the envelope, so this guards malformed external payloads rather than anything `sprawl` itself produces.
- Changeset failures (missing required field, etc.) → shared fallback shape `{"errors": {...}}` with no top-level `error` code; `reportErr` surfaces these as `error: "invalid"` + `details: <errors>` in json / toon output.

## Design Decisions

- **Two nouns, `task` and `item`.** The CLI had grown five separate renderings of one concept and two command namespaces (`checklist`, `note`) for it. Collapsing to two nouns plus `queue`, behind one row renderer, is the whole point of this feature.
- **A note is a field, not an object.** `note show` / `note set` implied a thing with its own identity; it isn't one. An item has at most one note, so the item's own verbs read and write it. This also removes the "which of these two routes writes notes?" question the server had.
- **`check` / `uncheck` instead of `toggle`**: agents don't reliably know current state. Explicit verbs match the server's explicit-bool endpoint and avoid a GET-then-PATCH race.
- **Fields with side effects get their own verb**: completion has `check` / `uncheck`, state and PR number have `state` / `pr`. `update` stays the pure title-and-note changeset, so it can never silently no-op on a key the server ignores.
- **`item <id>` costs a backend endpoint.** Every single-item verb already took a bare item id, but nothing could read one back — an item could be written to and never inspected, and never resolved to its parent task. There is no client-side workaround, so `GET /checklist_items/:id` was added.
- **`item <id>` shows no parent-task context.** An item id is opaque, so nothing on screen says which task it belongs to. Decided deliberately against the `queue` precedent, which does show a TASK column: `queue` crosses tasks, so it has to; `item <id>` answers exactly what you asked. `--format=json` carries the `task` stub for when you need it.
- **Piping a raw note now needs `jq`.** `note show --format=text` used to emit the body verbatim. `item <id> -h` renders a table instead, so extracting a body is `sprawl item 203 --format=json | jq -r '.checklist_item.notes'`. Accepted: the table is what a human wants, and one `jq` is a small price for one fewer command.
- **`--full` gates the fetch *and* the render.** The plain read carries every item with `has_notes` but no bodies, which is what keeps `task <id>` cheap; `--full` sends `?full=true` and expands them. Splitting the two would mean either always paying for the bodies or never being able to see them in one call.
- **Hard delete with no undo (and 404 = success)**: parallels `task delete`'s idempotent contract for the same retry-friendly reason, but the destruction is real — there's no trash-bin equivalent for items. Callers that want the row preserved should `item uncheck` instead. The `completed_at` flip on the parent task is intentional: removing the last unchecked item legitimately means "all remaining items are done."
- **Synthetic `{id, deleted, existed}` payload on 204**: same reasoning as `task delete` — the server returns no body, but json / toon consumers always need a parseable envelope.
