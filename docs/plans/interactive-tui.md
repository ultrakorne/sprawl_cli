# Interactive TUI — Implementation Spec

Status: ready to build. Derived from a design interview (2026-07-08). This is the
build contract for the dynamic implementation workflow.

## Goal

Add an interactive terminal UI to sprawl, launched when the binary runs with no
args on a TTY (and via an explicit `sprawl tui` command). Built on bubbletea
v2.0.8. Colors come **exclusively from the terminal's ANSI palette** (indices
0–15) so the UI adopts the user's terminal theme — same philosophy as
`internal/cli/style.go`. A human can browse tasks, drill into a task's
checklist, toggle item completion, view/edit notes, do full task/item CRUD,
search, and copy task/item context as **Markdown** to the system clipboard for
pasting into an LLM.

## Dependencies

- Add `charm.land/bubbletea/v2 v2.0.8` (canonical module path is
  `charm.land/bubbletea/v2`; matches the existing `charm.land/lipgloss/v2`).
- Clipboard: `tea.SetClipboard(string) Cmd` (OSC 52). No external clipboard
  tool, no cgo — preserves the single static-binary promise; works over SSH.
- `$EDITOR`: `tea.ExecProcess(exec.Command(...), cb)` to suspend/resume the TUI.
- Do **not** add `bubbles` unless a hand-rolled single-line inline input proves
  too fiddly; if truly needed, add only `charm.land/bubbles/v2` textinput.
- Update AGENTS.md's stale note claiming bubbletea was removed.

## Bubble Tea v2 API ground truth (from v2.0.8 source)

Source is in the module cache: `$(go env GOMODCACHE)/charm.land/bubbletea/v2@v2.0.8/*.go`.
Consult it for exact signatures. Key differences from v1:

- `func (m Model) View() tea.View` (NOT string). Build with
  `v := tea.NewView(s); v.AltScreen = true; return v`. AltScreen/mouse/etc. are
  **View fields**, not program options or commands.
- Key events arrive as `tea.KeyPressMsg`; switch on `msg.String()`.
- Resize: `tea.WindowSizeMsg{Width, Height}`.
- `tea.SetClipboard(s)` / `tea.SetPrimaryClipboard(s)` (both OSC 52).
- `tea.ExecProcess(cmd, func(err error) tea.Msg { ... })`.
- `p := tea.NewProgram(model, opts...); m, err := p.Run()`.
- lipgloss v2 is already vendored (`charm.land/lipgloss/v2`); reuse it.

## Package layout

- New package `internal/tui/` (keeps cobra concerns in `internal/cli`; the TUI is
  a distinct concern with its own model/screens).
- `internal/cli/root.go`: give root a `RunE` that launches the TUI when
  `len(args) == 0` AND stdin+stdout are a TTY; otherwise print help (today's
  behavior). Add an explicit `sprawl tui` subcommand → same launcher.
  `--help` still shows help (cobra short-circuits before RunE — verified).
- `tui.Run(ctx, deps)` receives resolved dependencies: a client factory, the
  resolved token, and the resolved-or-prompted agent secret. Reuse
  `internal/client` and the credential resolvers in `internal/cli/auth.go`
  (export a thin helper if needed rather than duplicating).

## Credentials

- **Token**: resolve via existing logic (`SPRAWL_TOKEN` → `config.toml`). Missing
  → render a "Not logged in — run `sprawl login`" screen with `[q]` quit. Do NOT
  build device-flow login into the TUI.
- **Agent secret**: if `--agent-secret` / `SPRAWL_AGENT_SECRET` present, use it.
  Else show a **masked** prompt screen first. Held in memory only — NEVER written
  to disk / log / flag default (AGENTS.md invariants #2, #3). Never echoed.
- **Validation**: the first authed call is the task-list fetch. On 401/403 (auth
  failure) drop to the masked secret prompt with an inline error ("secret
  rejected — try again"), **including** when a wrong secret came from env. On
  success, proceed to the list.

## Screens (a back stack; `esc` pops one)

1. **Secret prompt** (conditional): masked input; Enter → validate via list fetch.
2. **Not-logged-in**: message + quit (token missing).
3. **Task list** (home): task rows — ID · PROGRESS (done/total, traffic-light
   color) · PROJECT · TITLE (mirror the CLI list; DUE optional). Cursor = `›` +
   bold + cyan(6). Empty → "(no tasks) — n to create".
4. **Checklist** (task detail): header (#id title · due · project · progress);
   items `[ ]`/`[x]` (green when done) · #id · title; notes flag. Same cursor.
   Empty → "(no checklist items) — a to add".
5. **Note view**: selected item's note body (scroll if long); "(no notes) — e to
   add" when empty.
6. **Overlays**: delete confirm (y/n); inline single-line input (title / search);
   due-date picker (yesterday / today / week / none); help overlay (`?`).

## Data fetching (all as non-blocking `tea.Cmd`s)

- List: `client.ListTasks(ctx)`.
- Enter task → `client.GetTask(ctx, id, full=true)` (items + notes in one call);
  cache on the checklist screen.
- Toggle → `client.SetChecklistItemCompleted(ctx, itemID, completed)`.
  **Optimistic**: flip instantly; on API error revert + footer error. Update the
  parent task's progress locally so the list stays in sync when popped.
- Task CRUD: `CreateTask` / `UpdateTask` (title, description) / `SetTaskDueDate`
  (yesterday|today|week|none) / `DeleteTask` (soft).
- Item CRUD: `CreateChecklistItem` / `UpdateChecklistItem` (title) / `SetNotes`
  (note body) / `DeleteChecklistItem`.
- **Search**: live client-side filter (case-insensitive title substring) as you
  type; Enter → `client.SearchTasks(ctx, q)` to also surface checklist-item-title
  matches (with the matched-item context the server returns); esc clears.
- Show a spinner / "loading…" during in-flight calls. API errors → footer status
  line, never crash the TUI. `r` → full refresh of the current screen.

## Copy (Markdown, via OSC 52)

Pure, unit-testable formatters, then `tea.SetClipboard(md)`.

- **Task list (task focused) → whole task.** If the full task isn't already
  loaded, fetch `GetTask(full)` first, then copy:

  ```
  # Task #<id> — <title>
  status: <status>  due: <due|—>  project: <name|—>  progress: <done>/<total>

  <description, if present>

  ## Checklist
  - [x] #<itemid> <title>
        <note lines, indented> (if any)
  - [ ] #<itemid> <title>
  ```

- **Checklist / note (item focused) → single item.**

  ```
  ## Item #<id> — <title>  ([x]|[ ])
  task: #<taskid> <tasktitle>
  note:
  <note body, or "(none)">
  ```

- After copy → transient footer "✓ copied task #123" (~2s), then clears.

## Colors / styling

Terminal ANSI palette only (0–15), same choices as `internal/cli/style.go`:
done green(2), in-progress yellow(3), error/danger red(1), accent/header
cyan(6), `faint` for secondary. Selection = `›` + bold + `Foreground(6)`. No
hard-coded RGB. TUI styling is always-on (it owns the terminal) — build
TUI-local lipgloss styles rather than reusing the CLI's gated `stylesEnabled`
global.

## Keymap (final)

```
Global    ?=help overlay   r=refresh   esc=back one screen   ctrl+c=quit
List      ↑↓ / j k=move    g/G=top/bottom   enter=open task   /=search   q=quit
          c=copy task(md)  n=new task   e=edit title   E=edit description($EDITOR)
          t=set due        d=delete task(confirm)
Checklist ↑↓ / j k=move    g/G=top/bottom   space or x=toggle done
          enter=open note  esc=back
          c=copy item(md)  a=add item   e=edit item title   d=delete item(confirm)
Note      e=edit note($EDITOR)   c=copy item(md)   esc=back
```

space AND x both toggle on the checklist. g/G jump to top/bottom of any list.

## Non-TTY / edge cases

- Bare `sprawl` when stdout/stdin isn't a TTY (piped/redirected) → print help +
  exit (today's behavior). `sprawl tui` when not a TTY → error "interactive mode
  requires a terminal".
- Handle `tea.WindowSizeMsg` for responsive layout; wrap/scroll long content;
  horizontal overflow must never break the layout.
- Both binaries (`sprawl` + `sprawl_dev`) get it — shared code, no `internal/build`
  divergence.

## Project selection on create (known gap)

There is **no** ListProjects endpoint. On `n` (new task): inline title, optional
`$EDITOR` description; project assignment is optional — offer a picker built from
the distinct projects already present in the loaded task list, or skip it when
none are known. Do not invent an endpoint.

## Testing

- Pure functions unit-tested: markdown formatters, keymap dispatch, secret-prompt
  state machine, search filter, optimistic-toggle + revert, back-stack.
- Model-level `Update` tests feeding synthetic msgs (`KeyPressMsg`, API-result
  msgs, error msgs) asserting state transitions. Use bubbletea's teatest for a
  golden smoke test if practical; otherwise drive the Model directly.
- Mock the client with `httptest` (existing pattern) or inject a client
  interface.
- `make check` (fmt-check + vet + test) MUST pass. Do not bypass hooks.
  **Do NOT commit** (AGENTS.md + user rule).

## Docs (final phase)

- New `docs/features/interactive-tui/{INDEX,DESIGN,TECHNICAL}.md` following the
  project-documentation conventions.
- New `docs/CONTEXT.md` glossary capturing the terms resolved in the interview:
  - **terminal palette** — ANSI indices 0–15, follows the user's terminal theme;
    what the CLI/TUI colors use.
  - **app theme** — the server-side sprawl web-app theme (`theme get/set`,
    e.g. `tokyo-night`); unrelated to the terminal palette. (These two were
    conflated as "theme".)
  - **task** vs **checklist item** — the item is the unit the checkbox toggles;
    a task's done-ness is derived server-side. No "mark whole task done" API.
  - **note** — free-form text attached to a checklist item (not the task).
- Update `docs/INDEX.md` features table; refresh the AGENTS.md "Interactive
  prompts" note (bubbletea is back, for the TUI).
