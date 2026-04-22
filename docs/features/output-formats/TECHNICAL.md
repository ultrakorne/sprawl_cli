# Output Formats — Technical

## Architecture

`internal/cli/output.go` centralises format resolution and rendering. Every subcommand builds a `map[string]any` payload (even when the client returns typed structs) and hands it to `renderPayload` alongside a text-fallback string. `reportErr` wraps errors in the same renderer so command failure paths emit either the structured envelope or a stderr message depending on resolved format.

## Source Files

| File | Role |
|------|------|
| `internal/cli/output.go` | `resolveFormat`, `renderPayload`, `reportErr`, JSON / TOON encoders. |
| `internal/cli/root.go` | Registers the persistent `--format` flag and reads `SPRAWL_OUTPUT`. |

## Noteworthy Behavior

- **Typed structs never reach the renderer.** Client functions return `*client.Task` / `*client.ChecklistItem`, but the CLI layer converts them via `taskMap` / `checklistItemMap` / `checklistMaps` / `actorMap` / `projectMap` so the rendered shape is stable across all three formats. Null server fields (`project`, `created_by`, `last_actor`) surface as literal `nil` in TOON and `null` in JSON.
- **`reportErr` always returns the original error unchanged** so cobra's `RunE` exits non-zero regardless of how the error was rendered.
- **Unwrapping `*client.APIError`** populates `http_status` + `error` fields from `Code` (falling back to `Body` when `Code` is empty).

## Dependencies

- `github.com/alpkeskin/gotoon` — TOON renderer.
- stdlib `encoding/json`.
