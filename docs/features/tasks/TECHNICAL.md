# Tasks — Technical

## Architecture

`internal/client/client.go` exposes `ListTasks`, `SearchTasks`, `GetTask`, `CreateTask`, `UpdateTask`, `SetTaskDueDate`, `DeleteTask`. `GetTask(ctx, id, full)` appends `?full=true` when `full` is set. `internal/cli/task.go` wires the cobra subcommand tree, resolves auth via `newAuthedClient`, and renders with `taskMap` / `actorMap` / `projectMap`. The `task` parent command has no `show` subcommand — its own `RunE` (Args `ExactArgs(1)`) handles `task <id>`, mirroring `checklist`'s parent, and binds the `--full` bool. Write commands share `internal/cli/attrs.go` helpers (`loadJSONFromSource`, `mergeStringFlag`, `mergeProjectID`, `requireAttrs`) with `checklist add` / `checklist update`; `task due` and `task delete` skip the attrs helpers — `due`'s body is a single typed field and `delete` has no body at all.

## Source Files

| File | Role |
|------|------|
| `internal/client/client.go` | HTTP methods for `/api/v1/tasks*`, `APIError` decoding. `DeleteTask` is the no-body 204 sibling of the other writers. |
| `internal/cli/task.go` | Cobra subcommands; `taskMap` / `actorMap` / `projectMap` renderers. `newTaskDeleteCmd` / `runTaskDelete` handle the soft-delete + idempotent-404 path. |
| `internal/cli/attrs.go` | Shared `--from-json` + flag-merge helpers for write commands. |
| `internal/cli/output.go` | `isNotFoundAPIError` predicate shared by `task delete` / `checklist delete` for the 404-as-success contract. |

## Wire Shapes

Envelopes: `{"task": {…}}` for single-task responses, `{"tasks": [...]}` for lists. The CLI preserves the envelopes in JSON / TOON output. Key fields on a task: `id`, `title`, `description`, `status`, `due_date`, `project`, `checklist_progress`, `created_by`, `last_actor`.

`task <id> --full` requests `GET /api/v1/tasks/:id?full=true`, which adds a `checklist_items: [...]` array to the task object (ordered by position), each item being the usual checklist-item shape plus a `notes` value — a string, or `null` when the item has no notes. The decode side adds `Task.ChecklistItems []*ChecklistItem` and `ChecklistItem.Notes *string`. `taskMap` emits `checklist_items` only when the slice is non-nil (mirroring `MatchedChecklistItems`), so non-full task / list / search payloads keep their existing shape. `checklistItemMap` takes a `full` flag rather than keying off the pointer: the server serializes empty notes as `null`, which decodes identically to an absent field, so the wire alone can't say whether notes were fetched. On the full path it always emits `notes`, collapsing empty (`nil` or a legacy `""`) to a literal `null`; on non-full lists and single-item-write responses it omits the key. Both the non-full detail and the `--full` read render the identical card via `taskDetailText` → `taskCard`: a rounded box with the id/title in the top border, a `project · due · progress` grid (`taskMetaLines`), and the optional description. On the full path (`ChecklistItems` non-nil) a `CHECKLIST` section is appended — one block per item with a unicode checkbox (`☑`/`☐`), faint id, title, and the notes nested beneath verbatim (`(no notes)` when empty); its heading omits the count since the card already shows progress. last_actor / created_by are dropped from the human view (they still ride in the json/toon payload via `taskMap`). The section is separate from `fullChecklistText`, which still backs `checklist <id> --full`. Long notes wrap (via `ansi.Wrap`) to the remaining terminal width with a hanging indent, so a wrapped line continues under the note's start rather than the left margin; the width comes from `outputWidth`, captured once per Execute beside `stylesEnabled` in `enableStylingFor` (0 ⇒ non-terminal / unknown ⇒ print authored lines verbatim). Box-drawing characters, padding, wrap points, and width are emitted unconditionally; only color is gated on `stylesEnabled` (see `renderTitledBox`), so stripping ANSI reproduces the plain box exactly — `TestStyling_PreservesPlainLayout` locks this in for the full view, and `TestTaskFullText_WrapsNotesWithHangingIndent` covers the wrap path. `/tasks/search` responses additionally include `matched_checklist_items: [{id,title}, …]` per task — present (possibly `[]`) only on search payloads, absent everywhere else. The decode side uses `client.MatchedChecklistItem` and a `nil`-vs-empty distinction on `Task.MatchedChecklistItems` so `taskMap` can suppress the key on list/show/create/update output and pass it through verbatim on search output (including the `[]` "matched on title" signal).

`task due` uses a dedicated route, `PATCH /api/v1/tasks/:id/due_date`, with body `{"due": "yesterday" | "today" | "week" | null}`. Response is the same `{"task": {…}}` envelope, where `due_date` is the *resolved* ISO date the server computed in the user's timezone (or `null` after a clear). Asymmetry: write takes a preset name, read returns a date — there's no server-side echo of the preset, so a CLI that wants to render "currently set to: today" must compare the date itself. Validation is server-side: `due` outside the four accepted values surfaces as `APIError` Status 422 Code `invalid_due` (the CLI filters to those four locally, so this is unreachable except via a bug).

`task delete` is `DELETE /api/v1/tasks/:id` with no request body and a 204 No Content response. The CLI never reads the response body — `do()` is already 204-safe (it skips the JSON decode when `out` is nil or the body is empty, see `client.go` `doWithStatus`), so passing `nil` for `out` is the correct shape. `runTaskDelete` calls `DeleteTask`, swallows `*client.APIError` with Status 404 + Code `not_found` via `isNotFoundAPIError`, and renders a synthetic `{id, deleted: true}` payload through `renderPayload`. All other errors flow through `reportErr`.

## Noteworthy Behavior

- **Empty-attrs rejection happens in the CLI**, before any HTTP call — `requireAttrs` guards against a silent no-op POST / PATCH.
- **`--project-id` is parsed locally as an int** so bad input fails fast with a clean error (not a server 422).
- **`mergeStringFlag` checks `cmd.Flags().Changed(…)`** so `--description ""` explicitly clears while an omitted flag leaves the field alone.
- **`task delete` is idempotent against 404 `not_found` only.** `isNotFoundAPIError` matches `*client.APIError` with `Status==404` and `Code=="not_found"`; codes such as `theme_not_found` still surface as errors. The 204 success and a swallowed 404 emit byte-identical payloads, so retries are safe.

## Dependencies

- Server-side per-agent permission resolver. The CLI only surfaces 403 / 404 / 422.
