# Interactive TUI — Design

## Overview

`sprawl` is normally a one-shot, machine-friendly client. The TUI, shared by both binaries, adds a full-screen, human-driven mode on the same client and credentials: browse tasks, work a checklist, flag state and PR numbers, read and edit notes, do task and item CRUD, search, switch workspace, and — the headline use — copy a task's or item's context as Markdown for an LLM in two keystrokes. It stays inside the project's constraints: one static, cgo-free binary that works over SSH, colored only from the **terminal palette** (never the server-side **app theme**; see [CONTEXT.md](../../CONTEXT.md)).

## Launch rules

| Invocation | Behavior |
|---|---|
| `sprawl` on a TTY | Launches the TUI. |
| `sprawl` off a TTY (pipe, redirect, CI) | Prints help. |
| `sprawl tui` on a TTY | Launches the TUI. |
| `sprawl tui` off a TTY | Errors: `interactive mode requires a terminal`. |
| `sprawl --help` | Help, before the launcher runs. |

## Credentials

- **Token** — resolved as on the CLI. Missing → a **Not logged in** screen with quit only; the TUI never runs the device flow itself.
- **Narrowing factor** — an agent secret or project key from flag / env is used directly and the UI opens on the list. With neither, the first screen is a **credentials prompt** offering both fields (`tab` / ↑↓ switch, `enter` submits whichever is filled; both is legal). An empty submit is refused locally. Neither value is masked: a typo is only fixable if you can see it, and nothing leaves memory.
- **Workspace** — `--workspace` / `SPRAWL_WORKSPACE` selects the canvas; the header names the workspace once the server has been asked (`sprawl · <workspace> · tasks`, plus ` · <project key>` under a key).
- **Validation** — the first authed call is the task list. A `401`/`403` there drops to the prompt with an inline error, even when the bad value came from the environment; the rejected secret is cleared, the project key stays for correction. Mid-session, a secret-auth failure bounces to the prompt; a plain permission `403` is a footer error.

## Screens

Screens form a back stack; `esc` pops one. Overlays (delete confirm, single-line input, pickers, help) render over the current screen.

1. **Credentials prompt** / **Not logged in** — conditional first screens.
2. **Task list** — one row per task: id, progress (traffic-light), due, project, title, with the same 🗒 flag as a noted item when the task has a description. Empty: `(no tasks) — n to create`.
3. **Checklist** — header with the task's title, progress, due and project; a faint preview of the task description, capped so it never crowds out the items (a cut preview ends on `… +N more lines — E to open`); rows `[ ]`/`[x] · id · state icon · PR · title`, with a note flag. The state and PR columns are **always reserved**, so toggling a state never shifts the rows around it.
4. **Note** — the selected item's note, scrollable, under a header that spells the state out (`󱌣 In progress · PR #412`). Empty: `(no notes) — e to add`.

## Flows

- **Copy for an LLM** — `c` on the list copies the whole task (header, description, checklist with notes) as Markdown via OSC 52, fetching it first if needed; `c` on the checklist or note copies one item with its parent-task line. A footer line confirms.
- **Work a checklist** — `space` / `x` toggles the highlighted item **optimistically**: it flips at once, the task's progress updates locally, and a rejection reverts both. Completing an item drops its state icon on the spot (the server clears state on completion); the PR number stays.
- **Flag state** — `s` advances one step through none → ready → progress → review → none. Setting a state un-completes the item, mirrored immediately. A rejected write re-reads the task rather than reverting.
- **Link and open a PR** — `p` prompts for a number (prefilled; empty clears; non-positive refused inline). `o` opens the resolved URL in the browser, or copies it when there is no browser (SSH). PR numbers are also OSC 8 hyperlinks, colored only when the link resolves; the mouse is never captured, so terminal text selection keeps working.
- **Create / edit** — `n` new task (inline title, optional `$EDITOR` description, optional project picker built from projects already in the list); `e` inline title; `E` task description in `$EDITOR`; `t` due-date picker; `d` delete with confirm. On the checklist `n` adds an item, `e` edits its title and `E` edits the open task's description; on the note screen `e` edits the note in `$EDITOR`.
- **Search** — `/` on the list or the checklist opens a query line and filters on every keystroke; `enter` keeps the query as the screen's filter (shown with an `(N of M)` count), `esc` drops it, and either way the cursor stays on the row it was on. Titles match fuzzily (each word's letters in order, gaps allowed); descriptions and notes need each typed word as written. Matched characters are bold magenta, and a 🗒 flag gets a magenta background when the hit is in the note or description behind it.
- **List search reaches into checklists** — the first `/` loads every listed task's items and notes in the background (`loading checklists…`), so a task can match through an item. A task found only that way lists up to three matching items under its row. Title hits rank first, then description hits, then item-only hits ranked by their best item.
- **Checklist search** — filters the items in checklist order, and every item key acts on the filtered rows. A note opened under the filter highlights the matched words and opens scrolled to the first one. Opening a task always starts unfiltered.
- **Switch workspace** — `w` opens a picker of reachable workspaces (role and level shown, current marked); choosing one re-pins the session and refetches the list. Under a project key the footer says the key pins the workspace instead. See [workspaces](../workspaces/INDEX.md).
- **Refresh / errors** — `r` refetches; API errors land on the footer and never crash the UI; `?` shows the keymap.

## Keymap

```
Creds     tab / ↑↓ switch field · enter submit · esc quit
Global    ? help · r refresh · esc clear filter, else back · ctrl+c quit
List      ↑↓ j k · g/G · enter open · / search · c copy · n new · e title · E desc · t due · d delete · w workspace · q quit
Checklist ↑↓ j k · g/G · space/x toggle · enter note · / search · c copy · n add · e title · E desc · d delete · s state · p PR · o open PR
Note      ↑↓ scroll · e edit · c copy · o open PR
Search    type to filter · ↑↓ ctrl+p/n move · enter keep · esc clear
```

There is no "mark whole task done" key: a task's done-ness is derived from its items.

## Decisions

- **Terminal palette only, always on** — bubbletea owns the terminal and downgrades color to its real profile, so no `$NO_COLOR` branch is needed in the model.
- **OSC 52 and `$EDITOR`** — no clipboard binary, no cgo, and both work over SSH.
- **Optimistic toggle, resync-on-failure state** — toggling is the highest-frequency action and must feel instant; state interacts with completion server-side, so after a rejection the server's copy is the only trustworthy one.
- **Hand-rolled single-line input** — a few dozen lines beat a dependency.
- **Project picker only reuses seen projects** — there is no list-projects endpoint, so creation offers what the loaded list already shows, or none.
- **Description is context, the checklist is the screen** — the preview is a few faint lines at most and `E` opens the full text, so a long description never pushes the items off a short window.
- **Search is local** — the TUI filters what it holds instead of calling the server's search, which matches titles by substring and never reads notes. The cost is one full-task fetch per listed task on the first `/`, kept until `r` or a workspace switch.
- **Fuzzy titles, literal bodies** — a letters-in-order match over a long description or note would let almost any short query through, so bodies need whole words.
- **Workspace switch is memory-only** — same rule as the CLI: nothing about scope is persisted by sprawl.
