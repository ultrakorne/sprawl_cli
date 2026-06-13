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

- **Typed structs never reach the renderer.** Client functions return `*client.Task` / `*client.ChecklistItem`, but the CLI layer converts them via `taskMap` / `checklistItemMap` / `checklistMaps` / `actorMap` / `projectMap` so the rendered shape is stable across all three formats. Null server fields (`project`, `created_by`, `last_actor`) surface as literal `nil` in TOON and `null` in JSON.
- **`reportErr` always returns the original error unchanged** so cobra's `RunE` exits non-zero regardless of how the error was rendered.
- **Unwrapping `*client.APIError`** populates `http_status` + `error` fields from `Code`. When `Code` is empty, `reportErr` tries to decode the body as the shared changeset fallback `{"errors": {...}}`; if it matches, the envelope gains `error: "invalid"` + `details: <errors>`, otherwise the raw body goes into `error`.
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
