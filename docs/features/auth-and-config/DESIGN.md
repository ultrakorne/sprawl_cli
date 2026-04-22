# Authentication & Configuration — Design

## Overview

sprawl authenticates to the API with two credentials: a user **token** (obtained via the RFC 8628 device flow) and an **agent secret** (scopes per-agent permissions). The token is persisted in `config.toml` (mode 0600); the agent secret is never written to disk by sprawl.

## Components

### `sprawl login` (device flow)

Interactive: prints the settings URL (`<api-url>/settings`) so the user can grab their owner agent secret, then prints the verification URL + user code and polls `/api/auth/device/token` at the server's `interval`. On success, writes the token to `config.toml` and repeats the settings-URL reminder alongside a `SPRAWL_AGENT_SECRET` export hint. Ctrl+C cancels cleanly through the root context.

### Two-binary build

One codebase produces `sprawl` (prod URL baked in) and `sprawl_dev` (local URL baked in). Environment switching is *which binary you run*, not a flag. Each binary has its own config directory (`~/.config/sprawl/` vs `~/.config/sprawl_dev/`) so tokens never collide.

| Binary | `APIURL` | `AppName` | Config dir |
|---|---|---|---|
| `sprawl` | `https://sprawl.up.railway.app` | `sprawl` | `~/.config/sprawl/` |
| `sprawl_dev` | `http://localhost:4000` | `sprawl_dev` | `~/.config/sprawl_dev/` |

### Credential storage

- **Token**: `config.toml` at `$XDG_CONFIG_HOME/<AppName>/` (or `~/.config/<AppName>/` fallback), file mode 0600, directory mode 0700.
- **Agent secret**: `SPRAWL_AGENT_SECRET` env var or `--agent-secret` / `-s` flag. Never written to disk by sprawl. Flag is supported for one-shots, but `ps auxe` and shell history both leak it — prefer the env var for long-lived shells.

## Design Decisions

- **URL is never in config**: when prod moves, ship a new release. Avoids config migration and stale-URL support burden.
- **No `--profile` / `--env` flag**: binary choice is the switch. Makes the env selection visible in shell history.
- **`SPRAWL_API_URL`** is the only runtime URL override, intended for PR-branch testing — never persists.
- **Agent secret not on disk**: if it were mode-0600 in a config file, any process running as the user (including AI agents with shell access) could silently impersonate the owner, defeating per-agent permissions.
- **Login is always plain text** regardless of `--format`: the user has to approve in a browser anyway, so structured output buys nothing; agents can't complete this flow.
