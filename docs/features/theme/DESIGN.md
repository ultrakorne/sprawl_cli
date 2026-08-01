# Theme — Design

## Overview

The server holds a single theme id (e.g. `tokyo-night`, `catppuccin-latte`, `gruvbox`). `theme get` reads it; `theme set <id>` writes it (owner-only). Ids are lowercase kebab-case on the wire.

## Components

### `theme get`
`GET /api/v1/settings/theme`. Any authenticated agent can read. Wire shape: `{"theme": "<id>"}` (flat string, not an object). Text fallback is just the id.

### `theme set <id>`
`PATCH /api/v1/settings/theme` body `{"theme": "<id>"}`. Owner-only (non-owner → 403 `forbidden`). **Requires an agent secret** — see below. Unknown / mis-cased id → 404 `theme_not_found` from the server. Missing / blank `theme` key and any other changeset validation failure returns the shared fallback shape `{"errors": {...}}` (no top-level `error` code) — the CLI surfaces this as `error: "invalid"` + `details: <errors>` in json / toon output. Text fallback is `Theme set to <id>`.

## The theme is user-level, not project-level

A theme belongs to the user, not to a project, so the server rejects `PATCH /api/v1/settings/theme` outright whenever the request carries a project key (`X-Project-Key`) — regardless of what else the caller sends. `GET` is unaffected.

The CLI mirrors that instead of fighting it: `theme set` builds its client through `newUserScopedClient`, which **never sends the project key** and **requires an agent secret**. So:

- Agent secret configured (with or without a project key) → works; the key is simply not sent.
- Project key only → fails before the request with `the theme is a user-level setting, not project-level: agent secret not set — export SPRAWL_AGENT_SECRET or pass --agent-secret`.

That is the intended shape for agents confined to a project: they can read the theme, and they have no business changing it.

## Design Decisions

- **No client-side id normalisation.** The arg goes on the wire verbatim; the server is the single source of truth for valid ids. Hard-coding a list in the CLI would drift.
- **Flat string envelope**, not `{"theme": {"id": "…", "name": "…"}}`. The server no longer returns the display name.
