# Checklists & Notes — Technical

## Architecture

`internal/client/client.go` exposes `ListChecklistItems`, `CreateChecklistItem`, `SetChecklistItemCompleted`, `UpdateChecklistItem`, `DeleteChecklistItem`, `GetNotes`, `SetNotes`. `ListChecklistItems(ctx, taskID, full)` appends `?full=true` when `full` is set. `internal/cli/checklist.go` wires the `checklist` command tree; the parent `RunE` handles the listing behaviour (and binds the `--full` bool) while `add` / `check` / `uncheck` / `update` / `delete` dispatch as sibling subcommands. `internal/cli/note.go` wires the top-level `note` command.

## Source Files

| File | Role |
|------|------|
| `internal/client/client.go` | HTTP methods for `/api/v1/tasks/:id/checklist` and `/api/v1/checklist_items/*`. `DeleteChecklistItem` is the no-body 204 sibling of the other writers. |
| `internal/cli/checklist.go` | Parent `checklist` command + `add` / `check` / `uncheck` / `update` / `delete` subcommands; `checklistItemMap` / `checklistMaps`. `newChecklistDeleteCmd` / `runChecklistDelete` handle the hard-delete + idempotent-404 path. |
| `internal/cli/note.go` | `note show` / `note set` top-level commands. |
| `internal/cli/attrs.go` | Shared `--from-json` + flag-merge helpers. |
| `internal/cli/output.go` | `isNotFoundAPIError` predicate shared with `task delete` for the 404-as-success contract. |

## Wire Shapes

- `checklist_item` fields: `id`, `title`, `completed`, `position`, `has_notes`, `last_actor`. On the `?full=true` read paths each item additionally carries `notes` (a string, `""` when none) — decoded into `ChecklistItem.Notes *string` (`nil` ⇒ field absent on a non-full fetch or a single-item write response). `checklistItemMap` emits the `notes` key only when the pointer is non-nil, so non-full and write responses keep their shape.
- List envelope: `{"checklist_items": [...]}` (same shape full or not — full just adds `notes` per item). Single-item envelope: `{"checklist_item": {…}}`. Notes envelope: `{"notes": "…"}`.
- `fullChecklistText(items, indent)` renders the shared full text block for both `checklist <id> --full` (indent `""`) and the embedded `checklist:` section under `task <id> --full` (indent `"  "`); notes sit six columns past the item line and keep their line breaks. Callers handle the empty-slice case.
- `checklist delete` is `DELETE /api/v1/checklist_items/:id` with no request body and a 204 No Content response. The CLI relies on `do()` being 204-safe — it skips the JSON decode when `out` is nil or the body is empty (see `client.go` `doWithStatus`) — so passing `nil` for `out` is the correct shape. `runChecklistDelete` calls `DeleteChecklistItem`, swallows `*client.APIError` with Status 404 + Code `not_found` via `isNotFoundAPIError` (shared with `task delete`, see tasks/TECHNICAL.md), and renders a synthetic `{id, deleted: true}` payload through `renderPayload`.

## Noteworthy Behavior

- **Cobra routes exact subcommand matches first.** `sprawl checklist <task_id>` with a positional falls through to the parent `RunE` for listing; `sprawl checklist check <id>` dispatches to the `check` subcommand before the list RunE sees it. `delete` follows the same pattern.
- **`SetChecklistItemCompleted` sends `{"completed": bool}`** with `Content-Type: application/json`. This replaced an earlier bodyless `/:id/toggle` endpoint.
- **`note set` validates positional-vs-stdin locally**, before any HTTP call, so the error renders in the chosen format.
- **Clearing notes is a distinct success.** Empty-string body is accepted server-side; don't treat it as missing-input.
- **`checklist delete` is hard, with no recovery path.** Unlike `task delete`'s soft-delete, the row is gone after a successful 204. The CLI doesn't try to fetch-then-delete; the idempotent-404 contract is what makes retries safe.
- **Parent `completed_at` flip is server-driven.** The CLI doesn't observe or report it on the delete response (the response is empty). Callers that care about the new state should re-fetch the parent task.

## Dependencies

- Parent-task permission checks on the server — items and notes inherit. The CLI only surfaces 403 / 404.
