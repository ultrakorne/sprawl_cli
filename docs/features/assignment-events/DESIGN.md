# Assignment Events — Design

## Overview

An **assignment event** is the server's record that an item was assigned to an agent key (`assigned`) or taken off it (`removed`, including reassignment to someone else and bulk clears). Only `agent_key` assignees produce events; assigning a person produces none. A key sees only its own events, in the selected workspace and, under a project key, that project.

Events carry identifiers only — event id, change, the item's `assignment_revision` after the change, workspace, task and item ids, the actor and `occurred_at`. Content comes from `item <checklist_item_id>`; whether the event is still current comes from comparing its revision with the item's `assignment_revision`.

## `events watch`

Long-polls the feed and prints one JSON line per thing that happened, flushed immediately:

| Line | When |
|---|---|
| `{"type":"start","cursor":…}` | Once, when started without `--after`: the head cursor, before anything else. |
| `{"type":"assignment_event","cursor":…,"event":{…}}` | Per event, oldest first. `cursor` is the position just after it. |
| `{"type":"cursor_expired","cursor":…}` | The `--after` cursor had expired; the watch resumed from the fresh head it carries. |

`--after <cursor>` resumes after a line's cursor; `--wait` (1–30, default 25) is how long each request waits for an event. `--workspace` selects the workspace like every other workspace-bound command. `--format=text` prints the same stream as human lines for a person watching a terminal.

**sprawl never stores the cursor.** The consumer persists a line's cursor after it has handled that line and restarts with `--after` — so a crash between handling and persisting replays the line (at-least-once), never skips it. Keeping the cursor out of sprawl follows the credential model: nothing about a session is written to disk except the token.

**A cursor expires 30 days after the line that carried it** (for an event line, 30 days after the event). Idle polls advance the watch's position in memory only and never print it, so on a quiet key the persisted cursor ages from its line, not from the last poll: resuming it more than 30 days later gets `cursor_expired` and a reconcile, never a silent gap.

**Reconcile points.** `start` and `cursor_expired` both mean "events before this cursor are not coming": run `queue --assignee me` there and treat its result as the truth. A listener persisting the `start` cursor *before* that inventory run cannot miss an assignment made in between — it is either in the inventory or after the cursor.

**Failures.** Network errors, timeouts, `408`, `429` and `5xx` are retried on the same cursor with a backoff from 1 s doubling to 30 s, each attempt reported on stderr; stdout carries only stream lines. Any other refusal — `401`, `403`, `404`, `422` (e.g. `invalid_cursor`: malformed, or another key's) — ends the stream with the standard error envelope and a non-zero exit, because retrying can't change the answer. SIGINT / SIGTERM end the watch with exit 0, even while a write is blocked on a consumer that stopped reading; a second signal kills it outright.

## `events show <id>`

Reads one event (`{"event": {…}}`), accepting the MCP form `ae_<id>` too. It is the evidence for "who assigned this?": an item's `last_actor` moves with every later edit, an event's `actor` does not. Another key's event, or one outside the selected workspace or confined project, is `404`.

## `queue --assignee me`

The **inventory**: every incomplete, visible item assigned to the calling agent key, in any state, grouped by task like every queue. `--state` becomes optional and narrows it. Only `me` is accepted — the server resolves it from the presented credentials, so there is no id to get wrong. Its text table keeps the `STATE` column, because unlike a by-state queue the state varies per row.

## Design decisions

- **A stream of lines, not a page.** A listener is a pipe reader; one self-contained JSON object per line, each with its own resume point, is the smallest contract it can rely on.
- **The `start` line exists for the reconcile race**, not for display: without it a fresh listener has no cursor to persist before running the inventory.
- **`removed` is an event, not silence.** A listener that started work on an item needs to hear that it is no longer theirs.
- **`me` only.** Listing another key's assignments is a different question with different permissions; `task <id>` already shows any item's assignee.
