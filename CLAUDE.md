# sprawl — CLI for the sprawl API

HTTP client (JSON on the wire; TOON/JSON/text on stdout). Single static Go binary, shipped via goreleaser.
Used by the human owner and by AI agents. This repo is the *client*;

## Start here

- **The spec**: [`docs/plans/sprawl_cli_evaluation.md`](docs/plans/sprawl_cli_evaluation.md) — language, config model, credential rules, and per-phase CLI workload. Read this before making design changes.
- **Backend phase plans** (source of truth for request/response shapes): `/home/ultra/Developer/task_manager/docs/plans/api/`
  - `02_device_flow.md` — login flow (✅ shipped on server)
  - `03_settings_and_pubsub.md` — theme get/set (✅ shipped on server)
  - `04_read_endpoints.md` — tasks/checklist/notes reads (pending)
  - `05_write_endpoints_and_audit.md` — writes + PubSub (pending)
  - `06_documentation_and_verify.md` — owner-secret UI (pending)

## Current server phase

Phases 2–3 are live (`/api/auth/device`, `/api/auth/device/token`, `/api/v1/health`, `GET`/`PATCH /api/v1/settings/theme`). Phases 4–6 are still pending on the server. Implement CLI commands **as the server ships them** — don't build ahead. Verify against `sprawl_dev` targeting `http://localhost:4000` first, then the same code ships as the prod binary on the next release.

## Stack (don't change without updating the spec)

- **CLI framework**: `github.com/spf13/cobra`
- **HTTP**: stdlib `net/http` + `encoding/json` — no extra client deps
- **Config**: TOML via `github.com/BurntSushi/toml`
- **Output**: TOON via `github.com/alpkeskin/gotoon` (default), JSON via stdlib, plain text fallback
- **Releases**: `goreleaser` (stub in `.goreleaser.yaml`)


## Two-binary build pattern (critical)

One codebase produces two binaries. The *only* difference is linker-injected values in `internal/build`:

| Binary | `APIURL` | `AppName` | Config dir |
|---|---|---|---|
| `sprawl` (prod) | `https://sprawl.up.railway.app` | `sprawl` | `~/.config/sprawl/` |
| `sprawl_dev` | `http://localhost:4000` | `sprawl_dev` | `~/.config/sprawl_dev/` |

- **The API URL is never in config.** When prod moves, ship a new release. Do not add a `url` field or a `--url` flag.
- **No `--profile` / `--env` flag.** Binary choice is the environment switch.
- **One-off override**: `SPRAWL_API_URL=…` env var only. Never persists.

Makefile `build` / `build-dev` encode the ldflags. `.goreleaser.yaml` mirrors them for release builds.

## Credential model (do not regress)

| Credential | Storage |
|---|---|
| `token` (device-flow result) | Config file `config.toml`, mode **0600**. |
| `agent_secret` | `SPRAWL_AGENT_SECRET` env var or `--agent-secret` flag. **Never persisted to disk by sprawl.** |

The agent secret scopes per-agent permissions rather than authenticating a trust boundary, so flag convenience is acceptable; sprawl itself still never writes it to disk. Be aware that `--agent-secret <value>` leaves the literal in shell history and `ps auxe` — prefer `SPRAWL_AGENT_SECRET` for long-lived shells and reserve the flag for one-shots.

Resolution order per request:
1. `SPRAWL_TOKEN` env var → else `config.toml` `token`. Missing → "not logged in, run `sprawl login`".
2. `--agent-secret` flag → else `SPRAWL_AGENT_SECRET` env var. Missing → **fail before the HTTP call** with a clear message.

Every `/api/v1/*` call sends `Authorization: Bearer <token>` + `X-Agent-Secret: <secret>`.

## Repo layout

```
cmd/sprawl/          main.go — thin entry point, wires signal.NotifyContext for ctrl+C
internal/build/      ldflag-injected vars (APIURL, AppName, Version, Commit, Date)
internal/cli/        cobra root + subcommands (root.go with version, login.go, health.go, theme.go; auth.go for credential resolution + newAuthedClient helper; output.go for text/json/toon rendering)
internal/client/     stdlib net/http client — BaseURL resolution, CreateDeviceGrant, PollDeviceToken (typed DevicePollError), Health, GetTheme, SetTheme, APIError
internal/config/     XDG-aware Load/Save for config.toml (atomic write, mode 0600, dir mode 0700)
docs/plans/          specs and design notes
Makefile             build / build-dev / build-all / run-dev / test / clean
.goreleaser.yaml     release config (stub — needs owner/repo before first release)
mise.toml            pins Go 1.26.2 for this repo
```

## Common commands

```sh
make build-dev          # dist/sprawl_dev, localhost:4000 baked in
make build              # dist/sprawl, prod URL baked in
make build-all          # both
make run-dev ARGS="version"
make check              # fmt-check + vet + test. Run before declaring a task done.
make test               # tests only
make test-race          # tests + race detector (slower; run before releases)
make tidy fmt vet
```

To change the prod URL at build time without editing the Makefile:
`make build PROD_URL=https://staging.example.com`.

## Invariants (don't break these)

1. Every structured-output subcommand honours `--format=text|json|toon` (persistent flag on root). Default is `toon`; session-wide override via `SPRAWL_OUTPUT`. Login is interactive and stays plain text regardless of `--format` — agents can't approve in a browser anyway.
2. No command writes `agent_secret` to any file, log, or flag default.
3. No command prints the `token` or `agent_secret` to stdout/stderr.
4. URL is never read from config; only baked-in or `SPRAWL_API_URL` env override.
5. Two binaries share 100% of the code; divergence happens only via `internal/build` vars.

## Collaboration rules

do not commit to git

**Test before claiming done.** Every code change must pass `make check` (fmt-check + vet + test) before it's reported as complete. If `check` fails, the change isn't finished — fix it, don't wave it off. Never bypass hooks with `--no-verify` unless the user explicitly tells you to. Tests are mocked with `httptest`; no running backend is required.

## Implemented commands

- `version` — prints ldflag-injected values (APIURL, AppName, Version, Commit, Date).
- `login` — RFC 8628 device flow. POSTs `/api/auth/device`, prints the verification URL + user code, polls `/api/auth/device/token` at the server's `interval` until approval / expiry / denial / `invalid_grant`. Saves the token to `config.toml` (0600) on success, prints the `SPRAWL_AGENT_SECRET` reminder. Ctrl+C cancels cleanly via the root context.
- `health` — resolves token (env → config) and agent secret (flag → env), fails pre-HTTP if secret missing, calls `GET /api/v1/health`. Honours `--format=text|json|toon` (default toon, or `$SPRAWL_OUTPUT`) for both success (`{"status":"ok"}`) and error (`{"status":"error","error":"…","http_status":…}`). Exit 1 on any failure.
- `theme get` — `GET /api/v1/settings/theme`. Renders `{theme:{id,name}}` in the resolved format; text fallback is `Name (id)`. Any authenticated agent (owner or not) can read.
- `theme set <name>` — `PATCH /api/v1/settings/theme` body `{"theme":"<name>"}`. Case-insensitive match on the theme's display name (`"Tokyo Night"`, `"tokyo night"`, `"TOKYO NIGHT"` all match). Owner-only (non-owner → 403 `forbidden`); unknown name → 404 `theme_not_found`; missing body key → 422 `theme_required`. Same structured payload as `theme get` on success; text fallback is `Theme set to Name (id)`. Arg validation (`set` with zero/multi args) is performed inside `RunE` and routed through `reportErr` so the error renders in the chosen format instead of silently exiting.

E2E verified against the local server: token persisted, health round-trip returns 200, error paths (no login / missing secret / wrong secret) render cleanly in text, JSON, and TOON. Theme error paths (missing secret pre-HTTP, invalid secret → 403) verified in this session; success round-trip not re-run (owner secret is intentionally absent from the CLI's env).

## Open TODOs

- Repo lives at `git@github.com:ultrakorne/sprawl_cli.git`. Module path: `github.com/ultrakorne/sprawl_cli`.
- `.goreleaser.yaml` `release:` and `brews:` stanzas are still commented out — uncomment and wire when cutting the first release (owner `ultrakorne`, tap repo TBD).
- Next command work is **gated on server phase 4** landing: read endpoints (`sprawl task list|show|search`, `sprawl checklist <task_id>`, `sprawl note show <item_id>`). Don't build ahead.
- **Automated tests pending.** The plan is documented in `docs/plans/test_plan.md`: pure unit tests for `config`, `client`, `cli/output`, `cli/auth`, plus `httptest`-driven integration tests that round-trip the server's controller matrix (200 / 401 / 403 `invalid_agent_secret` / 403 `forbidden` / 404 / 422 / network errors) for every endpoint. Must land before phase 4 so the read endpoints reuse the `httptest` helper.
