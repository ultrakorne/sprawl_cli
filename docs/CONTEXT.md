# Project Context — Glossary

Shared vocabulary for the sprawl CLI. These terms were resolved during design and had previously been used ambiguously; this file pins down what each one means so docs, code, and conversations stay consistent. Definitions only — implementation lives in the feature docs.

## Terminal palette

The terminal emulator's 16 ANSI colors (indices 0-15). sprawl's human-readable output and the interactive TUI color themselves **exclusively** from these indices, so they adopt whatever theme the user's terminal is running. This is a client-side, presentation-only concern.

## App theme

The server-side sprawl web-app theme — a single named theme id (e.g. `tokyo-night`) read and written via `theme get` / `theme set`. It affects the web UI, not the terminal. It has nothing to do with the terminal palette.

> **terminal palette vs app theme** — these two were previously conflated as "theme". They are unrelated: the terminal palette is how the CLI/TUI render locally; the app theme is a server setting for the web app.

## Task

A unit of work in sprawl, identified by a numeric id, with attributes such as title, description, due date, project, and status. A task owns a set of checklist items. A task has **no** "mark whole task done" operation — its done-ness is derived server-side from the completion state of its checklist items.

## Checklist item

A single entry under a task's checklist, identified by its own numeric id, with a title and a completion flag. The **checklist item — not the task — is the unit the checkbox toggles**, and it is the thing a note attaches to. Completing all of a task's items is what makes the task complete.

> **task vs checklist item** — the task is the container; the checklist item is the togglable, note-bearing unit. There is no API to mark a whole task done directly.

## Note

Free-form text attached to a **checklist item** (not to a task). Each checklist item has at most one note. Read/written via `note show` / `note set` and edited in the TUI's note screen.
