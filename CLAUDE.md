# sprawl — CLI for the task_manager API

JSON-over-HTTP client. Single static Go binary, shipped via goreleaser.
Used by the human owner and by AI agents. This repo is the *client*;
the server lives at `/home/ultra/Developer/task_manager`.

## Start here

- **The spec**: [`docs/plans/sprawl_cli_evaluation.md`](docs/plans/sprawl_cli_evaluation.md) — language, config model, credential rules, and per-phase CLI workload. Read this before making design changes.
- **Backend phase plans** (source of truth for request/response shapes): `/home/ultra/Developer/task_manager/docs/plans/api/`
  - `02_device_flow.md` — login flow (✅ shipped on server)
  - `03_settings_and_pubsub.md` — theme get/set (pending)
  - `04_read_endpoints.md` — tasks/checklist/notes reads (pending)
  - `05_write_endpoints_and_audit.md` — writes + PubSub (pending)
  - `06_documentation_and_verify.md` — owner-secret UI (pending)

## Current server phase

Phase 2 is live (`/api/auth/device`, `/api/auth/device/token`, `/api/v1/health`). Phases 3–6 are still pending on the server. Implement CLI commands **as the server ships them** — don't build ahead. Verify against `sprawl_dev` targeting `http://localhost:4000` first, then the same code ships as the prod binary on the next release.

## Stack (don't change without updating the spec)

- **CLI framework**: `github.com/spf13/cobra`
- **HTTP**: stdlib `net/http` + `encoding/json` — no extra client deps
- **Config**: TOML via `github.com/BurntSushi/toml` (added when config-read is implemented)
- **Releases**: `goreleaser` (stub in `.goreleaser.yaml`)

Rationale for Go: single static ~2.5 MB binary, trivial cross-compile, instant cold start, matches agents-as-users model. Elixir and Rust were considered and ruled out — see the spec.

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
| `agent_secret` | **`SPRAWL_AGENT_SECRET` env var only. Never on disk, never a flag.** |

Why the agent secret is env-only: any process running as the user — including AI agents with shell access — can read `~/.config/…`. Writing the secret there defeats the per-agent permission system. AI agents get their *own* non-owner agent keys; the user exports that key's secret into the AI's shell env.

Resolution order per request:
1. `SPRAWL_TOKEN` env var → else `config.toml` `token`. Missing → "not logged in, run `sprawl login`".
2. `SPRAWL_AGENT_SECRET` env var. Missing → **fail before the HTTP call** with a clear message.

Every `/api/v1/*` call sends `Authorization: Bearer <token>` + `X-Agent-Secret: <secret>`.

## Repo layout

```
cmd/sprawl/          main.go — thin entry point
internal/build/      ldflag-injected vars (APIURL, AppName, Version, Commit, Date)
internal/cli/        cobra root and subcommands
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
make test
make tidy fmt vet
```

To change the prod URL at build time without editing the Makefile:
`make build PROD_URL=https://staging.example.com`.

## Invariants (don't break these)

1. Every subcommand supports `--json` for agent consumption. Root has it as a persistent flag.
2. No command writes `agent_secret` to any file, log, or flag default.
3. No command prints the `token` or `agent_secret` to stdout/stderr.
4. URL is never read from config; only baked-in or `SPRAWL_API_URL` env override.
5. Two binaries share 100% of the code; divergence happens only via `internal/build` vars.

## Open TODOs

- Repo lives at `git@github.com:ultrakorne/sprawl_cli.git`. Module path: `github.com/ultrakorne/sprawl_cli`.
- `.goreleaser.yaml` `release:` and `brews:` stanzas are still commented out — uncomment and wire when cutting the first release (owner `ultrakorne`, tap repo TBD).
- Nothing is implemented yet beyond `version`. Next: `sprawl login` (device flow) and `sprawl health`, per the phase-2 section of the spec.
