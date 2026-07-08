# Interactive TUI — Design

## Overview

`sprawl` is normally a one-shot, machine-friendly HTTP client. The interactive TUI adds a full-screen, human-driven mode on top of the same client and credentials. A human can browse their tasks, open a task's checklist, toggle checklist items done/undone, read and edit an item's note, do full task and checklist-item CRUD, search, and — the headline use case — copy a task's or item's context as Markdown to the system clipboard for pasting into an LLM.

The UI adopts whatever theme the user's terminal is running: all colors are ANSI **terminal palette** indices 0-15 (done green 2, in-progress yellow 3, error red 1, accent/header cyan 6, `faint` for secondary), never hard-coded RGB. This is the same choice the CLI's text styling makes (see `internal/cli/style.go`). This is unrelated to the server-side **app theme** (`theme get/set`); see [CONTEXT.md](../../CONTEXT.md).

## Why

- The primary workflow is handing task/checklist context to an LLM. Doing that from the one-shot CLI means chaining `task show` / `checklist` / `note show` and reshaping the output by hand. The TUI makes "select a task, press `c`, paste" a two-keystroke operation, formatting the Markdown for you.
- Toggling checklist items and jotting notes interactively is faster than composing individual `checklist check` / `note set` invocations.
- It stays inside the project's constraints: a single static, cgo-free binary that works over SSH. Clipboard copy is OSC 52 (no external clipboard tool), and `$EDITOR` editing suspends/resumes the program rather than shelling out to a GUI.

## Launch rules

| Invocation | Behavior |
|---|---|
| `sprawl` (no args) on a TTY | Launches the TUI. |
| `sprawl` (no args), stdin/stdout not a TTY (pipe, redirect, CI) | Prints help and exits — today's behavior is preserved. |
| `sprawl tui` on a TTY | Launches the TUI. |
| `sprawl tui`, not a TTY | Errors: `interactive mode requires a terminal`. |
| `sprawl --help` | Cobra short-circuits to help before the launcher runs. |

Both binaries (`sprawl` and `sprawl_dev`) get the TUI from the shared code; there is no `internal/build` divergence.

## Credentials

The TUI reuses the CLI's credential resolvers rather than reimplementing them.

- **Token** (bearer): resolved from `SPRAWL_TOKEN` → `config.toml`. If missing, the TUI shows a **Not logged in** screen ("Run `sprawl login`…") with quit only. The TUI never runs the device-flow login itself.
- **Agent secret**: if `--agent-secret` / `$SPRAWL_AGENT_SECRET` is present, it is used directly. Otherwise the first screen is a **masked** agent-secret prompt. The secret is held in memory only — never written to disk, logged, echoed, or set as a flag default.
- **Validation**: the first authed call is the task-list fetch. On `401`/`403` the model drops to the masked secret prompt with an inline error, **including** when a bad secret came from the environment. Mid-session, a `401` (secret revoked) bounces back to the prompt; a `403` (permission) is shown as a footer error and stays on-screen.

## Screens

Screens form a back stack; `esc` pops one.

1. **Agent-secret prompt** (conditional) — masked input; Enter validates via the list fetch.
2. **Not logged in** — message + quit (token missing).
3. **Task list** (home) — one row per task: `#id · progress (done/total, traffic-light color) · due · project · title`. Cursor is `›` + bold + cyan. Empty state: `(no tasks) — n to create`.
4. **Checklist** (task detail) — header (`#id title · progress · due · project`); item rows `[ ]`/`[x] · #id · title` with a `🗒` flag when the item has a note. Empty state: `(no checklist items) — a to add`.
5. **Note view** — the selected checklist item's note body, scrollable. Empty state: `(no notes) — e to add`.

### Overlays

- **Delete confirm** (`y`/`n`) — for task and checklist-item deletion.
- **Inline input** — single-line editor for new/edit titles and search.
- **Picker** — due-date picker (yesterday / today / week / none) and, on task create, an optional project picker built from the distinct projects already present in the loaded list (there is no ListProjects endpoint, so projects can only be reused, not discovered).
- **Help** (`?`) — keymap cheat sheet; any key closes it.

## User flows

- **Copy a task for an LLM**: on the task list, move to the task, press `c`. If the full task isn't loaded yet it is fetched first, then the whole task (header, description, and checklist with per-item notes) is copied as Markdown via OSC 52. A transient footer confirms `✓ copied task #123`.
- **Copy a single item**: on the checklist or note screen, press `c` to copy just that checklist item (its checkbox state, parent task reference, and note body) as Markdown.
- **Work a checklist**: `enter` on a task opens its checklist; `space` or `x` toggles the highlighted item. The toggle is **optimistic** — it flips instantly and the parent task's progress is updated locally; if the API rejects it, the item reverts and the error shows on the footer.
- **Create / edit**: `n` starts a new task (inline title, optional `$EDITOR` description, optional project picker); `e` edits the highlighted title inline; `E` edits a task's description in `$EDITOR`; `t` sets a due date; `a` adds a checklist item; `d` deletes (with a confirm overlay).
- **Edit a note**: on the note screen, `e` opens the item's note in `$EDITOR`; on save it is written back with `note set` semantics.
- **Search**: `/` enters live client-side filtering (case-insensitive title substring) as you type. `enter` runs a server-side search that also surfaces checklist-item-title matches (annotated with the matched item names under the row). `esc` clears the search.
- **Refresh / errors**: `r` refetches the current screen. API errors always land on the footer status line and never crash the UI; a spinner/`loading…` shows during in-flight calls.

## Keymap

```
Global    ?=help overlay   r=refresh   esc=back one screen   ctrl+c=quit
List      ↑↓ / j k=move    g/G=top/bottom   enter=open task   /=search   q=quit
          c=copy task(md)  n=new task   e=edit title   E=edit description($EDITOR)
          t=set due        d=delete task(confirm)
Checklist ↑↓ / j k=move    g/G=top/bottom   space or x=toggle done
          enter=open note  esc=back
          c=copy item(md)  a=add item   e=edit item title   d=delete item(confirm)
Note      e=edit note($EDITOR)   c=copy item(md)   ↑↓=scroll   esc=back
```

`space` and `x` both toggle on the checklist. `g`/`G` jump to top/bottom of any list. There is no "mark whole task done" key — a task's done-ness is derived server-side from its checklist items (see [CONTEXT.md](../../CONTEXT.md)).

## Design decisions

- **Terminal palette only, always on.** Unlike the CLI's `stylesEnabled` gate, TUI styling is unconditional: bubbletea owns the terminal for the session and downgrades color to the terminal's real profile at write time (stripping it on a dumb terminal). No `$NO_COLOR` branch is needed in the model.
- **OSC 52 for clipboard, `$EDITOR` for multi-line.** Both avoid external binaries and cgo, preserving the single-static-binary promise and working over SSH.
- **Optimistic toggles.** Checklist toggling is the highest-frequency action; flipping instantly and reverting on error keeps it snappy while staying correct.
- **Hand-rolled single-line input.** A one-line editor is small; the `bubbles` textinput dependency was avoided to keep the dependency surface minimal.
- **Project create is best-effort.** With no ListProjects endpoint, task creation can only offer projects already seen in the loaded list, or skip project assignment entirely — it never invents an endpoint.
