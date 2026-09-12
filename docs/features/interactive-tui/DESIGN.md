# Interactive TUI — Design

## Overview

`sprawl` is normally a one-shot, machine-friendly client. The TUI adds a full-screen, human-driven mode on the same client and credentials: browse tasks, work a checklist, flag state and PR numbers, read and edit notes, do task and item CRUD, search, switch workspace, and — the headline use — copy a task's or item's context as Markdown for an LLM in two keystrokes. It stays inside the project's constraints: one static, cgo-free binary that works over SSH, colored only from the **terminal palette** (never the server-side **app theme**; see [CONTEXT.md](../../CONTEXT.md)).

## Launch rules

| Invocation | Behavior |
|---|---|
| `sprawl` on a TTY | Launches the TUI. |
| `sprawl` off a TTY (pipe, redirect, CI) | Prints help. |
| `sprawl tui` on a TTY | Launches the TUI. |
| `sprawl tui` off a TTY | Errors: `interactive mode requires a terminal`. |
| `sprawl --help` | Help, before the launcher runs. |

Both binaries get the TUI from the shared code.

## Credentials

- **Token** — resolved as on the CLI. Missing → a **Not logged in** screen with quit only; the TUI never runs the device flow itself.
- **Narrowing factor** — an agent secret or project key from flag / env is used directly and the UI opens on the list. With neither, the first screen is a **credentials prompt** offering both fields (`tab` / ↑↓ switch, `enter` submits whichever is filled; both is legal). An empty submit is refused locally. Neither value is masked: a typo is only fixable if you can see it, and nothing leaves memory.
- **Workspace** — `--workspace` / `SPRAWL_WORKSPACE` selects the canvas; the header names the workspace once the server has been asked (`sprawl · <workspace> · tasks`, plus ` · <project key>` under a key).
- **Validation** — the first authed call is the task list. A `401`/`403` there drops to the prompt with an inline error, even when the bad value came from the environment; the rejected secret is cleared, the project key stays for correction. Mid-session, a secret-auth failure bounces to the prompt; a plain permission `403` is a footer error.

## Screens

Screens form a back stack; `esc` pops one. Overlays (delete confirm, single-line input, pickers, help) render over the current screen.

1. **Credentials prompt** / **Not logged in** — conditional first screens.
2. **Task list** — one row per task: id, progress (traffic-light), due, project, title. Empty: `(no tasks) — n to create`.
3. **Checklist** — header with the task's title, progress, due and project; rows `[ ]`/`[x] · id · state icon · PR · title`, with a note flag. The state and PR columns are **always reserved**, so toggling a state never shifts the rows around it.
4. **Note** — the selected item's note, scrollable, under a header that spells the state out (`󱌣 In progress · PR #412`). Empty: `(no notes) — e to add`.

## Flows

- **Copy for an LLM** — `c` on the list copies the whole task (header, description, checklist with notes) as Markdown via OSC 52, fetching it first if needed; `c` on the checklist or note copies one item with its parent-task line. A footer line confirms.
- **Work a checklist** — `space` / `x` toggles the highlighted item **optimistically**: it flips at once, the task's progress updates locally, and a rejection reverts both. Completing an item drops its state icon on the spot (the server clears state on completion); the PR number stays.
- **Flag state** — `s` advances one step through none → ready → progress → review → none. Setting a state un-completes the item, mirrored immediately. A rejected write re-reads the task rather than reverting.
- **Link and open a PR** — `p` prompts for a number (prefilled; empty clears; non-positive refused inline). `o` opens the resolved URL in the browser, or copies it when there is no browser (SSH). PR numbers are also OSC 8 hyperlinks, colored only when the link resolves; the mouse is never captured, so terminal text selection keeps working.
- **Create / edit** — `n` new task (inline title, optional `$EDITOR` description, optional project picker built from projects already in the list); `e` inline title; `E` task description in `$EDITOR`; `t` due-date picker; `d` delete with confirm. On the checklist `n` adds an item and `e` edits its title; on the note screen `e` edits the note in `$EDITOR`.
- **Search** — `/` filters the list live by title; `enter` runs a server search that also matches item titles (shown under the row); `esc` clears.
- **Switch workspace** — `w` opens a picker of reachable workspaces (role and level shown, current marked); choosing one re-pins the session and refetches the list. Under a project key the footer says the key pins the workspace instead. See [workspaces](../workspaces/INDEX.md).
- **Refresh / errors** — `r` refetches; API errors land on the footer and never crash the UI; `?` shows the keymap.

## Keymap

```
Creds     tab / ↑↓ switch field · enter submit · esc quit
Global    ? help · r refresh · esc back · ctrl+c quit
List      ↑↓ j k · g/G · enter open · / search · c copy · n new · e title · E desc · t due · d delete · w workspace · q quit
Checklist ↑↓ j k · g/G · space/x toggle · enter note · c copy · n add · e title · d delete · s state · p PR · o open PR
Note      ↑↓ scroll · e edit · c copy · o open PR
```

There is no "mark whole task done" key: a task's done-ness is derived from its items.

## Decisions

- **Terminal palette only, always on** — bubbletea owns the terminal and downgrades color to its real profile, so no `$NO_COLOR` branch is needed in the model.
- **OSC 52 and `$EDITOR`** — no clipboard binary, no cgo, and both work over SSH.
- **Optimistic toggle, resync-on-failure state** — toggling is the highest-frequency action and must feel instant; state interacts with completion server-side, so after a rejection the server's copy is the only trustworthy one.
- **Hand-rolled single-line input** — a few dozen lines beat a dependency.
- **Project picker only reuses seen projects** — there is no list-projects endpoint, so creation offers what the loaded list already shows, or none.
- **Workspace switch is memory-only** — same rule as the CLI: nothing about scope is persisted by sprawl.
