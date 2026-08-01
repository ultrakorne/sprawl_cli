# Interactive TUI — Technical

## Package layout

The TUI lives in its own package, `internal/tui/`, keeping cobra concerns in `internal/cli`. Cobra wires the launcher; the TUI owns the model and screens.

| File | Role |
|------|------|
| `internal/tui/tui.go` | Package doc, `Deps`, `IsTTY()`, `Run(ctx, deps)` — builds the model and runs the bubbletea program. |
| `internal/tui/model.go` | `Model` struct, `newModel`, back-stack helpers, selection helpers, footer status, data reconciliation, pure search filter. |
| `internal/tui/update.go` | `Update` — key dispatch, overlay handling, message handling, side-effect commands. |
| `internal/tui/view.go` | `View` (returns `tea.View`), the per-screen and overlay renderers, framing/clipping/scrolling helpers. |
| `internal/tui/msgs.go` | Message types, error classification (`isAuthErr`/`isUnauthorized`/`isNotFound`), and the `tea.Cmd` command builders that call the client. |
| `internal/tui/client.go` | `Client` interface — the subset of `*client.Client` the model depends on (for test injection). |
| `internal/tui/style.go` | `styles` (terminal-palette lipgloss styles), traffic-light `progress`, `checkbox`. |
| `internal/tui/input.go` | `textInput` — hand-rolled single-line editor (values always echoed in the clear). |
| `internal/tui/keymap.go` | `action` enum + pure `dispatch(screen, key)` key→action mapping. |
| `internal/tui/markdown.go` | Pure Markdown formatters `taskMarkdown` / `itemMarkdown` for clipboard payloads. |
| `internal/tui/editor.go` | `editInEditorCmd` — `$EDITOR` suspend/resume via `tea.ExecProcess`. |
| `internal/cli/root.go` | Root `RunE`: bare `sprawl` on a TTY → `launchTUI`, else help. Registers `newTUICmd`. |
| `internal/cli/interactive.go` | `newTUICmd` (`sprawl tui`) and `launchTUI` — resolves credentials and starts the TUI. |
| `internal/cli/auth.go` | `resolveToken` / `resolveAgentSecret` — reused by `launchTUI`. |

## Dependency

- `charm.land/bubbletea/v2` (v2.0.8) — added for the TUI. `charm.land/lipgloss/v2` was already vendored and is reused. `github.com/charmbracelet/x/term` (TTY detection) and `github.com/charmbracelet/x/ansi` (ANSI-aware truncate/wrap) are used for framing.
- No `bubbles`, no external clipboard tool, no cgo — the single-static-binary, works-over-SSH promise is preserved.

## Bubbletea v2 model

The TUI is an Elm-architecture `*Model`:

- **`Init() tea.Cmd`** returns `m.initCmd`, set by `newModel` from the resolved deps (either the validating list fetch, or nothing when starting on the secret / not-logged-in screen).
- **`Update(msg) (tea.Model, tea.Cmd)`** in `update.go` handles `tea.KeyPressMsg` (switched on `msg.String()`), `tea.WindowSizeMsg` (responsive layout), and the package's own result/error messages.
- **`View() tea.View`** in `view.go` builds a string and wraps it: `v := tea.NewView(s); v.AltScreen = true; return v`. AltScreen is a **View field** in v2, not a program option.

`Run` treats `tea.ErrInterrupted` / `tea.ErrProgramKilled` (ctrl+c / kill) as a clean exit and returns nil.

### Back stack

`Model.stack []screen` with `push` / `pop` / `popTo`. `esc` pops one screen (ignored at the root). Screens: `screenCreds`, `screenNotLoggedIn`, `screenList`, `screenChecklist`, `screenNote`. Overlays (`ovConfirm`, `ovInput`, `ovPicker`, `ovHelp`) are a separate `Model.overlay` field rendered instead of the base screen, not stack entries.

## Screens and overlays

- **Credentials prompt** (`viewCreds` / `credRow`) — two unmasked `textInput`s (agent secret, project key) with `credFocus` selecting the active one; `tab`/↑↓ switch, Enter sets `credBusy` and fires `validateAndListCmd`.
- **Not logged in** (`viewNotLoggedIn`) — static; quit only.
- **List** (`viewList` / `listRows`) — column-aligned task rows with a centered scroll window (`windowStart`); traffic-light progress; search title/annotations.
- **Checklist** (`viewChecklist` / `checklistRows`) — header from the cached full task; item rows with checkbox, id, title, `🗒` note flag.
- **Note** (`viewNote` / `wrapLines`) — soft-wrapped, scrollable note body with a `noteOff` line offset.
- **Overlays** (`viewOverlay`) — confirm, single-line input, picker, help.

`frame(title, body, hints)` assembles every screen to exactly the window height: header + rule, a body region clipped to available height, a transient status line, and a key-hint line. `clip` uses `ansi.Truncate` so styled lines never overflow horizontally.

## Data flow

All network access is non-blocking `tea.Cmd`s built in `msgs.go`; each returns a typed result or error message that `Update` folds into the model.

| Trigger | Command | Result message |
|---|---|---|
| Startup / validate | `validateAndListCmd` (`ListTasks`) | `tasksLoadedMsg{validated:true}` or `authFailedMsg` |
| Refresh list | `listTasksCmd` | `tasksLoadedMsg` |
| Open task / copy task | `getTaskCmd` (`GetTask` full=true) | `taskLoadedMsg{forCopy}` |
| Toggle item | `toggleItemCmd` (`SetChecklistItemCompleted`) | `itemToggledMsg` / `toggleFailedMsg` |
| Task CRUD | `createTaskCmd` / `updateTaskCmd` / `setDueCmd` / `deleteTaskCmd` | `taskMutatedMsg` / `taskDeletedMsg` |
| Item CRUD | `addItemCmd` / `updateItemCmd` / `setNotesCmd` / `deleteItemCmd` | `itemMutatedMsg` / `notesSetMsg` / `itemDeletedMsg` |
| Search (server) | `searchTasksCmd` | `searchResultMsg` |

Key details:

- **Optimistic toggle**: the model flips the item and updates the parent task's derived progress locally (`recomputeProgress`, `syncListProgress`) before the request. `toggleFailedMsg` reverts and sets a footer error.
- **Full-task caching**: opening a task caches the full task (items + notes) on the checklist screen (`Model.detail`); copy reuses the cache when available.
- **Search**: live client-side filtering (`filterTasksByTitle`, pure) while typing; `enter` runs `SearchTasks` server-side and surfaces `MatchedChecklistItems`.
- **Idempotent delete**: `isNotFound` treats a `404 not_found` on delete as a successful no-op, mirroring the CLI.
- **Footer status**: `setTransient` shows a message that auto-clears after ~2.5s via a token-guarded `clearStatusAfter` tick (a stale timer can't wipe a newer status); `setError` shows a persistent error until the next status.

## Credential handling

`launchTUI` (in `internal/cli/interactive.go`) resolves credentials with the existing CLI resolvers and passes them to the TUI via `tui.Deps`:

- A missing token is **not** fatal: `Deps.LoggedIn=false` → the model starts on `screenNotLoggedIn`.
- A missing agent secret is **not** fatal: the model starts on `screenCreds`, which asks for a secret **or** a project key — unless either factor is already resolved (`Deps.Secret` / `Deps.ProjectKey`), in which case it starts on `screenList` and validates immediately. An empty submit is refused inline (`credErr`) instead of being sent.
- `Deps.NewClient(secret, projectKey string) Client` is a closure that captures **only** the bearer token (`client.NewAuthed(token, secret, client.WithProjectKey(key))`); both narrowing factors are supplied per call, so the prompt can retry with either one without them leaking into a shared field.
- Validation happens on the first `ListTasks`. `validationErrMsg` routes a `401`/`403` to `authFailedMsg` (→ credentials prompt); post-validation, `opErrMsg` routes only a secret-auth failure (`401`, or a secret-coded `403`) back to the prompt and keeps other errors (incl. a plain `403 forbidden`) on the footer. `authFailedMsg` clears `m.secret` but re-seeds `keyInput` from `m.projectKey` and calls `focusCreds()`, which focuses the key field when that was the only factor in play.
- `Model.projectKey` is set from `Deps` or from the prompt and feeds the client built by `NewClient`; it is also rendered in the list header title.
- The secret lives only in `Model.secret` / `secretInput` — never persisted or logged. It *is* echoed on the prompt (deliberately: an invisible value can't be proofread), which does not weaken AGENTS.md invariants #2/#3 — those are about not writing it to disk or into flag defaults.

## OSC 52 copy

Copy actions build a Markdown string with the pure formatters in `markdown.go`, then hand it to bubbletea's `tea.SetClipboard(md)` (an OSC 52 escape — no external clipboard binary, works over SSH):

- **Whole task** (`taskMarkdown`): `# Sprawl Task #<id> — <title>`, a status/due/project/progress line, the description (if any), and a `## Checklist` section with `- [x] #<id> <title>` rows and indented note lines. Emitted on `c` from the list; if the full task isn't loaded it is fetched first (`getTaskCmd(forCopy)`).
- **Single item** (`itemMarkdown`): a `sprawl task: #<id> <title>` parent-context line, then the item as a `- [ ]`/`- [x] #<id> <title>` task-list line with its note indented underneath. The item line + note are rendered by the shared `writeItemMarkdown` helper, so single-item and whole-task copies stay identical. Emitted on `c` from the checklist / note screens.

Both are followed by a transient footer confirmation (`✓ copied task #123` / `✓ copied item #45`), batched with the clipboard command.

## `$EDITOR` flow

Multi-line fields (a task **description**, a checklist item's **note**) are edited via `editInEditorCmd` (`editor.go`):

1. Seed a temp file (`sprawl-*.md`) with the current value; a failed seed/close returns an `editorDoneMsg` carrying the error rather than opening a truncated buffer.
2. Resolve `$EDITOR` (falling back to `vi`), split on spaces so an editor with flags (e.g. `code --wait`) works, and run it via `tea.ExecProcess` — bubbletea suspends the program, hands the terminal to the child, and restores on return.
3. On exit, read the buffer back into `editorDoneMsg{kind,id,body}` and remove the temp file. `Update` dispatches by `editKind` (`editTaskDesc` → `updateTaskCmd`, `editItemNote` → `setNotesCmd`).

## Testing

Pure functions and the model are unit-tested without a running program:

- `markdown_test.go`, `input_test.go`, `keymap_test.go` cover the formatters, the single-line input, and key→action dispatch.
- `model_test.go` drives `Update` with synthetic messages (`KeyPressMsg`, result/error messages) and asserts state transitions — secret-prompt state machine, optimistic toggle + revert, back-stack, search filter.
- `httptest_test.go` wires a real `*client.Client` to an `httptest` server (the same pattern as `internal/client`), exercising the `Client` interface end to end. No running backend is required.

`make check` (fmt-check + vet + test) must pass; do not commit.
