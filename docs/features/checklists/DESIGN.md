# Checklists & Notes — Design

## Overview

Checklist items are ordered children of a task. Each can carry a free-form `notes` blob addressed separately so it doesn't bloat list responses. All permission checks run on the *parent task* — there's no item-level override.

## Components

### `checklist <task_id>` (list)
`GET /api/v1/tasks/:task_id/checklist`. Text fallback: tabwriter-aligned table with `[x]` / `[ ]` completion boxes and a `notes` marker when `has_notes` is true.

`--full` opts into `GET /api/v1/tasks/:task_id/checklist?full=true`, where each item carries its `notes` inline (alongside `has_notes`) — a string, or `null` when the item has no notes — one call instead of an N-call `note show` loop over the items. The json/toon envelope is unchanged (`{checklist_items:[…]}`); each item map gains a `notes` key on the full path (`checklistItemMap` emits it whenever `--full` is set, rendering `null` for empty notes to mirror the server and `note show`). Text mode drops the table for a per-item block (`fullChecklistText`, shared with `task <id> --full`): one line per item plus its notes indented beneath, `(no notes)` when empty.

### `checklist add <task_id>`
`POST /api/v1/tasks/:task_id/checklist` body `{"checklist_item":{…}}`. Flags: `--title`, `--notes`, `--from-json <path|->`. Server appends and assigns position.

### `checklist check <item_id>` / `checklist uncheck <item_id>`
Both hit `PATCH /api/v1/checklist_items/:id/completed` with body `{"completed": true}` or `{"completed": false}`. Server is idempotent — no-ops when the state already matches but still echoes the item.

### `checklist update <item_id>`
`PATCH /api/v1/checklist_items/:id` body `{"checklist_item":{…}}`. `--title` / `--notes` / `--from-json`. Completion isn't mutable here — use `check` / `uncheck`.

### `checklist delete <item_id>`
`DELETE /api/v1/checklist_items/:id`. **Hard delete** — the row is removed from the database, not soft-deleted, and there is no undo. As a server-side side effect, the parent task's `completed_at` is recomputed in the same transaction: it flips to "done" if this was the last unchecked item, or clears if no items remain. Server broadcasts `checklist_item_deleted` on PubSub and returns 204 No Content; the CLI emits `{id: "<item_id>", deleted: true}` (json/toon) or `Deleted checklist item #<item_id>` (text). A 404 `not_found` is treated as success — repeated deletes and deletes against an id that never existed render the same payload. Other 4xx (401/403, malformed) surface through `reportErr`.

## Error shapes

Task / checklist create / update endpoints wrap server-side validation:

- Non-object nested body (e.g. `{"task": "foo"}` or `{"checklist_item": []}`) → 422 `invalid_body`. The CLI always wraps attrs in a JSON object, so this is a guard for malformed external payloads rather than something `sprawl` itself produces.
- Changeset failures (missing required field, etc.) → shared fallback shape `{"errors": {...}}` with no top-level `error` code; `reportErr` surfaces these as `error: "invalid"` + `details: <errors>` in json / toon output.

### `note show <item_id>`
`GET /api/v1/checklist_items/:id/notes`. Text fallback is the raw notes blob so it pipes cleanly into `less` / `rg`. An item with no notes is a valid success: the server returns `null`, which the CLI surfaces as `notes: null` in json/toon and an empty body in text.

### `note set <item_id> [<notes>]`
`PUT /api/v1/checklist_items/:id/notes` body `{"notes":"…"}`. Text via a positional arg OR `--stdin` (mutually exclusive; both → local error before HTTP). Empty string clears notes.

## Design Decisions

- **`check` / `uncheck` instead of `toggle`**: agents don't reliably know current state. Explicit verbs match the server's explicit-bool endpoint and avoid a GET-then-PATCH race.
- **Notes as a separate endpoint**: keeps list responses small when notes are large; makes clearing notes a distinct, auditable action. `--full` is the opt-in escape hatch when a caller genuinely wants every item's notes at once (e.g. an agent reading a whole checklist before starting work) — it's server-assembled (`?full=true`), so the CLI stays a thin wrapper and there's no per-item fan-out or partial-failure handling on the client.
- **`note set` positional-or-stdin**: supports both interactive (`sprawl note set 8 "…"`) and piped (`cat draft.md | sprawl note set 8 --stdin`). Both paths at once is rejected locally.
- **Hard delete with no undo (and 404 = success)**: parallels `task delete`'s idempotent contract for the same retry-friendly reason, but the destruction is real — there's no trash-bin equivalent for items, the row simply goes away. Callers that want the row preserved should `checklist uncheck` instead. The `completed_at` flip on the parent task is intentional: removing the last unchecked item legitimately means "all remaining items are done."
- **Synthetic `{id, deleted: true}` payload on 204**: same reasoning as `task delete` — the server returns no body, but json / toon consumers always need a parseable envelope.
