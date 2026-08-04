# Items — Technical

## Architecture

`internal/client/client.go` exposes `GetChecklistItem`, `CreateChecklistItem`, `SetChecklistItemCompleted`, `UpdateChecklistItem`, `SetChecklistItemState`, `DeleteChecklistItem`, `ListChecklistItemsByState`. `internal/cli/item.go` wires the `item` command tree: the parent `RunE` is the show behaviour, while `add` / `update` / `check` / `uncheck` / `state` / `pr` / `delete` dispatch as sibling subcommands. `state` / `pr` are covered in [item-state-and-pr](../item-state-and-pr/TECHNICAL.md), not here.

**`internal/cli/item_render.go` is the single source of item rendering.** `task <id>`, `item <id>` and `queue` all call it; none of them builds a row. `TestItemRow_IdenticalCellsAcrossViews` is what keeps that true — it asserts the three views produce byte-identical cells for the same item, so the moment one grows its own row the test fails.

## Source Files

| File | Role |
|------|------|
| `internal/cli/item_render.go` | The shared renderer: `itemCols` / `itemView`, `itemTableHeader`, `itemRow`, `itemTable`, `itemNoteLines`, `taskHeader`, `itemMap`, `wrapLines`, plus the `stateLabel` / `stateStyle` / `checkbox` / `notesFlag` primitives. |
| `internal/cli/item.go` | Parent `item` command + `add` / `update` / `check` / `uncheck` / `state` / `pr` / `delete`; `runItemShow`, `itemDetailMap`, the `✓ …` write summaries, `parseState` / `parsePRNumber`. |
| `internal/cli/queue.go` | The cross-task read; adapts `ItemDetail`s to `itemView`s and feeds the same table. |
| `internal/cli/task.go` | `taskShowText` — the title/description header plus the same table. |
| `internal/client/client.go` | HTTP methods for `/api/v1/checklist_items/*` and `POST /api/v1/tasks/:id/checklist`. `DeleteChecklistItem` is the no-body 204 sibling of the other writers. |
| `internal/icons/icons.go` | The state-glyph vocabulary, shared with the TUI so both surfaces render a state identically. `SPRAWL_ICONS=plain` opts out of the Nerd Font set. |
| `internal/cli/style.go` | `renderTable` / `colWidths` / `colOffset` (the layout the notes hang off), `col.link` + `hyperlink` (OSC 8). |
| `internal/cli/attrs.go` | Shared `--from-json` + flag-merge helpers. |
| `internal/cli/output.go` | `isNotFoundAPIError` predicate shared with `task delete` for the 404-as-success contract. |

## Wire Shapes

- `checklist_item` fields on the wire: `id`, `title`, `completed`, `position`, `state`, `pr_number`, `has_notes`, `last_actor`, and `notes` where the bodies were fetched. `state` and `pr_number` are nullable and decode to their zero value (`""` / `0`) rather than a pointer; the renderers map that back to a literal `null`, so both keys are always present in output — see [item-state-and-pr](../item-state-and-pr/TECHNICAL.md).
- **`itemMap` is the only builder of an item payload**, and it deliberately differs from the wire in three ways — see [output-formats](../output-formats/TECHNICAL.md) for the full contract:
  - `state` is emitted **short-form** (`ready` / `progress` / `review` / `null`), never the server's spellings.
  - `pr_url` is **added**, resolved via `client.PRURL` from whichever project the view has in hand. `null` when the chain breaks.
  - `position` is **dropped**. Array order carries ordering; the server returns items in position order.
  - `notes` rides only when `withNotes` is set. `has_notes` always rides.
- `GET /api/v1/checklist_items/:id` returns `{"checklist_item": {…, "task": {"id", "title", "project"}}}`, decoded into `client.ItemDetail` (a `ChecklistItem` plus an `ItemTask`). The same struct backs the by-state queue, whose elements have the identical shape. The task stub is **required**: it carries the project, and the project's `github_url` is what turns `pr_number` into a link.
- `queue --full` sends `?full=true` on `GET /api/v1/checklist_items`. **The server does not implement that parameter yet** — it ignores it and returns items without `notes`, so `--full` currently renders identically to a plain `queue`. The CLI side is complete and the flag needs no change once the server adds the key, exactly as it did for the task read.
- `GET /api/v1/tasks/:id` embeds `checklist_items` **with** `has_notes` and **without** `notes`; `?full=true` adds `notes` (null when empty). An empty checklist is `[]`, not a missing key — so `Task.ChecklistItems == nil` means "this response doesn't carry items at all" (list / search / create / update) and `[]` means "this task has none", and `taskMap` suppresses the key only in the first case.
- `item delete` is `DELETE /api/v1/checklist_items/:id` with no request body and a 204 No Content response. The CLI relies on `do()` being 204-safe — it skips the JSON decode when `out` is nil or the body is empty (see `client.go` `doWithStatus`) — so passing `nil` for `out` is the correct shape. `runItemDelete` swallows `*client.APIError` with Status 404 + Code `not_found` via `isNotFoundAPIError` (shared with `task delete`) and renders a synthetic `{id, deleted, existed}` payload.

## Noteworthy Behavior

- **Cobra routes exact subcommand matches first.** `sprawl item <id>` with a positional falls through to the parent `RunE` for the show behaviour; `sprawl item check <id>` dispatches to the `check` subcommand before the show RunE sees it. Same pattern as `task <id>`.
- **Notes hang off the layout, not off a guess.** `itemTable` asks `colWidths` where the `ID` column starts and indents the note lines to it, so the block tracks the table whatever the checkbox and state columns widen to. `noteMinIndent` floors it at 2 so the queue — where `ID` is column 0 — doesn't put notes against the margin. Continuation lines align under the note *body*, not under the `note:` label.
- **OSC 8 goes outside the styling, never inside.** Feeding an escape into lipgloss makes it render the URL as literal text. `layoutRow` styles the cell, then `hyperlink` wraps it — the same ordering constraint the TUI documents on `Model.prField`. The escapes are zero-width so they never enter the width maths, and they're gated on `stylesEnabled` like colour: a pipe gets the characters and nothing else.
- **State glyphs must be one cell.** The `STATE` column is a constant width; a two-cell glyph shears every row carrying it. `icons.Width` pins it, and both `internal/icons` and the TUI assert against it.
- **`SetChecklistItemCompleted` sends `{"completed": bool}`** with `Content-Type: application/json`. This replaced an earlier bodyless `/:id/toggle` endpoint.
- **Completion clears the item's state, server-side.** The CLI doesn't model that — the echoed item carries the new truth — but anything caching an item across a check must take the response, not patch its own copy.
- **`item update` is the one write route whose response is serialised in full**, so it echoes the saved note back. `runItemUpdate` passes `withNotes: true` for exactly that reason; every other write verb passes `false`, because their responses genuinely don't carry notes and emitting a `null` would claim the note was cleared.
- **A write response carries no task, so no project, so no `pr_url`.** `renderItemWrite` passes a nil project deliberately: the number is there, the link isn't resolvable from that payload alone, and inventing one would be a lie.
- **`--notes -` and `--from-json -` can't coexist.** Both read stdin; whichever runs second gets nothing. `itemWriteFlags.buildAttrs` rejects the combination locally, before any HTTP call, so the error renders in the chosen format.
- **Clearing a note is a distinct success.** An empty-string body is accepted server-side; don't treat it as missing input. `cmd.Flags().Changed("notes")` is what distinguishes "clear it" from "leave it alone" — a zero value can't.
- **`item delete` is hard, with no recovery path.** Unlike `task delete`'s soft-delete, the row is gone after a successful 204. The idempotent-404 contract is what makes retries safe; the 403 case deliberately isn't covered by it.
- **Parent `completed_at` flip is server-driven.** The CLI doesn't observe or report it on the delete response (the response is empty). Callers that care about the new state should re-fetch the parent task.

## Dependencies

- Parent-task permission checks on the server — items inherit. The CLI surfaces 403 (exists, out of reach — including any id outside a project key's project) and 404 (doesn't exist).
- `internal/icons` — shared with `internal/tui`; changing a glyph changes both surfaces at once, which is the point.
