# Tasks — Technical

## Architecture

`internal/client/client.go` exposes `ListTasks`, `SearchTasks`, `GetTask`, `CreateTask`, `UpdateTask`, `SetTaskDueDate`. `internal/cli/task.go` wires the cobra subcommand tree, resolves auth via `newAuthedClient`, and renders with `taskMap` / `actorMap` / `projectMap`. Write commands share `internal/cli/attrs.go` helpers (`loadJSONFromSource`, `mergeStringFlag`, `mergeProjectID`, `requireAttrs`) with `checklist add` / `checklist update`; `task due` skips the attrs helpers because its body is a single typed field, not a free-form attrs map.

## Source Files

| File | Role |
|------|------|
| `internal/client/client.go` | HTTP methods for `/api/v1/tasks*`, `APIError` decoding. |
| `internal/cli/task.go` | Cobra subcommands; `taskMap` / `actorMap` / `projectMap` renderers. |
| `internal/cli/attrs.go` | Shared `--from-json` + flag-merge helpers for write commands. |

## Wire Shapes

Envelopes: `{"task": {…}}` for single-task responses, `{"tasks": [...]}` for lists. The CLI preserves the envelopes in JSON / TOON output. Key fields on a task: `id`, `title`, `description`, `status`, `due_date`, `project`, `checklist_progress`, `created_by`, `last_actor`.

`task due` uses a dedicated route, `PATCH /api/v1/tasks/:id/due_date`, with body `{"due": "yesterday" | "today" | "week" | null}`. Response is the same `{"task": {…}}` envelope, where `due_date` is the *resolved* ISO date the server computed in the user's timezone (or `null` after a clear). Asymmetry: write takes a preset name, read returns a date — there's no server-side echo of the preset, so a CLI that wants to render "currently set to: today" must compare the date itself. Validation is server-side: `due` outside the four accepted values surfaces as `APIError` Status 422 Code `invalid_due` (the CLI filters to those four locally, so this is unreachable except via a bug).

## Noteworthy Behavior

- **Empty-attrs rejection happens in the CLI**, before any HTTP call — `requireAttrs` guards against a silent no-op POST / PATCH.
- **`--project-id` is parsed locally as an int** so bad input fails fast with a clean error (not a server 422).
- **`mergeStringFlag` checks `cmd.Flags().Changed(…)`** so `--description ""` explicitly clears while an omitted flag leaves the field alone.

## Dependencies

- Server-side per-agent permission resolver. The CLI only surfaces 403 / 404 / 422.
