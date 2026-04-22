# Tasks — Technical

## Architecture

`internal/client/client.go` exposes `ListTasks`, `SearchTasks`, `GetTask`, `CreateTask`, `UpdateTask`. `internal/cli/task.go` wires the cobra subcommand tree, resolves auth via `newAuthedClient`, and renders with `taskMap` / `actorMap` / `projectMap`. Write commands share `internal/cli/attrs.go` helpers (`loadJSONFromSource`, `mergeStringFlag`, `mergeProjectID`, `requireAttrs`) with `checklist add` / `checklist update`.

## Source Files

| File | Role |
|------|------|
| `internal/client/client.go` | HTTP methods for `/api/v1/tasks*`, `APIError` decoding. |
| `internal/cli/task.go` | Cobra subcommands; `taskMap` / `actorMap` / `projectMap` renderers. |
| `internal/cli/attrs.go` | Shared `--from-json` + flag-merge helpers for write commands. |

## Wire Shapes

Envelopes: `{"task": {…}}` for single-task responses, `{"tasks": [...]}` for lists. The CLI preserves the envelopes in JSON / TOON output. Key fields on a task: `id`, `title`, `description`, `status`, `due_date`, `project`, `checklist_progress`, `created_by`, `last_actor`.

## Noteworthy Behavior

- **Empty-attrs rejection happens in the CLI**, before any HTTP call — `requireAttrs` guards against a silent no-op POST / PATCH.
- **`--project-id` is parsed locally as an int** so bad input fails fast with a clean error (not a server 422).
- **`mergeStringFlag` checks `cmd.Flags().Changed(…)`** so `--description ""` explicitly clears while an omitted flag leaves the field alone.

## Dependencies

- Server-side per-agent permission resolver. The CLI only surfaces 403 / 404 / 422.
