# Tasks — Design

## Overview

The commands wrap the `/api/v1/tasks*` surface. The `task` parent command shows one task from a bare positional id (`task <id>`); list / search / create / update / due / delete are subcommands. Reads respect server-side per-agent permission filtering: non-owner agents only see tasks their key resolves `:read` / `:write` / `:write_create` on (task override → project override → `agent_keys.default_permission`). Writes accept explicit flags (`--title`, `--description`, `--project-id`) and / or `--from-json <path|->`; explicit flags override fields parsed from the JSON source, so agents can pipe a template and tweak one field on the command line.

## Components

### `task list`
`GET /api/v1/tasks`. Lists every task the caller can read. Text view is an aligned `ID DUE PROGRESS PROJECT TITLE` table; `(no tasks)` when empty. STATUS has no column — PROGRESS (traffic-light colored: red `0/x`, yellow in-progress, green `x/x`) carries that signal for humans, while `status` stays in the json/toon payload for agents.

### `task <id>` (show)
`GET /api/v1/tasks/:id`. A bare positional id on the `task` parent command shows one task — there is no `show` subcommand (removed for symmetry with `checklist <task_id>`; see Design Decisions). 404 when the id isn't visible to the caller; 403 when visible by scope but denied by the permission resolver. Text view is a bordered card: id/title in the top border, a `project · due · progress` grid, then the description. status, last_actor, and created_by are omitted from text (progress is the human signal; actors are glance-irrelevant) but stay in the json/toon payload.

`--full` opts into `GET /api/v1/tasks/:id?full=true`, which embeds the task's checklist under `task.checklist_items: [...]` (ordered by position), each item carrying its `notes` blob inline alongside the usual `has_notes` flag. One server call replaces the old `task <id>` + `checklist <id>` + per-item `note show` fan-out. The CLI decodes the embedded items into `Task.ChecklistItems` (nil ⇒ field absent on a non-full fetch, so it's suppressed in output). The `--full` text view is the same card as the non-full detail (id/title in the border, a `project · due · progress` grid, the description) with a `CHECKLIST` section appended — each item a colored checkbox, faint id, title, and its (possibly multi-line) notes nested beneath (`(no notes)` for empty). The checklist heading omits the count since the card already shows progress; last_actor / created_by stay out of the human view (still in json/toon for agents). Without `--full` the response and rendered shape are unchanged.

### `task search <query>`
`GET /api/v1/tasks/search?q=<query>`. Case-insensitive substring match on **task title and checklist item titles** (notes are not searched). Each task in the response carries an additional `matched_checklist_items: [{id,title}, …]` field — `[]` when only the title matched (UI hint: "matched on title"), otherwise one entry per checklist item whose title matched. Tasks dedupe: a task whose title and one or more items both match still appears once. Empty / whitespace query → 422 `query_required` from the server; the CLI does not pre-validate.

### `task create`
`POST /api/v1/tasks` body `{"task":{…}}`. Flags: `--title`, `--description`, `--project-id` (integer — parsed locally so non-numeric input fails before the HTTP call), `--from-json <path|->`. Rejected with a local error if the merged attrs map is empty. The caller's own permission is what lets them read the task back — no post-create grant happens server-side. Projectless create (`--project-id` omitted) requires the key's `default_permission` to be `write_create`; project-scoped create requires `write_create` resolved at project scope. A key that's only `write` on an otherwise-visible project gets a 403.

Server-side `project_id` validation runs before permission checks:

- Malformed (non-integer) `project_id` (only reachable through `--from-json`, since `--project-id` is parsed locally) → 422 `invalid_project_id`.
- Unknown or unowned `project_id` → 404 `not_found` (the caller can't disambiguate "doesn't exist" from "exists but not visible to me").
- Other changeset failures (e.g. missing `title`) → shared `{"errors": {...}}` shape, surfaced by `reportErr` as `error: "invalid"` + `details: <errors>`.
- Non-object `task` wrapper → 422 `invalid_body` (the CLI never emits this itself; it always wraps attrs in a JSON object).

### `task update <id>`
`PATCH /api/v1/tasks/:id` body `{"task":{…}}`. `--title` / `--description` / `--from-json` (no `--project-id` — the server's update changeset ignores it). `--description ""` is treated as an explicit clear via `cmd.Flags().Changed("description")`, not as "flag unset".

### `task delete <id>`
`DELETE /api/v1/tasks/:id`. Soft-delete: the row stays in the DB with `hidden=true` and `deleted_at` set, neighbor cards on the canvas are reflowed atomically in the same transaction, and the server broadcasts `task_updated` for each moved neighbor plus a final `task_deleted` on PubSub. **There is no API to restore** — the LiveView trash bin is the only undo path. Server returns 204 No Content; the CLI emits `{id: "<id>", deleted: true}` (json/toon) or `Deleted task #<id>` (text) so structured consumers always get a payload. A 404 `not_found` is treated as success — repeated deletes and deletes against an id that never existed render the same payload, matching the idempotent semantics of HTTP DELETE. Other 4xx (401 unauthenticated, 403 forbidden, malformed) surface through `reportErr` like every other command. The 404 idempotency only matches the bare `not_found` code; codes like `theme_not_found` still surface as errors.

### `task due <id> <preset>`
`PATCH /api/v1/tasks/:id/due_date` body `{"due": "<preset>" | null}`. Positional preset, validated locally — one of `yesterday` / `today` / `week` / `none`. `none` wires as JSON null and clears the due date; the other three are passed through verbatim and resolved server-side against the user's timezone and `week_end_day` setting. Response is the same `{"task": {...}}` envelope as `task <id>`, with `due_date` carrying the resolved ISO date (or null). Server errors: 422 `invalid_due` (only reachable through a CLI bug, since presets are filtered locally), 404 `not_found` (task not visible to the caller), 403 `forbidden` (no `:write` on the task), 401 `unauthenticated`.

## Design Decisions

- **`task <id>` instead of `task show <id>`**: a bare positional id shows the task, matching `checklist <task_id>` so the read syntax is `<noun> <id>` across both. The `show` subcommand was removed outright (no alias) — pre-public, so the churn is acceptable. `task show 42` now falls through to the parent RunE as two positional args and fails the `ExactArgs(1)` check with cobra's generic usage error; no special migration message.
- **`--full` is opt-in, server-assembled**: the bundled task+items+notes view is one `?full=true` call the server assembles atomically, not a CLI fan-out — keeping the CLI a thin 1:1 wrapper and avoiding partial-failure semantics. Scoped to `task <id>` and `checklist <task_id>` only (not `list` / `search`, which would explode into N server-side fan-outs).
- **No `--project-id` on update**: server-side changeset ignores it; surfacing it would mislead.
- **Empty-attrs rejected locally**: prevents no-op POSTs that would otherwise waste a round-trip.
- **`--description ""` clears**: idiomatic for agents wanting explicit empty, distinguished from the flag being unset.
- **`task due` is a separate verb**: the update changeset ignores `due_date`, so accepting `--due` on `task update` would silently no-op. The dedicated route also takes a preset name (write-only sugar), not a date — bundling it with `--title` / `--description` would mix two write modes.
- **Preset validated in the CLI**: bounded enum, matches the `--project-id` precedent — clean local error beats a server 422 round-trip.
- **`none` instead of `--clear`**: keeps the surface positional-only and parallels the read shape (server returns `null` for cleared dates).
- **404 on delete is success**: HTTP DELETE is idempotent by spec, and agents reasonably retry. Surfacing the 404 would force every caller to special-case it; swallowing it locally keeps the success payload stable across retries. Scoped to the bare `not_found` code so unrelated 404s aren't masked.
- **Synthetic `{id, deleted: true}` body on 204**: the server replies with no body, but json / toon consumers expect *something* to parse. The CLI fabricates a minimal envelope so output stays uniform across formats.
