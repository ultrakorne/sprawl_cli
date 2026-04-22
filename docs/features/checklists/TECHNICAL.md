# Checklists & Notes — Technical

## Architecture

`internal/client/client.go` exposes `ListChecklistItems`, `CreateChecklistItem`, `SetChecklistItemCompleted`, `UpdateChecklistItem`, `GetNotes`, `SetNotes`. `internal/cli/checklist.go` wires the `checklist` command tree; the parent `RunE` handles the listing behaviour while `add` / `check` / `uncheck` / `update` dispatch as sibling subcommands. `internal/cli/note.go` wires the top-level `note` command.

## Source Files

| File | Role |
|------|------|
| `internal/client/client.go` | HTTP methods for `/api/v1/tasks/:id/checklist` and `/api/v1/checklist_items/*`. |
| `internal/cli/checklist.go` | Parent `checklist` command + `add` / `check` / `uncheck` / `update` subcommands; `checklistItemMap` / `checklistMaps`. |
| `internal/cli/note.go` | `note show` / `note set` top-level commands. |
| `internal/cli/attrs.go` | Shared `--from-json` + flag-merge helpers. |

## Wire Shapes

- `checklist_item` fields: `id`, `title`, `completed`, `position`, `has_notes`, `last_actor`.
- List envelope: `{"checklist_items": [...]}`. Single-item envelope: `{"checklist_item": {…}}`. Notes envelope: `{"notes": "…"}`.

## Noteworthy Behavior

- **Cobra routes exact subcommand matches first.** `sprawl checklist <task_id>` with a positional falls through to the parent `RunE` for listing; `sprawl checklist check <id>` dispatches to the `check` subcommand before the list RunE sees it.
- **`SetChecklistItemCompleted` sends `{"completed": bool}`** with `Content-Type: application/json`. This replaced an earlier bodyless `/:id/toggle` endpoint.
- **`note set` validates positional-vs-stdin locally**, before any HTTP call, so the error renders in the chosen format.
- **Clearing notes is a distinct success.** Empty-string body is accepted server-side; don't treat it as missing-input.

## Dependencies

- Parent-task permission checks on the server — items and notes inherit. The CLI only surfaces 403 / 404.
