# Checklists & Notes — Design

## Overview

Checklist items are ordered children of a task. Each can carry a free-form `notes` blob addressed separately so it doesn't bloat list responses. All permission checks run on the *parent task* — there's no item-level override.

## Components

### `checklist <task_id>` (list)
`GET /api/v1/tasks/:task_id/checklist`. Text fallback: tabwriter-aligned table with `[x]` / `[ ]` completion boxes and a `notes` marker when `has_notes` is true.

### `checklist add <task_id>`
`POST /api/v1/tasks/:task_id/checklist` body `{"checklist_item":{…}}`. Flags: `--title`, `--notes`, `--from-json <path|->`. Server appends and assigns position.

### `checklist check <item_id>` / `checklist uncheck <item_id>`
Both hit `PATCH /api/v1/checklist_items/:id/completed` with body `{"completed": true}` or `{"completed": false}`. Server is idempotent — no-ops when the state already matches but still echoes the item.

### `checklist update <item_id>`
`PATCH /api/v1/checklist_items/:id` body `{"checklist_item":{…}}`. `--title` / `--notes` / `--from-json`. Completion isn't mutable here — use `check` / `uncheck`.

## Error shapes

Task / checklist create / update endpoints wrap server-side validation:

- Non-object nested body (e.g. `{"task": "foo"}` or `{"checklist_item": []}`) → 422 `invalid_body`. The CLI always wraps attrs in a JSON object, so this is a guard for malformed external payloads rather than something `sprawl` itself produces.
- Changeset failures (missing required field, etc.) → shared fallback shape `{"errors": {...}}` with no top-level `error` code; `reportErr` surfaces these as `error: "invalid"` + `details: <errors>` in json / toon output.

### `note show <item_id>`
`GET /api/v1/checklist_items/:id/notes`. Text fallback is the raw notes blob so it pipes cleanly into `less` / `rg`. Empty notes is a valid success.

### `note set <item_id> [<notes>]`
`PUT /api/v1/checklist_items/:id/notes` body `{"notes":"…"}`. Text via a positional arg OR `--stdin` (mutually exclusive; both → local error before HTTP). Empty string clears notes.

## Design Decisions

- **`check` / `uncheck` instead of `toggle`**: agents don't reliably know current state. Explicit verbs match the server's explicit-bool endpoint and avoid a GET-then-PATCH race.
- **Notes as a separate endpoint**: keeps list responses small when notes are large; makes clearing notes a distinct, auditable action.
- **`note set` positional-or-stdin**: supports both interactive (`sprawl note set 8 "…"`) and piped (`cat draft.md | sprawl note set 8 --stdin`). Both paths at once is rejected locally.
