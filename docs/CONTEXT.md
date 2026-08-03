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

## Item state

The hand-set marker on a **checklist item** saying where its work stands: `ready_to_pickup`, `in_progress`, `in_review`, or none. It is mutually exclusive with completion — an item carrying a state is incomplete by construction.
_Avoid_: item status, stage, phase, column

## Task status

The task-level `status` string, **derived** server-side from how many of the task's checklist items are checked. Nothing sets it by hand.

> **item state vs task status** — different concepts that happen to share the string `in_progress`. The item state is hand-set on a single checklist item; the task status is computed from checked counts across the whole task. Never call an item's state its "status".

## PR number

The GitHub pull-request number attached to a **checklist item** — a bare positive integer, stored without a URL. Independent of state and completion; only an explicit clear removes it.
_Avoid_: PR link, PR url, pull request id

## Repo URL

A **project's** GitHub repository address (`github_url`), the thing a **PR number** is resolved against to produce a link. A project without one is normal, not an error — its items render PR numbers bare.
_Avoid_: github link, repo link, remote

## Queue

The set of **checklist items** in one **item state** across every task the caller can read — an agent's "what can I pick up?" view, surfaced by the top-level `queue` command. It is a query, not a stored list.
_Avoid_: backlog, inbox, board

## Relationships

- A **task** owns ordered **checklist items**; a **checklist item** owns at most one **note**, at most one **item state**, and at most one **PR number**.
- A **task** belongs to at most one project; that project supplies the **repo URL** that turns the item's **PR number** into a link.
- A **queue** is every **checklist item** sharing one **item state**, across tasks.

## Flagged ambiguities

- "status" was used for both an item's hand-set **item state** and a task's derived **task status** — resolved: these are distinct, and only the task has a status.
- "PR" was used for both the **PR number** and the assembled link — resolved: the number is what's stored; the link is built on the fly from the project's **repo URL**.
