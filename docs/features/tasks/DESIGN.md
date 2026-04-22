# Tasks — Design

## Overview

Five commands wrap the `/api/v1/tasks*` surface. Reads respect server-side per-agent permission filtering: non-owner agents only see tasks their key resolves `:read` / `:write` / `:write_create` on (task override → project override → `agent_keys.default_permission`). Writes accept explicit flags (`--title`, `--description`, `--project-id`) and / or `--from-json <path|->`; explicit flags override fields parsed from the JSON source, so agents can pipe a template and tweak one field on the command line.

## Components

### `task list`
`GET /api/v1/tasks`. Lists every task the caller can read. Text fallback is a tabwriter-aligned `ID STATUS DUE PROGRESS PROJECT TITLE` table; `(no tasks)` when empty.

### `task show <id>`
`GET /api/v1/tasks/:id`. 404 when the id isn't visible to the caller; 403 when visible by scope but denied by the permission resolver. Text fallback is the multi-line detail (id/title, status, due, project, progress, actors, description).

### `task search <query>`
`GET /api/v1/tasks/search?q=<query>`. Case-insensitive substring match on title, server-side. Empty / whitespace query → 422 from the server; the CLI does not pre-validate.

### `task create`
`POST /api/v1/tasks` body `{"task":{…}}`. Flags: `--title`, `--description`, `--project-id` (integer — parsed locally so non-numeric input fails before the HTTP call), `--from-json <path|->`. Rejected with a local error if the merged attrs map is empty. The caller's own permission is what lets them read the task back — no post-create grant happens server-side. Projectless create (`--project-id` omitted) requires the key's `default_permission` to be `write_create`; project-scoped create requires `write_create` resolved at project scope. A key that's only `write` gets a 403.

### `task update <id>`
`PATCH /api/v1/tasks/:id` body `{"task":{…}}`. `--title` / `--description` / `--from-json` (no `--project-id` — the server's update changeset ignores it). `--description ""` is treated as an explicit clear via `cmd.Flags().Changed("description")`, not as "flag unset".

## Design Decisions

- **No `--project-id` on update**: server-side changeset ignores it; surfacing it would mislead.
- **Empty-attrs rejected locally**: prevents no-op POSTs that would otherwise waste a round-trip.
- **`--description ""` clears**: idiomatic for agents wanting explicit empty, distinguished from the flag being unset.
