# Tasks — Design

## Overview

The commands wrap the `/api/v1/tasks*` surface. The `task` parent command shows one task from a bare positional id (`task <id>`); list / search / create / update / due / delete are subcommands. Reads respect server-side per-agent permission filtering: non-owner agents only see tasks their key resolves `:read` / `:write` / `:write_create` on (task override → project override → `agent_keys.default_permission`). A project key narrows that further, server-side: `list` and `search` come back pre-filtered to the one project (projectless tasks included in *nothing*), `task <id>` outside it is refused (the running server answers 403), and creates land inside it — the CLI does no filtering of its own in either case. Writes accept explicit flags (`--title`, `--description`, `--project-id`) and / or `--from-json <path|->`; explicit flags override fields parsed from the JSON source, so agents can pipe a template and tweak one field on the command line.

## Components

### `task list`
`GET /api/v1/tasks`. Lists every task the caller can read. Text view is an aligned `ID DUE PROGRESS PROJECT TITLE` table; `(no tasks)` when empty. STATUS has no column — PROGRESS (traffic-light colored: red `0/x`, yellow in-progress, green `x/x`) carries that signal for humans, while `status` stays in the json/toon payload for agents.

### `task <id>` (show)
`GET /api/v1/tasks/:id`. A bare positional id on the `task` parent command shows one task — there is no `show` subcommand (removed for symmetry with `item <id>`; see Design Decisions). 403 when the task is out of reach (including any id outside a project key's project); 404 when it doesn't exist.

The response embeds the task's items under `task.checklist_items: [...]` (ordered by position), each with `has_notes` but **no note body**. The text view is a two-line header — bold title, then the description — followed by the shared item table:

```
  Ship the CLI consolidation
  outline the v2 API and land it behind a flag

  [x]  ID   STATE  PR    NOTES  TITLE
  ─────────────────────────────────────────────────
  [x]  203  -      #412  o      add the migration
  [ ]  204  ◐      -     -      write the docs
```

**No bordered card** — no box, no meta grid, no project / due / progress. The header carries title and description only; the id is absent because you typed it to get here, and an empty description leaves the title line alone. status, project, due date, progress, last_actor and created_by are all still in the json/toon payload, and `task list` carries the first four per task.

The table itself is defined in [items](../items/INDEX.md) — `task <id>`, `item <id>` and `queue` all render the same row.

`--full` opts into `GET /api/v1/tasks/:id?full=true`, which adds each item's `notes` body and expands it on the lines below its row, starting at the `ID` column so the prose gets the width it needs (see [items](../items/INDEX.md)). This is the task read that carries the project, so it's the one that renders a PR number as a clickable link rather than a bare `#412` — see [item-state-and-pr](../item-state-and-pr/INDEX.md).

### `task search <query>`
`GET /api/v1/tasks/search?q=<query>`. Case-insensitive substring match on **task title and item titles** (notes are not searched). Each task in the response carries an additional `matched_checklist_items: [{id,title}, …]` field — `[]` when only the title matched, otherwise one entry per item whose title matched. Tasks dedupe: a task whose title and one or more items both match still appears once. Empty / whitespace query → 422 `query_required` from the server; the CLI does not pre-validate.

**Search is a lookup, not a view.** It answers "which tasks and items mention this?" and hands back the ids you then read with `item <id>` or `task <id>`. Each hit renders as its own block — the task's title and description, then the ids and titles of the items that matched under it — so several hits stack readably:

```
  Ship the CLI consolidation

  ID   TITLE
  ────────────────────────
  203  add the migration
  205  ship it

  Another task that matched
```

A task that matched on its own title has no matching items and renders as the header alone; the title already said why it's there.

The block shows **id and title only**, and deliberately not the shared item table. `/tasks/search` serialises matched items as `{id, title}` — no `completed`, `state`, `pr_number` or `has_notes` — so every other column would print a zero value: an unchecked box next to an item that may well be checked, `-` in STATE for an item that is in review. That reads as data rather than as absence, which is the one thing worth avoiding in a surface whose entire job is telling you where to look next. `client.MatchedChecklistItem` is a distinct two-field type rather than a sparsely-populated `ChecklistItem` so the narrowness is visible at the call site.

### `task create`
`POST /api/v1/tasks` body `{"task":{…}}`. Flags: `--title`, `--description`, `--project-id` (integer — parsed locally so non-numeric input fails before the HTTP call), `--from-json <path|->`. Rejected with a local error if the merged attrs map is empty. The caller's own permission is what lets them read the task back — no post-create grant happens server-side. Projectless create (`--project-id` omitted) requires the key's `default_permission` to be `write_create`; project-scoped create requires `write_create` resolved at project scope. A key that's only `write` on an otherwise-visible project gets a 403.

Server-side `project_id` validation runs before permission checks:

- Malformed (non-integer) `project_id` (only reachable through `--from-json`, since `--project-id` is parsed locally) → 422 `invalid_project_id`.
- Unknown or unowned `project_id` → 404 `not_found` (the caller can't disambiguate "doesn't exist" from "exists but not visible to me").
- Other changeset failures (e.g. missing `title`) → shared `{"errors": {...}}` shape, surfaced by `reportErr` as `error: "invalid"` + `details: <errors>`.
- Non-object `task` wrapper → 422 `invalid_body` (the CLI never emits this itself; it always wraps attrs in a JSON object).

**Under a project key** (`--project-key` / `$SPRAWL_PROJECT_KEY`) creates default to the confined project, so `--project-id` becomes unnecessary — that's the main ergonomic win, since the CLI never has to look up or pass an id. Passing it anyway is honoured when it names that same project; naming any other project is 403 `forbidden`, and there is no way to create a projectless task while confined.

### `task update <id>`
`PATCH /api/v1/tasks/:id` body `{"task":{…}}`. `--title` / `--description` / `--from-json` (no `--project-id` — the server's update changeset ignores it). `--description ""` is treated as an explicit clear via `cmd.Flags().Changed("description")`, not as "flag unset".

### `task delete <id>`
`DELETE /api/v1/tasks/:id`. Soft-delete: the row stays in the DB with `hidden=true` and `deleted_at` set, neighbor cards on the canvas are reflowed atomically in the same transaction, and the server broadcasts `task_updated` for each moved neighbor plus a final `task_deleted` on PubSub. **There is no API to restore** — the LiveView trash bin is the only undo path. Server returns 204 No Content; the CLI emits `{id: "<id>", deleted: true}` (json/toon) or `Deleted task #<id>` (text) so structured consumers always get a payload. A 404 `not_found` is treated as success — repeated deletes and deletes against an id that never existed render the same payload, matching the idempotent semantics of HTTP DELETE. Other 4xx (401 unauthenticated, 403 forbidden, malformed) surface through `reportErr` like every other command. The 404 idempotency only matches the bare `not_found` code; codes like `theme_not_found` still surface as errors.

**Under a project key the 404 is ambiguous**: the server answers 404 for any task outside the confined project, so the plain "already gone (no change)" line would claim a delete that never happened to a task living elsewhere. Confined, the text fallback becomes `No task #<id> in project "<key>" (nothing deleted — it may exist outside this project key)`. The structured payload is unchanged (`deleted: true, existed: false`), and unconfined wording is untouched.

### `task due <id> <preset>`
`PATCH /api/v1/tasks/:id/due_date` body `{"due": "<preset>" | null}`. Positional preset, validated locally — one of `yesterday` / `today` / `week` / `none`. `none` wires as JSON null and clears the due date; the other three are passed through verbatim and resolved server-side against the user's timezone and `week_end_day` setting. Response is the same `{"task": {...}}` envelope as `task <id>`, with `due_date` carrying the resolved ISO date (or null). Server errors: 422 `invalid_due` (only reachable through a CLI bug, since presets are filtered locally), 404 `not_found` (task not visible to the caller), 403 `forbidden` (no `:write` on the task), 401 `unauthenticated`.

## Design Decisions

- **`task <id>` instead of `task show <id>`**: a bare positional id shows the task, matching `item <id>` so the read syntax is `<noun> <id>` across both nouns. The `show` subcommand was removed outright (no alias) — pre-public, so the churn is acceptable. `task show 42` now falls through to the parent RunE as two positional args and fails the `ExactArgs(1)` check with cobra's generic usage error; no special migration message.
- **`--full` is opt-in, server-assembled**: the bundled task+items+notes view is one `?full=true` call the server assembles atomically, not a CLI fan-out — keeping the CLI a thin 1:1 wrapper and avoiding partial-failure semantics. Scoped to `task <id>` and `queue` only (not `list` / `search`, which would multiply the response by every checklist's length for something those callers render as a progress bar).
- **The card is gone.** `task <id>` used to render a bordered box with a `project · due · progress` grid above its items. All three of those already appear per task in `task list`, and the box competed with the item table for attention. The header was cut to title + description — and the description was originally cut too, then added back specifically to rescue it. Everything removed is still reachable in `--format=json`.
- **No `--project-id` on update**: server-side changeset ignores it; surfacing it would mislead.
- **Empty-attrs rejected locally**: prevents no-op POSTs that would otherwise waste a round-trip.
- **`--description ""` clears**: idiomatic for agents wanting explicit empty, distinguished from the flag being unset.
- **`task due` is a separate verb**: the update changeset ignores `due_date`, so accepting `--due` on `task update` would silently no-op. The dedicated route also takes a preset name (write-only sugar), not a date — bundling it with `--title` / `--description` would mix two write modes.
- **Preset validated in the CLI**: bounded enum, matches the `--project-id` precedent — clean local error beats a server 422 round-trip.
- **`none` instead of `--clear`**: keeps the surface positional-only and parallels the read shape (server returns `null` for cleared dates).
- **404 on delete is success**: HTTP DELETE is idempotent by spec, and agents reasonably retry. Surfacing the 404 would force every caller to special-case it; swallowing it locally keeps the success payload stable across retries. Scoped to the bare `not_found` code so unrelated 404s aren't masked.
- **Synthetic `{id, deleted: true}` body on 204**: the server replies with no body, but json / toon consumers expect *something* to parse. The CLI fabricates a minimal envelope so output stays uniform across formats.
