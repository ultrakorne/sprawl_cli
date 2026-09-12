# Tasks — Technical

## Architecture

`internal/client/client.go` exposes `ListTasks`, `SearchTasks`, `GetTask`, `CreateTask`, `UpdateTask`, `SetTaskDueDate`, `DeleteTask`. `GetTask(ctx, id, full)` appends `?full=true` when `full` is set. `internal/cli/task.go` wires the cobra subcommand tree, resolves auth via `newAuthedClient`, and renders with `taskMap` / `actorMap` / `projectMap`. The `task` parent command has no `show` subcommand — its own `RunE` (Args `ExactArgs(1)`) handles `task <id>`, mirroring `item`'s parent, and binds the `--full` bool. The human rendering (`taskShowText`) is a thin composition: `taskHeader` + `itemTable`, both from `internal/cli/item_render.go`, which is the single source of item rendering across `task <id>`, `item <id>` and `queue`. Write commands share `internal/cli/attrs.go` helpers (`loadJSONFromSource`, `mergeStringFlag`, `mergeProjectID`, `requireAttrs`) with `item add` / `item update`; `task due` and `task delete` skip the attrs helpers — `due`'s body is a single typed field and `delete` has no body at all.

## Source Files

| File | Role |
|------|------|
| `internal/client/client.go` | HTTP methods for `/api/v1/tasks*`, `APIError` decoding. `DeleteTask` is the no-body 204 sibling of the other writers. |
| `internal/cli/task.go` | Cobra subcommands; `taskMap` / `actorMap` / `projectMap` renderers. `newTaskDeleteCmd` / `runTaskDelete` handle the soft-delete + idempotent-404 path. |
| `internal/cli/attrs.go` | Shared `--from-json` + flag-merge helpers for write commands. |
| `internal/cli/output.go` | `isNotFoundAPIError` predicate shared by `task delete` / `item delete` for the 404-as-success contract. |

## Wire Shapes

Envelopes: `{"task": {…}}` for single-task responses, `{"tasks": [...]}` for lists. The CLI preserves the envelopes in JSON output. Key fields on a task: `id`, `title`, `description`, `status`, `due_date`, `project`, `checklist_progress`, `created_by`, `last_actor`.

The nested `project` object carries `id`, `name`, `key`, `color`, and `github_url`. `key` and `github_url` are additive; `projectMap` emits both unconditionally, with `github_url` as a literal `null` when the project has no repo — which is also what a pre-rollout server's absent field decodes to, so consumers branch on one condition rather than two. `github_url` is validated server-side to be exactly `https://github.com/<owner>/<repo>`, and is what turns an item's `pr_number` into a link (see [item-state-and-pr](../item-state-and-pr/TECHNICAL.md)). Task `status` is derived from checked counts and is **not** the same thing as an item's hand-set `state`. On the wire both could read `in_progress`; in CLI output they can't, because item states are emitted short-form (`progress`) — see [CONTEXT.md](../../CONTEXT.md).

`GET /api/v1/tasks/:id` embeds a `checklist_items: [...]` array on the task object (ordered by position), each item carrying `has_notes` but no note body. `?full=true` adds `notes` per item — a string, or `null` when empty. The decode side is `Task.ChecklistItems []*ChecklistItem` and `ChecklistItem.Notes *string`.

`taskMap(t, withNotes)` emits `checklist_items` only when the slice is non-nil (mirroring `MatchedChecklistItems`), so list / search / create / update payloads keep their existing shape — those responses carry no items at all. **An empty checklist is `[]` on the wire, not a missing key**, so `nil` and `[]` mean genuinely different things and the suppression is correct in both directions. Each item goes through the shared `itemMap`, which resolves `pr_url` from the task's own project — the read path where the whole chain is in hand.

`withNotes` is a parameter rather than something inferred from the payload: the server serializes an empty note as `null`, which decodes identically to an absent field, so the wire alone can't say whether bodies were fetched. Guessing wrong would report "no note" for a note that exists. It is simply the caller's `--full`.

The human view is `taskShowText`: `taskHeader` (bold title, then the description on following lines, wrapped to `outputWidth`) and then `itemTable`. There is no card — `taskDetailText`, `taskCard`, `taskMetaLines`, `metaGrid`, `renderTitledBox` and friends were all deleted. `--full` passes through to `itemTable`, which expands each note under its row, indented to the `TITLE` column's offset as reported by `colWidths` / `colOffset` (the same layout `renderTable` just used, so the two can't drift). Long notes wrap via `ansi.Wrap` to the remaining width with a hanging indent; the width comes from `outputWidth`, captured once per Execute beside `stylesEnabled` in `enableStylingFor` (0 ⇒ non-terminal / unknown ⇒ print authored lines verbatim). Structural characters — padding, the header rule, the note indentation — are emitted unconditionally; only colour and the OSC 8 link are gated on `stylesEnabled`, so stripping ANSI reproduces the plain rendering exactly. `TestStyling_PreservesPlainLayout` locks that in across `task show`, `task show --full`, `queue` and `queue --full`, and `TestTaskFullText_WrapsNotesWithHangingIndent` covers the wrap path.

`/tasks/search` responses additionally include `matched_checklist_items: [{id,title}, …]` per task — present (possibly `[]`) only on search payloads, absent everywhere else. The decode side uses `client.MatchedChecklistItem` and a `nil`-vs-empty distinction on `Task.MatchedChecklistItems` so `taskMap` can suppress the key on list/show/create/update output and pass it through verbatim on search output (including the `[]` "matched on title" signal).

`MatchedChecklistItem` is its own two-field type, NOT a `ChecklistItem` with most fields unset. The server sends only `{id, title}` here, and a full struct with two fields populated is indistinguishable from a real item that happens to be unchecked and stateless — so `taskSearchText` would render `[ ]` and `-` for items that are neither, and nothing downstream could tell those zero values from real ones. `taskMap` emits the two fields directly rather than routing them through `itemMap`, for the same reason: a `state: null` / `pr_url: null` the server never sent presents absence as data.

`task due` uses a dedicated route, `PATCH /api/v1/tasks/:id/due_date`, with body `{"due": "yesterday" | "today" | "week" | null}`. Response is the same `{"task": {…}}` envelope, where `due_date` is the *resolved* ISO date the server computed in the user's timezone (or `null` after a clear). Asymmetry: write takes a preset name, read returns a date — there's no server-side echo of the preset, so a CLI that wants to render "currently set to: today" must compare the date itself. Validation is server-side: `due` outside the four accepted values surfaces as `APIError` Status 422 Code `invalid_due` (the CLI filters to those four locally, so this is unreachable except via a bug).

`task delete` is `DELETE /api/v1/tasks/:id` with no request body and a 204 No Content response. The CLI never reads the response body — `do()` is already 204-safe (it skips the JSON decode when `out` is nil or the body is empty, see `client.go` `doWithStatus`), so passing `nil` for `out` is the correct shape. `runTaskDelete` calls `DeleteTask`, swallows `*client.APIError` with Status 404 + Code `not_found` via `isNotFoundAPIError`, and renders a synthetic `{id, deleted: true}` payload through `renderPayload`. All other errors flow through `reportErr`.

## Noteworthy Behavior

- **Empty-attrs rejection happens in the CLI**, before any HTTP call — `requireAttrs` guards against a silent no-op POST / PATCH.
- **`--project-id` is parsed locally as an int** so bad input fails fast with a clean error (not a server 422).
- **`mergeStringFlag` checks `cmd.Flags().Changed(…)`** so `--description ""` explicitly clears while an omitted flag leaves the field alone.
- **`task <id>` resolves PR links because it carries the project.** Every `itemView` it builds gets `t.Project`, so `client.PRURL` can assemble the link and the `PR` cell becomes an OSC 8 hyperlink. A projectless task, or a project with no repo URL, renders the bare `#<n>` — neither is an error.
- **`task delete` is idempotent against 404 `not_found` only.** `isNotFoundAPIError` matches `*client.APIError` with `Status==404` and `Code=="not_found"`; codes such as `theme_not_found` still surface as errors. The 204 success and a swallowed 404 emit byte-identical payloads, so retries are safe.

## Dependencies

- Server-side per-agent permission resolver. The CLI only surfaces 403 / 404 / 422.
