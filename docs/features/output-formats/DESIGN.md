# Output Formats — Design

## Overview

Every `/api/v1/*`-wrapping subcommand honours a uniform `--format=text|json` persistent flag. `json` is the default — the server's envelope with a small, documented item normalization (see TECHNICAL.md), and what agents and scripts consume; `text` is a human-readable view (aligned tables for lists, multi-line details for single records). `-h` / `--human` is a shorthand for `--format=text` (mnemonic: **h**uman). `SPRAWL_OUTPUT` sets a session default without repeating the flag. Login is interactive and stays plain text regardless — agents can't approve in a browser anyway.

## Human (text) styling

`text` output is the only format that gets dressed up. It is color-styled with [lipgloss v2](https://charm.land/lipgloss) (`charm.land/lipgloss/v2`):

- **Tables** are preceded by a blank line (breathing room from the prompt) and led by a header row + a `─` rule beneath it — both in **cyan** (ANSI 6), a distinct accent that doesn't collide with the status traffic-light. The rule separates header from rows without any column dividers or grid. Applies to every table view (`task list` / `search`, `activity`'s two tables, and the shared item table behind `task <id>` / `item <id>` / `queue`).
- **Checklist progress** is traffic-light colored (`0/x` red, in-progress yellow, `x/x` green; `0/0` plain), and **checkboxes** green / faint.
- **Detail / section** views use bold titles & section labels and faint keys / placeholders.

Task status isn't rendered in text at all — wherever progress is shown it conveys the same signal — but it stays in the json payload for agents.

Layout vs. color: the blank line and the `─` rule are layout, so they're present in plain (non-TTY) output too; only the colors are stripped off a terminal.

Two rules keep this from ever being a problem:

- **Text only.** `json` is a machine format and is never styled — the styling lives behind `resolveFormat == text`, so no escape sequence can ever reach a structured payload. Agents read that format; styling exists purely for the human owner.
- **Terminal colors, terminal-gated.** Colors are the terminal's own ANSI palette indices (0–15), not fixed RGB — so output adopts whatever theme the terminal / Omarchy is running rather than clashing with it. Styling is emitted only when stdout is a real TTY; piping, redirecting to a file, or `$NO_COLOR` collapses it to plain text identical, character-for-character, to the unstyled rendering. Colored table cells never break column alignment: widths are measured from the plain text, not the escaped string.

## Error envelope

When a command fails in `json` mode, the error is rendered as a structured envelope rather than free text:

```json
{"status": "error", "error": "<message>", "http_status": 403}
```

`http_status` is omitted for errors that never made it to the wire (e.g. no narrowing factor configured). In `text` mode, errors go to stderr and nothing is written to stdout.

When the server responds with the shared changeset fallback shape `{"errors": {...}}` (used for validation failures like missing / invalid fields), the CLI emits `error: "invalid"` alongside a `details` field carrying the raw errors map — so agents can act on per-field messages without re-parsing a JSON-in-string blob:

```json
{"status": "error", "http_status": 422, "error": "invalid", "details": {"title": ["can't be blank"]}}
```

### `hint` on auth / scope failures

The auth and scope error codes (`unauthenticated`, `second_factor_required`, `invalid_project_key`, `invalid_agent_secret`, `forbidden`, `workspace_mismatch`, `workspace_required`, and `not_found` when a workspace is selected — see [workspaces](../workspaces/DESIGN.md)) carry an extra `hint` string — a plain-language reading of the code plus the fix:

```json
{"status": "error", "http_status": 403, "error": "invalid_project_key",
 "hint": "project key \"acmee\" doesn't match any of your projects — check the Project key field on the project's side panel in /tasks (matched case-insensitively, whitespace trimmed)"}
```

The key is **additive**: `status` / `error` / `http_status` keep their meaning and shape, and `hint` is absent on every error without guidance. In `text` mode the same guidance replaces the raw `http 403: invalid_project_key` line and prints the remedy indented beneath it:

```
error: project key "acmee" doesn't match any of your projects
  check the Project key field on the project's side panel in /tasks (matched case-insensitively, whitespace trimmed)
```

`invalid_project_key` and `forbidden` are deliberately worded differently: the first is a typo, the second is a permissions boundary.

## Precedence

Format resolution: explicit `--format` flag → `-h` / `--human` (→ `text`) → `SPRAWL_OUTPUT` env → `json` default. An explicit `--format` always wins, so `--format=json -h` stays `json`. Whitespace and case are normalised; invalid values return an error (no silent fallback).

## Design Decisions

- **JSON is the default, not text**: the CLI's structured output is read by agents and scripts far more often than by a human at a prompt, so the machine shape is what you get without a flag; `-h` is the one keystroke that switches to the human view.
- **Error envelope uses `error` (not `code`)**: mirrors the server's error field so agents parse one shape regardless of origin.
- **Plain-text errors go to stderr only**: avoids corrupting a stdout pipe when `text` mode is used in scripts that check exit status.
- **Styling is text-only and TTY-gated**: keeps machine output (json) and any non-terminal sink pristine, so adding color never risks an agent or a pipe. `-h` reuses the otherwise-conventional help shorthand on purpose — for the human owner, "human output" is the more useful one-keystroke flag; `--help` still works.
