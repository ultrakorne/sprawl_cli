# Authentication & Configuration — Technical

## Architecture

`internal/build` holds ldflag-injected vars (`APIURL`, `AppName`, `Version`, `Commit`, `Date`). `internal/client` reads them at BaseURL time (with `SPRAWL_API_URL` env override taking precedence). `internal/config` handles XDG-aware load/save of the token. `internal/cli/auth.go` resolves the token (env → config), the agent secret (flag → env), and the project key (flag → env) per request and builds an authed `*client.Client`; having neither narrowing factor fails before any HTTP call.

## Source Files

| File | Role |
|------|------|
| `cmd/sprawl/main.go` | Thin entry point; wires `signal.NotifyContext` for ctrl+C. |
| `internal/build/build.go` | Vars set by `-ldflags` at build time. |
| `internal/config/config.go` | XDG-aware `Load` / `Save` for `config.toml`; atomic write, mode 0600 / 0700. |
| `internal/client/client.go` | `BaseURL`, `CreateDeviceGrant`, `PollDeviceToken` (typed `DevicePollError`), authed client constructor + `WithProjectKey` option. |
| `internal/cli/login.go` | Device-flow command: grant → print URLs → poll → persist token. |
| `internal/cli/auth.go` | `resolveToken`, `resolveAgentSecret`, `resolveProjectKey`, `newAuthedClient` (bearer + ≥1 narrowing factor), `newUserScopedClient` (agent-secret-only path for user-level writes). |
| `internal/cli/output.go` | `explainAPIError` — maps the auth / scope error codes to a plain-language headline + remedy (text) and a `hint` key (json). |
| `Makefile` | Encodes the ldflags for `build` / `build-dev` / `build-all`. |
| `.goreleaser.yaml` | Mirrors the Makefile ldflags for release builds (stub — `release:` / `brews:` stanzas still commented out). |

## Resolution Order

Per-request credential resolution:

1. Token: `SPRAWL_TOKEN` env → `config.toml` `token`. Missing → "not logged in, run `sprawl login`".
2. Agent secret: `--agent-secret` / `-s` flag → `SPRAWL_AGENT_SECRET` env.
3. Project key: `--project-key` / `-p` flag → `SPRAWL_PROJECT_KEY` env. Trimmed locally (whitespace-only counts as unset and falls through to the env); length / charset rules stay server-side so the CLI never disagrees with the source of truth.
4. Neither 2 nor 3 present → fail before the HTTP call, naming both env vars.

Every `/api/v1/*` request sends `Authorization: Bearer <token>` plus whichever narrowing headers are configured — `X-Agent-Secret: <secret>` and/or `X-Project-Key: <key>`. Device-flow endpoints (`/api/auth/device`, `/api/auth/device/token`) send none of them; those are unauthenticated.

`PATCH /api/v1/settings/theme` is the one exception: it goes through `newUserScopedClient`, which requires the agent secret and never sends the project key.

## Server behaviour under a project key

| Endpoint | Effect |
|---|---|
| `GET /tasks`, `/tasks/search`, `/activity_log` | Pre-filtered to that project — no client-side filtering anywhere in the CLI. |
| `GET /tasks/:id` | 200 inside the project. Outside, **403 `forbidden`** (verified against a live backend, 2026-08-04; an earlier build answered 404 here, which was the drift the server has since pinned with a test). |
| `GET /checklist_items/:id` | Same rule, inherited from the parent task: 403 outside the project, 404 only when the item genuinely doesn't exist. |
| `POST /tasks` | Lands in the project. `project_id` may be omitted entirely; naming a *different* project is 403. |
| Writes | Unchanged inside the project, 403 outside. `PATCH /tasks/:id` has never been able to move a task between projects, so a confined client can't escape. |
| `PATCH /settings/theme` | 403 — user-level, not project-level. `GET` still works. |

Projectless tasks are invisible under confinement: they belong to no project, so they're outside every project.

`task delete` / `item delete` treat a 404 `not_found` as idempotent success. A **403 is not** covered by that shortcut — the record exists and is out of reach, so `isNotFoundAPIError` deliberately doesn't match it and the command errors with the project-key-aware guidance below. That is the honest answer for anything outside the confined project.

A 404 under a project key is still possible (the record is genuinely gone), and it stays ambiguous enough to be worth wording carefully, so `deletedText` switches to `No task #<id> in project "<key>" (nothing deleted — it may exist outside this project key)`. Unconfined wording is unchanged.

## Error-code guidance

`explainAPIError` (in `internal/cli/output.go`) covers the failures a user can fix from the shell. Text output replaces the raw `http 403: <code>` line with the headline and prints the remedy indented beneath it; json keeps `status` / `error` / `http_status` unchanged and adds a `hint` string (`headline — remedy`). Codes without guidance (404s, changeset failures, transport errors) render exactly as before, with no `hint` key.

| Status | `error` | Headline |
|---|---|---|
| 401 | `unauthenticated` (or any bare 401) | bearer token rejected → re-run `login` |
| 403 | `second_factor_required` | no project key or agent secret reached the server |
| 403 | `invalid_project_key` | project key `"x"` doesn't match any of your projects (a typo) |
| 403 | `invalid_agent_secret` | agent secret rejected → check `/auth-settings` |
| 403 | `forbidden` | not allowed on this resource (a permissions boundary, not a typo) — the remedy names the project key when one is set |

## Noteworthy Behavior

- **`PollDeviceToken` returns a typed `*DevicePollError`** with `Code` set to the RFC 8628 value (`authorization_pending`, `access_denied`, `expired_token`, `invalid_grant`). The login command uses it to decide whether to keep polling or stop with a user-visible message.
- **Poll interval honours the server's value** returned in the grant; the CLI does not cap or accelerate it.
- **Config save is atomic**: writes to a sibling tempfile then renames, so a failed write never truncates an existing `config.toml`.
- **`BaseURL()` strips a single trailing slash** from both `SPRAWL_API_URL` and `build.APIURL` so downstream path composition is unambiguous.

## Dependencies

- `github.com/BurntSushi/toml` — config encoding.
- `github.com/spf13/cobra` — command tree.
