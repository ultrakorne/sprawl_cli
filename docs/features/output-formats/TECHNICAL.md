# Output Formats — Technical

## Architecture

`internal/cli/output.go` centralises format resolution and rendering. Every subcommand builds a `map[string]any` payload (even when the client returns typed structs) and hands it to `renderPayload` alongside a text-fallback string. `reportErr` wraps errors in the same renderer so command failure paths emit either the structured envelope or a stderr message depending on resolved format. `internal/cli/style.go` owns the human (`text`) styling: the lipgloss style set, the styling gate, and the column-aligned table renderer.

## Source Files

| File | Role |
|------|------|
| `internal/cli/output.go` | `resolveFormat`, `renderPayload`, `reportErr`, JSON / TOON encoders, and `writeHuman` (the colorprofile write choke point). |
| `internal/cli/root.go` | Registers the persistent `--format` and `-h`/`--human` flags, reads `SPRAWL_OUTPUT`, reclaims `-h` from cobra's help, and calls `enableStylingFor` in `PersistentPreRunE`. |
| `internal/cli/style.go` | `styler` (lipgloss styles), `stylesEnabled` gate, `statusStyle` / `checkboxStyle`, and `renderTable` (color-aware aligned tables). |

## Noteworthy Behavior

- **Typed structs never reach the renderer.** Client functions return `*client.Task` / `*client.ChecklistItem` / `*client.ItemDetail`, but the CLI layer converts them via `taskMap` / `itemMap` / `itemTaskMap` / `actorMap` / `projectMap` so the rendered shape is stable across all three formats. Null server fields (`project`, `created_by`, `last_actor`) surface as literal `nil` in TOON and `null` in JSON.
- **`itemMap` is the only builder of an item payload**, shared by `task <id>`, `item <id>` and `queue`. It deliberately does **not** pass the server's item through unchanged:
  - `state` is emitted **short-form** — `ready` / `progress` / `review` / `null`, never `ready_to_pickup` etc. Input still accepts both spellings (Postel; it costs one map lookup that already exists). This is what keeps an item's state from colliding with a task's `status`, which really is `in_progress`. A consumer diffing CLI json against a PubSub payload or the web app will see `progress` vs `in_progress` — that divergence is deliberate.
  - `pr_url` is **added** — `client.PRURL` applied to whichever project the view has in hand. `null` when the item has no PR number, the task has no project, or the project has no repo URL; none of the three is an error. Building it centrally is the point of the project riding along at all: every consumer would otherwise concatenate it by hand, and the "render it unlinked" rule is easy to get wrong.
  - `position` is **dropped**. It drove only the `POS` column, which is gone; array order already carries ordering, since the server returns items in position order.
  - `notes` rides only where the bodies were actually fetched — always on `item <id>`, and under `--full` for `task <id>` / `queue`. `has_notes` rides always, because it's what drives the `NOTES` column. Empty notes collapse to a literal `null` (never `""`), for one uniform "empty ⇒ null" contract.
- **`withNotes` is a parameter, not an inference.** The server serialises an empty note as `null`, which decodes identically to an absent field, so the payload alone cannot say whether bodies were fetched. It is threaded down from the caller's `--full`.
- **`reportErr` always returns the original error unchanged** so cobra's `RunE` exits non-zero regardless of how the error was rendered.
- **Unwrapping `*client.APIError`** populates `http_status` + `error` fields from `Code`. When `Code` is empty, `reportErr` tries to decode the body as the shared changeset fallback `{"errors": {...}}`; if it matches, the envelope gains `error: "invalid"` + `details: <errors>`, otherwise the raw body goes into `error`.
- **`isNotFoundAPIError` keys on the `error` field, not the status alone.** It matches only `404` *and* `Code == "not_found"`, which is what makes `task delete` / `item delete` idempotent without also swallowing a `theme_not_found`. The consequence is a live requirement on the server: **every `/api/v1/*` 404 must return the `{"error": "not_found"}` envelope**, including requests to paths that no longer route. A bare Phoenix `NoRouteError` page (HTML, or the dev error view) decodes to an empty `Code`, so the raw body lands in `error` and the idempotent-delete shortcut silently stops applying. That mostly bites *older* CLI binaries still calling retired routes — the current one doesn't — which is exactly who the guarantee is for. An `/api/v1` catch-all on the server is what upholds it.
- **Styling is gated, not woven in.** Text builders call `sty.render` / `renderTable`, which are no-ops unless the process-level `stylesEnabled` flag is set. `root.go`'s `PersistentPreRunE` sets it (via `enableStylingFor`) only when the resolved format is `text` **and** stdout is a real TTY (strict `term.IsTerminal`, so `CLICOLOR_FORCE` can't flip a test buffer). Because it stays false by default, unit tests that call builders directly see plain strings and their substring assertions keep matching.
- **`writeHuman` is the single styled-output write path.** It wraps the destination in a `colorprofile.Writer`, which strips/downsamples escapes per the detected profile and honors `$NO_COLOR` / `CLICOLOR*`. So even if a builder emitted color, a non-terminal or `NO_COLOR` sink receives plain text.
- **`-h` reclaim.** cobra auto-registers `--help` with a `-h` shorthand; `root.go` pre-defines a long-only `--help` persistent flag so cobra skips that, freeing `-h` for `--human` on every command while `--help` keeps working.
- **Colored tables stay aligned** because `renderTable` measures column widths from each cell's *plain* text and pads manually — Go's `text/tabwriter` counts a colored cell's escape bytes toward its width, so it can't align colored columns.

## Dependencies

- `github.com/alpkeskin/gotoon` — TOON renderer.
- `charm.land/lipgloss/v2` — text styling (ANSI palette colors, bold/faint).
- `github.com/charmbracelet/colorprofile` — profile detection + escape stripping at write time (`writeHuman`).
- `github.com/charmbracelet/x/term` — strict TTY check for the styling gate.
- stdlib `encoding/json`.
