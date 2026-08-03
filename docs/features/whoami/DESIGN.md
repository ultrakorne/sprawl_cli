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
  "project": {
    "id": <int>,
    "name": "<string>",
    "key": "<string>",
    "level": "none" | "read" | "write" | "write_create",
    "github_url": "<string>" | null
  } | null,
  "project_permissions": [
    { "project_id": <int>, "name": "<string>", "level": "read" | "write" | "write_create" }
  ]
}
```

`project` is the confinement this call ran under — non-null only when a project key was sent (`--project-key` / `$SPRAWL_PROJECT_KEY`). `level` is what the caller actually resolves to there, and can be `"none"`: a valid key naming a project this agent key cannot reach. That is deliberately not an auth error, so `whoami` can say "the key is valid but this agent has no access" instead of leaving the user staring at an empty task list.

`github_url` is the confined project's repo — the address a checklist item's PR number is resolved against (see [item-state-and-pr](../item-state-and-pr/INDEX.md)). It is always emitted, `null` when the project has no repo, which is also how a pre-rollout server's absent field decodes. It is reachable here **only under a project key**: an unconfined `whoami` has no project block at all, so the URL then only arrives nested in task payloads.

`project_permissions` lists only overrides whose level rank is strictly higher than `default_permission`. Owner keys and `write_create` defaults always come back with `[]` because nothing can elevate them. The list is sorted by `project_id` ascending; level strings are full (`"read"` / `"write"` / `"write_create"`), not abbreviated.

## Behaviour

- **No narrowing factor fails before the HTTP call** with a clear pre-flight error, catching misconfiguration without burning a server round-trip.
- Structured output honours `--format` / `SPRAWL_OUTPUT` like every other command. Default is TOON.
- `--format=text` shows agent identity (`emoji name #id`), the default permission (or `role: owner`), and one line per *level* group of elevated projects (`write_create in: Foo, Bar`). Empty list renders as `(none)`.
- Under a project key, `--format=text` adds these lines between the agent and the permission list:

  ```
  working in: Acme Corp (acme)
    access:   write_create
    github:   https://github.com/acme/widgets
  ```

  With `level: none` the access line spells out the situation instead of showing a bare word: *"none — the key is valid but this agent has no access to that project"*. The `github:` line is printed **only when the project has a repo** — its absence is not an error, it just means PR numbers on this project's items render bare. Without a project key the output is unchanged from before project keys existed — no project lines at all.
- Errors come through the standard envelope: 401 on bad/missing token, 403 on `invalid_project_key` / `invalid_agent_secret` / `agent_key_revoked` / `forbidden`. The auth codes also carry a `hint` (see [output-formats](../output-formats/DESIGN.md)).
