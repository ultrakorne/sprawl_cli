# Interactive TUI — Technical

## Architecture

The TUI is its own package, `internal/tui`, so cobra stays in `internal/cli`. `launchTUI` resolves the token, the narrowing factors and the workspace selector with the CLI's own resolvers and hands them to the TUI as `Deps`, including a `NewClient(secret, projectKey, workspace)` closure that captures only the bearer — the factors and the selector are supplied per call so the credentials prompt and the workspace picker can rebuild the client without anything leaking into a shared field.

The model is Elm-style bubbletea v2: `Init` returns the validating list fetch (or nothing when starting on the credentials / not-logged-in screen); `Update` folds key presses, window size and the package's typed result messages; `View` returns a `tea.View` with `AltScreen` set (a view field in v2, not a program option). All network access is a `tea.Cmd` built in `internal/tui/msgs.go` against the `Client` interface — the subset of `*client.Client` the model uses, so tests inject a fake or an `httptest`-backed real client. Screens are a back stack (`creds`, `not logged in`, `list`, `checklist`, `note`); overlays (confirm, input, picker, help) are a separate field rendered over the current screen.

## Where things live

| File | Role |
|------|------|
| `internal/tui/tui.go` | `Deps`, `IsTTY`, `Run` (a ctrl+c / kill exit is clean). |
| `internal/tui/model.go` | `Model`, `newModel`, the back stack, filtered selection helpers, the search index, footer status, `replaceItem`, `nextState`, workspace state and picker items. |
| `internal/tui/update.go` | Key dispatch, overlay handling, message reducers, the `s` / `p` / `o` / `w` handlers. |
| `internal/tui/view.go` | Per-screen renderers, `frame`, hint packing, the fixed state / PR columns, search highlighting, OSC 8 link helpers. |
| `internal/tui/msgs.go` | Message types, error classification, and every command that calls the client, including the bounded-concurrency index fill. |
| `internal/tui/fuzzy.go` | Pure `/` matching and ranking: fuzzy titles, word-matched bodies, highlight positions. |
| `internal/tui/client.go` | The `Client` interface, including `Whoami` — the one user-level call the TUI makes. |
| `internal/tui/keymap.go` | `action` enum and the pure `dispatch(screen, key)` map. |
| `internal/tui/style.go` | Terminal-palette styles; the state glyphs come from `internal/icons`. |
| `internal/tui/input.go` | Hand-rolled single-line `textInput`. |
| `internal/tui/markdown.go` | Pure `taskMarkdown` / `itemMarkdown` clipboard formatters. |
| `internal/tui/editor.go` | `$EDITOR` round trip via `tea.ExecProcess`. |
| `internal/tui/open.go` | Detached browser launch for `o`, https-only. |
| `internal/cli/interactive.go` | `sprawl tui` and `launchTUI`. |
| `internal/cli/root.go` | Bare `sprawl` on a TTY → `launchTUI`, else help. |

## Noteworthy

### Validation routes errors differently before and after

The validating list fetch treats any `401`/`403` as a credentials failure and returns to the prompt, clearing the secret but re-seeding the project key. After validation only a secret-coded failure (`401`, or a `403` naming the agent secret) goes back to the prompt; a plain `403 forbidden` is a permission boundary and stays on the footer. The prompt rebuilds the client with the workspace the session already had, so a re-auth never silently drops the selector.

### The header learns the workspace from a silent `whoami`

`Deps.Workspace` carries the selector from `launchTUI`. After credentials validate, the model issues one `whoamiCmd` and folds its `whoamiLoadedMsg` into the header, resolving the selected id against the reachable list itself — `whoami` ignores the selector. A failure on that silent fetch is dropped and the header simply stays unnamed; the same message with `openPicker` set (the `w` path) opens the picker and does report errors. `w` refetches before opening the picker, and switching builds a new client and clears every loaded task, item, pending fetch, filter and the search index: ids are per workspace, so nothing carries over. See [workspaces](../workspaces/TECHNICAL.md).

### Optimistic toggle; state cycle resyncs on failure

Toggling flips the item and recomputes the task's progress locally before the request; the failure message carries both the previous completion flag and the previous state, because completing an item clears its state and the revert must restore both. The `s` cycle is also optimistic (so repeated presses advance rather than resend a stale next-state) but its failure path re-fetches the task — state and completion interact server-side, so a remembered value is not trustworthy. The PR write is not optimistic; nothing needs to stay in sync between keypresses.

### `replaceItem` is the single "server copy wins" path

Every single-item write response (title, state, PR) is applied through it. It preserves the loaded note body — those responses never carry `notes` — and re-syncs the parent task's progress on the list behind. The note write goes through the generic item PATCH (`UpdateChecklistItem` with `notes`), whose response does echo the note; an echoed `""` is collapsed to nil so the model sees one empty-means-nil contract.

### Stale responses are dropped by id

`taskLoadedMsg` carries the requested id; a response for a task the user has already backed out of is discarded rather than overwriting the open one.

### Search is local; the index shares the open task

The TUI never calls `/tasks/search`: it matches item titles by substring only, skips notes, and returns items as bare `{id, title}`. Instead `/` on the list starts `ensureIndex`, which fetches the full copy of every listed task not yet held, six at a time, into one `indexLoadedMsg`. Drill-ins and task copies also land in the index. While a task is open it is stored as the very pointer the checklist edits, so toggles, renames and note edits are searchable without a refetch; once back on the list a fetched copy wins over the leftover `m.detail`, and a fill never overwrites an entry that arrived after it started. `r`, a re-auth and a workspace switch reset it, cancelling the running fill's requests; a generation counter drops a fill a reset has overtaken. A task that fails to load, or loads without a checklist, is reported once on the footer and not re-requested until the next reset; an auth or workspace error goes through `opErrMsg` like any other call. A list reload under an active filter tops the index up with new tasks.

### Match positions are rune indices that survive wrapping

Titles match as fzf-style subsequences (tightest window, bonus for an exact occurrence); descriptions and notes need every term as a substring, because a subsequence over a long body matches almost anything. Lower-casing is rune by rune so positions index `[]rune(title)` directly, and `renderMarked` recovers each rune's mark across word-wrap by walking the wrapped segment against the original; this relies on wrapping only ever dropping whitespace.

### Filtered rows are the selection space

`listSel` and `itemSel` index the filtered rows, so every action targets what is on screen; keeping or clearing a filter, a task edit and popping back to a screen all re-find the selected id instead of keeping the index. The note screen is pinned to its item by id (`noteItemID`), not through the filtered rows, so an edit that drops the item out of the filter can't slide the screen onto another item. The note screen highlights and scrolls only for the checklist's kept filter; `open` resets that filter, so a list query never leaks into a task.

### A matched note flag is marked with a background

The 🗒 glyph is usually drawn by a colour emoji font that ignores the foreground, so a hit in the note or description behind it paints the background. Magenta is the one palette colour nothing else uses, so hits stay legible on the cyan selected row.

### Hyperlinks wrap styled text, never the reverse

lipgloss renders an OSC 8 escape in its input as literal text, so links are applied after styling and before padding, so only the number is clickable. `ansi.Truncate` keeps the escapes balanced when a line is clipped; an unbalanced one would hyperlink the rest of the screen. A test asserts no rendered screen ever shows `]8;;` or a bare `https://`.

### Columns are reserved and measured by display width

The state column is `icons.Width` cells and the PR column has a floor that only grows for every row at once, so pressing `s` never shifts the list sideways. Padding uses display width, not rune count: the glyphs are four-byte, one-cell runes.

### Footer height drives body height

Hints pack backwards from the last binding into at most two rows, breaking on separators; `bodyHeight(hints)` is the one source of truth for how many body lines fit, shared by the renderers and the scroll clamp so they cannot disagree.

### The description preview borrows from the checklist's height

On the checklist screen the description block is rendered first and the item rows get `bodyHeight` minus its length, so selection scrolling stays correct. The preview is at most three lines (two of text plus the `E to open` hint), capped at `(bodyH-1)/2`, then a blank separator, so the checklist always keeps at least half the body; and whitespace-only descriptions count as none on both the preview and the list-row flag. `E` targets the open task on the checklist and the highlighted row on the list (`descriptionTask`).

### Clipboard Markdown matches the server's export

State and PR ride on the checklist line as a trailing `<!-- state: … pr: … -->` comment, byte-identical to the server's Markdown export so a pasted file round-trips through import. The whole-task copy names the repo once in its header; the single-item copy carries the resolved PR URL, since a bare number would have nothing to resolve against.

### `$EDITOR` runs blocking, GUI editors included

`$VISUAL` wins over `$EDITOR`, falling back to `vi`; known GUI editors get their wait flag injected when absent, because without it the file is read back the instant the launcher process exits. A failed temp-file seed returns an error message rather than opening a truncated buffer.
