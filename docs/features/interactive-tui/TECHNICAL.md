# Interactive TUI — Technical

## Architecture

The TUI is its own package, `internal/tui`, so cobra stays in `internal/cli`. `launchTUI` resolves the token, the narrowing factors and the workspace selector with the CLI's own resolvers and hands them to the TUI as `Deps`, including a `NewClient(secret, projectKey, workspace)` closure that captures only the bearer — the factors and the selector are supplied per call so the credentials prompt and the workspace picker can rebuild the client without anything leaking into a shared field.

The model is Elm-style bubbletea v2: `Init` returns the validating list fetch (or nothing when starting on the credentials / not-logged-in screen); `Update` folds key presses, window size and the package's typed result messages; `View` returns a `tea.View` with `AltScreen` set (a view field in v2, not a program option). All network access is a `tea.Cmd` built in `msgs.go` against the `Client` interface — the subset of `*client.Client` the model uses, so tests inject a fake or an `httptest`-backed real client. Screens are a back stack (`creds`, `not logged in`, `list`, `checklist`, `note`); overlays (confirm, input, picker, help) are a separate field rendered over the current screen.

## Where things live

| File | Role |
|------|------|
| `internal/tui/tui.go` | `Deps`, `IsTTY`, `Run` (a ctrl+c / kill exit is clean). |
| `internal/tui/model.go` | `Model`, `newModel`, the back stack, selection helpers, footer status, `replaceItem`, `nextState`, workspace state and picker items. |
| `internal/tui/update.go` | Key dispatch, overlay handling, message reducers, the `s` / `p` / `o` / `w` handlers. |
| `internal/tui/view.go` | Per-screen renderers, `frame`, hint packing, the fixed state / PR columns, OSC 8 link helpers. |
| `internal/tui/msgs.go` | Message types, error classification, and every command that calls the client. |
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

`Deps.Workspace` carries the selector from `launchTUI`. After credentials validate, the model issues one `whoamiCmd` and folds its `whoamiLoadedMsg` into the header, resolving the selected id against the reachable list itself — `whoami` ignores the selector. A failure on that silent fetch is dropped and the header simply stays unnamed; the same message with `openPicker` set (the `w` path) opens the picker and does report errors. `w` refetches before opening the picker, and switching builds a new client and clears every loaded task, item, pending fetch and search: ids are per workspace, so nothing carries over. See [workspaces](../workspaces/TECHNICAL.md).

### Optimistic toggle; state cycle resyncs on failure

Toggling flips the item and recomputes the task's progress locally before the request; the failure message carries both the previous completion flag and the previous state, because completing an item clears its state and the revert must restore both. The `s` cycle is also optimistic (so repeated presses advance rather than resend a stale next-state) but its failure path re-fetches the task — state and completion interact server-side, so a remembered value is not trustworthy. The PR write is not optimistic; nothing needs to stay in sync between keypresses.

### `replaceItem` is the single "server copy wins" path

Every single-item write response (title, state, PR) is applied through it. It preserves the loaded note body — those responses never carry `notes` — and re-syncs the parent task's progress on the list behind. The note write goes through the generic item PATCH (`UpdateChecklistItem` with `notes`), whose response does echo the note; an echoed `""` is collapsed to nil so the model sees one empty-means-nil contract.

### Stale responses are dropped by id

`taskLoadedMsg` carries the requested id; a response for a task the user has already backed out of is discarded rather than overwriting the open one.

### Hyperlinks wrap styled text, never the reverse

lipgloss renders an OSC 8 escape in its input as literal text, so links are applied after styling and before padding, so only the number is clickable. `ansi.Truncate` keeps the escapes balanced when a line is clipped; an unbalanced one would hyperlink the rest of the screen. A test asserts no rendered screen ever shows `]8;;` or a bare `https://`.

### Columns are reserved and measured by display width

The state column is `icons.Width` cells and the PR column has a floor that only grows for every row at once, so pressing `s` never shifts the list sideways. Padding uses display width, not rune count: the glyphs are four-byte, one-cell runes.

### Footer height drives body height

Hints pack backwards from the last binding into at most two rows, breaking on separators; `bodyHeight(hints)` is the one source of truth for how many body lines fit, shared by the renderers and the scroll clamp so they cannot disagree.

### Clipboard Markdown matches the server's export

State and PR ride on the checklist line as a trailing `<!-- state: … pr: … -->` comment, byte-identical to the server's Markdown export so a pasted file round-trips through import. The whole-task copy names the repo once in its header; the single-item copy carries the resolved PR URL, since a bare number would have nothing to resolve against.

### `$EDITOR` runs blocking, GUI editors included

`$VISUAL` wins over `$EDITOR`, falling back to `vi`; known GUI editors get their wait flag injected when absent, because without it the file is read back the instant the launcher process exits. A failed temp-file seed returns an error message rather than opening a truncated buffer.
