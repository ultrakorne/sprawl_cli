# Activity — Design

## Overview

`sprawl activity` resolves credentials and calls `GET /api/v1/activity_log`. The response is the day's completed tasks plus the day's completed checklist items, both scoped to what the caller is allowed to see (same cascade as `task list`). Default day is today in the user's timezone. Exit 1 on any failure.

## Flags

- `--date YYYY-MM-DD` — pick an explicit calendar day.
- `--days-ago N` — non-negative integer, `0` = today, `1` = yesterday, …
- The two flags are mutually exclusive. `--days-ago` is range-checked locally for non-negativity / integer-ness; the **365-day upper bound is enforced server-side** so the CLI doesn't have to track the limit.
- Omit both ⇒ today.

## Wire shape

```json
{
  "status": "ok",
  "date": "YYYY-MM-DD",
  "completed_tasks": [ <Task>, ... ],
  "completed_items": [
    {
      "id": <int>,
      "title": "<string>",
      "completed": true,
      "completed_at": "<iso8601>",
      "position": <int>,
      "has_notes": <bool>,
      "last_actor": { "type": "user" | "agent", "id": <int>, "emoji": "<string>" } | null,
      "task": {
        "id": <int>,
        "title": "<string>",
        "project": { "id": <int>, "name": "<string>", ... } | null
      }
    }
  ]
}
```

`completed_tasks` reuses the same `Task` shape as `task list` / `task <id>` so consumers can route both feeds through one renderer. `completed_items` extends a checklist item with `completed_at` (the timestamp the day grouping is built on) and a trimmed parent-task summary (`id`, `title`, `project`) so a client can group items by task without a follow-up fetch.

The server response is unenveloped; the CLI re-injects `"status":"ok"` so structured output matches the other read commands.

## Behaviour

- **Local validation runs before the HTTP call.** Mutual exclusion of `--date` / `--days-ago`, integer / non-negative check on `--days-ago`, and `YYYY-MM-DD` parseability on `--date` all fail with a crisp pre-flight error. The 365-day cap remains a server concern.
- **Missing agent secret fails before the HTTP call** via the shared `newAuthedClient` pre-flight, same as every other authed command.
- Structured output honours `--format` / `SPRAWL_OUTPUT`. Default is TOON.
- `--format=text` renders two tabwriter-aligned tables — completed tasks (reusing the `task list` columns) then completed items (`ID  COMPLETED_AT  PROJECT  TASK  TITLE`) — separated by a blank line, headed by `activity for YYYY-MM-DD`. When both arrays are empty the output collapses to a single `(no activity)` line so emptiness is unambiguous.
- Errors come through the standard envelope: 401 on bad/missing token, 403 on `agent_secret_required` / `invalid_agent_secret` / `agent_key_revoked`, 422 `invalid_date_params` if the server rejects the date combo (the CLI's pre-flight catches the common cases first).
