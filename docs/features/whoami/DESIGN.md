# Whoami — Design

## Overview

`sprawl whoami` resolves credentials and calls `GET /api/v1/whoami`. It answers three questions in one call: who the server thinks you are, which workspace and project this session's calls land in, and what you may do there. A 200 doubles as proof the auth pipeline is healthy. Exit 1 on any failure.

## Surface

The response (mirrored in json with `status: ok`) carries:

- **`agent`** — id, name, emoji, `is_owner`, and `default_permission` (`none` / `read` / `write` / `write_create`), the key's baseline scope.
- **`workspace`** — the workspace this session's calls run in: the server's default for the factors, or the `--workspace` selection resolved against the reachable list. Each workspace carries id, name, **role** (the user's own standing) and **level** (what the presented key resolves to there).
- **`workspaces`** — every workspace the user can reach, same shape.
- **`project`** — the confinement this call ran under: id, name, key, the **level** the caller resolves to there, and `github_url` (the repo PR numbers are resolved against; `null` when the project has none). `null` when no project key was sent.
- **`project_permissions`** — per-project overrides ranking strictly above the default, `[]` for owners and `write_create` defaults. Sent only by servers that report permissions this way rather than through workspaces.

`project.level` can be `none`: a valid key naming a project this agent cannot reach. That is deliberately not an auth error, so the command can say "the key is valid but this agent has no access" instead of leaving the user staring at an empty list.

In `text` the view reads top to bottom: `agent:` with `role: owner` or `default:`; `workspace:` (or `workspace (selected):` under a selector) with `role:` and `access:`; `working in:` with `access:` and `github:` when a project key is set; then `elevated project permissions:` grouped one line per level when the server reports them. A level of `none` is spelled out with why lists come back empty rather than shown as a bare word.

## Flows

- **Liveness check** — run it after `login` and exporting a factor; a 200 means the token, the factor and the server all agree.
- **Confirm the scope before writing** — the bundled skill runs it once before the first mutation to read `project.level`.
- **Diagnose an empty list** — `access: none` on the workspace or project says the key cannot reach it; `whoami` names it, `task list` would only be empty.
- **Catch a workspace mismatch early** — under a project key, a `--workspace` naming another workspace than the project's earns a warning here, because every task call would be refused with `workspace_mismatch`.

## Decisions

- **No narrowing factor fails before the HTTP call**, with the same message as every other authed command.
- **The selection is resolved locally.** `whoami` is user-level and ignores the selector, so the CLI matches the selected id against `workspaces` and fails plainly when it is unreachable — before the first task call turns it into a bare 404.
- **Keys are emitted only when the server sent them.** Workspace keys, `project_permissions` and `github_url` follow what the answering server reports; the CLI never invents an empty key, so consumers branch on one condition per feature.
- **`project` is always present**, `null` when unconfined — a server predating project keys omits the field and decodes the same way.
- **Level `none` is explained, not printed.** A bare `none` reads as an error; the sentence says what it means.
- Errors come through the standard envelope with the auth `hint`s (see [output-formats](../output-formats/DESIGN.md)).
