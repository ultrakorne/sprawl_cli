# Whoami — Design

## Overview

`sprawl whoami` resolves credentials (token + agent secret) and calls `GET /api/v1/whoami`. The command renders the calling agent and any project-scoped permission overrides; a 200 doubles as proof the auth pipeline is healthy. Exit 1 on any failure.

## Wire shape

```json
{
  "status": "ok",
  "agent": {
    "id": <int>,
    "name": "<string>",
    "emoji": "<string>",
    "is_owner": <bool>,
    "default_permission": "none" | "read" | "write" | "write_create"
  },
  "project_permissions": [
    { "project_id": <int>, "name": "<string>", "level": "read" | "write" | "write_create" }
  ]
}
```

`project_permissions` lists only overrides whose level rank is strictly higher than `default_permission`. Owner keys and `write_create` defaults always come back with `[]` because nothing can elevate them. The list is sorted by `project_id` ascending; level strings are full (`"read"` / `"write"` / `"write_create"`), not abbreviated.

## Behaviour

- **Missing agent secret fails before the HTTP call** with a clear pre-flight error, catching misconfiguration without burning a server round-trip.
- Structured output honours `--format` / `SPRAWL_OUTPUT` like every other command. Default is TOON.
- `--format=text` shows agent identity (`emoji name #id`), the default permission (or `role: owner`), and one line per *level* group of elevated projects (`write_create in: Foo, Bar`). Empty list renders as `(none)`.
- Errors come through the standard envelope: 401 on bad/missing token, 403 on `agent_secret_required` / `invalid_agent_secret` / `agent_key_revoked`.
