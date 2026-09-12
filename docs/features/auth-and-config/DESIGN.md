# Authentication & Configuration — Design

## Overview

sprawl authenticates to the API with a user **token** (obtained via the RFC 8628 device flow) plus at least one **narrowing factor**: an **agent secret** (act as that agent key, with its per-agent permissions) or a **project key** (confine the request to one project), or both. The token is persisted in `config.toml` (mode 0600); neither narrowing factor is ever written to disk by sprawl. A **workspace selector** (`--workspace` / `$SPRAWL_WORKSPACE`) may accompany the factors to run the request in another workspace; it is a filter over where the factors work, never a factor itself (see [workspaces](../workspaces/DESIGN.md)).

The narrowing factor is not a security boundary — the bearer is the credential. It is a blast-radius control: a project key pins a repo to one project so a CLI (or an agent driving it) can't write into the wrong project or pull every project into context.

## Components

### `sprawl login` (device flow)

Interactive: prints the settings URL (`<api-url>/auth-settings`) so the user can grab their owner agent secret, then prints the verification URL + user code and polls `/api/auth/device/token` at the server's `interval`. On success, writes the token to `config.toml` and repeats the settings-URL reminder alongside a `SPRAWL_AGENT_SECRET` export hint. Ctrl+C cancels cleanly through the root context.

### Two-binary build

One codebase produces `sprawl` (prod URL baked in) and `sprawl_dev` (local URL baked in). Environment switching is *which binary you run*, not a flag. Each binary has its own config directory (`~/.config/sprawl/` vs `~/.config/sprawl_dev/`) so tokens never collide.

| Binary | `APIURL` | `AppName` | Config dir |
|---|---|---|---|
| `sprawl` | `https://sprawl.today` | `sprawl` | `~/.config/sprawl/` |
| `sprawl_dev` | `http://localhost:4201` | `sprawl_dev` | `~/.config/sprawl_dev/` |

### Credential storage

- **Token**: `config.toml` at `$XDG_CONFIG_HOME/<AppName>/` (or `~/.config/<AppName>/` fallback), file mode 0600, directory mode 0700.
- **Agent secret**: `SPRAWL_AGENT_SECRET` env var or `--agent-secret` / `-s` flag. Never written to disk by sprawl. Flag is supported for one-shots, but `ps auxe` and shell history both leak it — prefer the env var for long-lived shells.
- **Project key**: `SPRAWL_PROJECT_KEY` env var or `--project-key` / `-p` flag. Never written to disk by sprawl either. It is not secret (it's the 2–32 char key shown on the project's side panel in `/tasks`), so it can appear in output — `whoami` prints it and the TUI shows it in the header.
- **Workspace selector**: `SPRAWL_WORKSPACE` env var or `--workspace` / `-w` flag, a positive integer id from `sprawl workspace list`. Never written to disk. Not a credential — it does not count toward the narrowing rule and travels as a path prefix, not a header. See [workspaces](../workspaces/INDEX.md).

### Which factor to use

| Situation | Factor |
|---|---|
| A repo / shell that should only ever touch one project | Project key. Creates land there with no `project_id`; everything else is 403. |
| A client that needs its own identity (MCP clients, anything whose edits should carry a distinct attribution emoji) | Agent secret for a non-owner key. |
| A read-only key that should also be pinned to one project | Both — the server intersects them (min of every factor). |

A project-key request with an owner bearer still stamps `last_actor: {"type": "user"}`, exactly like a browser edit. If CLI edits should be visibly attributed to an agent, use an agent secret for a non-owner key.

### Per-repo confinement

The project key is deliberately **not** stored in `config.toml` and there is no repo-level config file. Confinement is the shell's job:

```sh
# .envrc (direnv), or a wrapper script, or your shell profile
export SPRAWL_PROJECT_KEY=acme
```

This keeps the "no sprawl-managed state on disk beyond the token" rule intact and makes the active scope visible in the environment rather than hidden in a file the user forgot about. `--project-key` / `-p` overrides it per invocation.

## Design Decisions

- **URL is never in config**: when prod moves, ship a new release. Avoids config migration and stale-URL support burden.
- **No `--profile` / `--env` flag**: binary choice is the switch. Makes the env selection visible in shell history.
- **`SPRAWL_API_URL`** is the only runtime URL override, intended for PR-branch testing — never persists.
- **Agent secret not on disk**: if it were mode-0600 in a config file, any process running as the user (including AI agents with shell access) could silently impersonate the owner, defeating per-agent permissions.
- **Project key not on disk either**: not for secrecy (it isn't secret) but so there is exactly one place scope comes from — the environment. A `.sprawl.toml` discovered by walking up from the cwd would make "which project am I in?" depend on where you happened to `cd`.
- **Workspace selector not on disk, for the same reason**: a workspace is a resource, not a credential, and the one place scope comes from is the environment. There is deliberately no `workspace use` that writes a default.
- **The workspace selector is not a factor**: it changes which workspace the factors work in and nothing about permission, so it never satisfies the narrowing rule — `-w 3` with neither key nor secret fails locally exactly as before. A malformed id (anything but a positive integer) also fails locally, because the server's only answer would be a 404 that reads as "task not found".
- **Missing narrowing factor fails locally**: the server answers `403 second_factor_required`, but it can't know whether the user meant to set a project key or an agent secret. The CLI fails before the request and names both.
- **`theme set` is the one command a project key can't authorize**: a theme is user-level, so the server rejects any theme write carrying `X-Project-Key`. Rather than send a request that can only 403, `theme set` drops the key, sends the agent secret, and fails pre-flight without one. `theme get` is unaffected and works under either factor.
- **Login is always plain text** regardless of `--format`: the user has to approve in a browser anyway, so structured output buys nothing; agents can't complete this flow.
