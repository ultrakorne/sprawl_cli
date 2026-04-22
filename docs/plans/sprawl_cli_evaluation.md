# sprawl CLI — Language, Config, and Start-Point

## Context

The API authorization plan (`docs/plans/api_authorization_plan.md`, phases 1–6) builds a REST surface so a CLI — **sprawl** — can act on tasks, checklists, notes, and theme. The CLI lives in its own repo, ships as a single static binary, and is used by the human user and by AI agents.

**Phase 2 is already implemented** on the server: device flow endpoints, `APIAuth` plug, and `/api/v1/health` are live. Further phases (3 theme + PubSub, 4 read endpoints, 5 write endpoints, 6 docs) are pending.

This document is the starting spec for the `sprawl` repo.

---

## Language: Go

`sprawl` is written in Go. Single static binary (~5–10 MB, no runtime deps), trivial cross-compile (`GOOS`/`GOARCH`), instant cold start, and `goreleaser` handles releases + Homebrew/Scoop/checksums from one YAML. Elixir (escript needs Erlang on target; Burrito bundle is ~30 MB with slow boot) and Rust (slower iteration while the server is moving weekly) were considered and ruled out.

### Stack

- **Args**: [`cobra`](https://github.com/spf13/cobra)
- **HTTP**: stdlib `net/http` + `encoding/json` (no need for extra deps)
- **Config**: TOML via [`BurntSushi/toml`](https://github.com/BurntSushi/toml)
- **Releases**: [`goreleaser`](https://goreleaser.com/)

---

## Config model — two binaries, URL baked at compile time

Two key decisions shape the config model:

1. **The API base URL is never stored in the config file.** It's compiled into the binary at build time via `-ldflags`. When the production service moves, ship a new release; users update and the new URL takes effect automatically — no config migration, no "force update" logic, no stale-URL support burden.
2. **Dev and prod are separate binaries, not flag-switched modes.** Ship two binaries from the same codebase: `sprawl` (prod) and `sprawl_dev` (dev). Each has its own baked-in URL and its own config directory, so tokens never collide and there's no `--profile` or `--env` flag to remember.

### Build-time variables

```go
// in main.go (or an internal/build package)
var (
    APIURL    = "https://tasks.example.com"   // overridden at build time
    AppName   = "sprawl"                      // overridden at build time
)
```

```sh
# Prod binary
go build -ldflags "\
  -X 'main.APIURL=https://tasks.example.com' \
  -X 'main.AppName=sprawl'" \
  -o dist/sprawl ./cmd/sprawl

# Dev binary
go build -ldflags "\
  -X 'main.APIURL=http://localhost:4000' \
  -X 'main.AppName=sprawl_dev'" \
  -o dist/sprawl_dev ./cmd/sprawl
```

`goreleaser` ships both from one config — two matrix entries on the same `main.go`. Alternatively, two `cmd/` directories if the builds ever diverge; prefer ldflags until they do.

### Credential storage — agent secret is never persisted

The two-layer auth model (token + agent secret) only does useful work if the agent secret is treated like a password, not like a dotfile. Storing it in a mode-0600 file would mean any process running as the user — including AI agents with shell access — can silently impersonate the human owner, defeating the per-agent permission system entirely.

Simple rule: **the agent secret is never written to disk by `sprawl`.** It is provided per-invocation via the `SPRAWL_AGENT_SECRET` environment variable. The user manages their own shell environment to set it (e.g. a shell alias, a `.envrc` loaded by direnv, a separate authenticated shell, or manual `export` before a session).

| Credential | Storage |
|---|---|
| `token` (device-flow result) | Config file (`config.toml`, mode 0600). Low blast radius alone — a leaked token without an agent secret gets a 403 from the API. |
| `agent_secret` (owner or agent) | **Env var `SPRAWL_AGENT_SECRET` only.** No config field, no keyring, no flag (flags leak into shell history). |

#### Config file schema

`AppName` drives the config directory:

- Prod binary → `$XDG_CONFIG_HOME/sprawl/config.toml` (`~/.config/sprawl/config.toml` fallback)
- Dev binary → `$XDG_CONFIG_HOME/sprawl_dev/config.toml` (`~/.config/sprawl_dev/config.toml` fallback)

Mode 0600. Schema is minimal:

```toml
token = "..."   # from `sprawl login`
```

No `url`. No `agent_secret`. No profiles.

#### Resolution order (per request)

1. `SPRAWL_TOKEN` env var → else config file `token`. Missing → "not logged in, run `sprawl login`".
2. `SPRAWL_AGENT_SECRET` env var. Missing → immediate error before the HTTP call: "SPRAWL_AGENT_SECRET not set — export it for this shell and retry."

#### `sprawl login` UX

After the device flow succeeds:

1. Save `token` to `config.toml` with mode 0600.
2. Print instructions: "Set `SPRAWL_AGENT_SECRET=<your owner key secret>` in your shell to authenticate requests. Get the secret from the server UI (phase 6) or the `ensure_owner_key/1` logs (phase 2 interim)."
3. That's it. No prompt to paste the secret; no storage of the secret.

#### AI agent usage pattern (documented best practice)

- For every AI agent you want to allow CLI access to, create a *new* non-owner agent key via the phase 6 UI with the minimum permissions it needs (e.g. `read` on specific projects).
- Copy that agent's secret exactly once (it's displayed on create, not recoverable).
- Launch the AI with `SPRAWL_AGENT_SECRET=<that agent's secret>` in its environment. Do not share the owner secret with an AI.
- Because the secret is never on disk, there's nothing for the AI to steal just by reading files — it can only impersonate whatever key you explicitly gave it via env.

If an AI's agent secret leaks, the blast radius is whatever that one agent key was scoped to. Revoke it by marking that single key `revoked_at` — no impact on the owner.

### Rare URL override

For one-offs — e.g. testing a PR branch running on a different host — a single escape hatch:

- `SPRAWL_API_URL=https://pr-123.example.com sprawl health`

No `--url` flag on every command; the env var is enough for the rare case and keeps the cobra surface clean. Do not persist it to config — by design, next invocation goes back to the baked-in URL.

### Answering the original questions

- **"Can the URL be force-updated if the service moves?"** Yes — because it's never in the config. Ship a release; the new binary has the new URL. Zero user action beyond `brew upgrade sprawl` (or equivalent).
- **"Can I have `sprawl` and `sprawl_dev` simultaneously without switching?"** Yes — two binaries, two config dirs, each logged in independently. Run `sprawl task list` for prod and `sprawl_dev task list` for local; no flags, no conflicts.
- **"Isn't storing the agent secret in config effectively letting any local process impersonate me?"** Yes — which is exactly why the agent secret is never written to disk. It's passed only via `SPRAWL_AGENT_SECRET` in the shell environment, and AI agents are expected to use their own non-owner agent keys instead of the human's owner secret.

---

## What the CLI needs to implement, per server phase

This is a summary so the CLI dev knows what to build at each point. The server-side phase files (`docs/plans/api/*.md` in task_manager) are the source of truth for request/response shapes; consult them when wiring each command.

### Phase 2 (DONE) — foundation for CLI work

Available on server now:

- `POST /api/auth/device` (no auth) → `{ device_code, user_code, verification_uri, verification_uri_complete, expires_in, interval }`
- `POST /api/auth/device/token` body `{ "device_code": "..." }` → poll until `200 { "token": "..." }`. Error codes follow RFC 8628: `authorization_pending`, `expired_token`, `access_denied`, `invalid_grant`.
- `GET /api/v1/health` → auth'd smoke test.
- Every `/api/v1/*` call requires `Authorization: Bearer <token>` + `X-Agent-Secret: <secret>` headers.

**CLI commands to build now:**

- `sprawl login` (or `sprawl_dev login`) — full device flow: POST to create a grant, print the `verification_uri_complete` (and the `user_code` in case the link can't be clicked), poll `token` at the server's `interval` until approval or expiry. On success, save `token` to the config file and print the env-var reminder: "Set `SPRAWL_AGENT_SECRET` in your shell to authenticate requests."
- `sprawl health` — `GET /api/v1/health`, print `200 ok` or the status + error body. Fails fast with a clear message if `SPRAWL_AGENT_SECRET` is unset.
- Every subcommand inherits `--json` from the cobra root command. No `--url`, no `--profile`, no `--agent-secret` flag — URL is baked in; binary choice (`sprawl` vs `sprawl_dev`) separates environments; agent secret always comes from `SPRAWL_AGENT_SECRET` in the environment.

Start with the `sprawl_dev` target since phase 2 is only running locally today. After this, the CLI's entire auth plumbing is validated end-to-end — later phases are just adding JSON endpoints on top.

### Phase 3 — theme (first real domain command)

Server adds:

- `GET /api/v1/settings/theme` → `{ "theme": { "id", "name" } }`
- `PATCH /api/v1/settings/theme` body `{ "theme": "Tokyo Night" }` → same shape. Case-insensitive name match. Unknown name → 404 `theme_not_found`. Non-owner agent → 403.

**CLI:** `sprawl theme get`, `sprawl theme set <name>`. Shake out the CLI's error-rendering for 401 / 403 / 404 / 422 here — every later command reuses that machinery.

### Phase 4 — read endpoints

Server adds:

- `GET /api/v1/tasks`
- `GET /api/v1/tasks/search?q=<needle>` (empty `q` → 422)
- `GET /api/v1/tasks/:id`
- `GET /api/v1/tasks/:task_id/checklist`
- `GET /api/v1/checklist_items/:id/notes` → `{ "notes": "..." }`

JSON shapes documented in phase 4's plan; key fields on `task_json`: `id`, `title`, `description`, `status`, `due_date`, `project`, `checklist_progress`, `created_by`, `last_actor`.

**CLI:** `sprawl task list`, `sprawl task show <id>`, `sprawl task search <q>`, `sprawl checklist <task_id>`, `sprawl note show <item_id>`. Add `--json` everywhere so agents consume structured output.

### Phase 5 — write endpoints + live push

Server adds:

- `POST /api/v1/tasks` body `{ "task": { ... } }` → 201 + task
- `PATCH /api/v1/tasks/:id` body `{ "task": { ... } }`
- `POST /api/v1/tasks/:task_id/checklist`
- `PATCH /api/v1/checklist_items/:id/completed` body `{ "completed": true|false }`
- `PATCH /api/v1/checklist_items/:id`
- `PUT /api/v1/checklist_items/:id/notes` body `{ "notes": "..." }`

PubSub now broadcasts per-user mutation events; phase 5 is browser ↔ CLI live-reactivity territory, but the CLI itself just needs to issue the HTTP calls — no subscription yet.

**CLI:** `sprawl task create`, `sprawl task update <id>`, `sprawl checklist add <task_id>`, `sprawl checklist check <item_id>`, `sprawl checklist uncheck <item_id>`, `sprawl checklist update <item_id>`, `sprawl note set <item_id>`. For bodies, accept flags (`--title`, `--status`) and `--from-json -` (read JSON from stdin) so agents can pipe objects in directly.

### Phase 6 — docs + owner-secret UI

Server adds a settings page that shows the owner agent secret exactly once. Impact on CLI: `sprawl login` no longer needs to prompt for the secret separately — it can direct the user to the settings URL before the device-flow prompt.

---

## What to copy to the `sprawl` repo

When the new repo is created, seed it with:

1. **This document**, trimmed of task_manager-repo-specific references, as `PLAN.md` or `ROADMAP.md`.
2. **A short protocol reference** — the endpoint summaries above, with exact request/response shapes copied verbatim from `docs/plans/api/02_device_flow.md`, `03_settings_and_pubsub.md`, `04_read_endpoints.md`, `05_write_endpoints_and_audit.md`. This becomes the CLI dev's cheat sheet so they don't re-read phase files repeatedly.
3. **A test fixture list** mirroring the server's controller test matrix (owner / non-owner with write / non-owner with read / missing bearer / missing agent secret / other-user's id) so the CLI's own integration tests hit the same scenarios against a local dev server.

No server code, schemas, or migrations are copied — the CLI is a pure JSON-over-HTTP client.

---

## Verification

- `sprawl_dev login` completes the device flow against the local server and writes credentials to `~/.config/sprawl_dev/config.toml`.
- `sprawl_dev health` returns 200.
- `sprawl login` (prod binary) hits the compiled-in production URL with no flags — verify by `strings ./dist/sprawl | grep https://` to confirm the URL is baked in, and by running the binary on a clean machine.
- Both binaries coexist on the same user account without config collisions (`~/.config/sprawl/` and `~/.config/sprawl_dev/` are distinct).
- One-off override: `SPRAWL_API_URL=https://pr-123.example.com sprawl health` targets the env-var URL for that invocation only; next invocation without the env var goes back to the baked-in URL.

Each later phase lands one or more new subcommands on both binaries simultaneously (they share a codebase); each subcommand is verified against the dev binary first (valid path + 401/403/404/422 failure paths + `--json` output stable), then released as part of the next prod build.
