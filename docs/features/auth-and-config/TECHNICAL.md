# Authentication & Configuration — Technical

## Architecture

`internal/build` holds ldflag-injected vars (`APIURL`, `AppName`, `Version`, `Commit`, `Date`). `internal/client` reads them at BaseURL time (with `SPRAWL_API_URL` env override taking precedence). `internal/config` handles XDG-aware load/save of the token. `internal/cli/auth.go` resolves the token (env → config) and agent secret (flag → env) per request and builds an authed `*client.Client`; a missing secret fails before any HTTP call.

## Source Files

| File | Role |
|------|------|
| `cmd/sprawl/main.go` | Thin entry point; wires `signal.NotifyContext` for ctrl+C. |
| `internal/build/build.go` | Vars set by `-ldflags` at build time. |
| `internal/config/config.go` | XDG-aware `Load` / `Save` for `config.toml`; atomic write, mode 0600 / 0700. |
| `internal/client/client.go` | `BaseURL`, `CreateDeviceGrant`, `PollDeviceToken` (typed `DevicePollError`), authed client constructor. |
| `internal/cli/login.go` | Device-flow command: grant → print URLs → poll → persist token. |
| `internal/cli/auth.go` | `resolveToken`, `resolveAgentSecret`, `newAuthedClient` — builds the authed client for every `/api/v1/*` subcommand. |
| `Makefile` | Encodes the ldflags for `build` / `build-dev` / `build-all`. |
| `.goreleaser.yaml` | Mirrors the Makefile ldflags for release builds (stub — `release:` / `brews:` stanzas still commented out). |

## Resolution Order

Per-request credential resolution:

1. Token: `SPRAWL_TOKEN` env → `config.toml` `token`. Missing → "not logged in, run `sprawl login`".
2. Agent secret: `--agent-secret` / `-s` flag → `SPRAWL_AGENT_SECRET` env. Missing → fail before the HTTP call with a clear message.

Every `/api/v1/*` request sends `Authorization: Bearer <token>` + `X-Agent-Secret: <secret>`. Device-flow endpoints (`/api/auth/device`, `/api/auth/device/token`) send neither; those are unauthenticated.

## Noteworthy Behavior

- **`PollDeviceToken` returns a typed `*DevicePollError`** with `Code` set to the RFC 8628 value (`authorization_pending`, `access_denied`, `expired_token`, `invalid_grant`). The login command uses it to decide whether to keep polling or stop with a user-visible message.
- **Poll interval honours the server's value** returned in the grant; the CLI does not cap or accelerate it.
- **Config save is atomic**: writes to a sibling tempfile then renames, so a failed write never truncates an existing `config.toml`.
- **`BaseURL()` strips a single trailing slash** from both `SPRAWL_API_URL` and `build.APIURL` so downstream path composition is unambiguous.

## Dependencies

- `github.com/BurntSushi/toml` — config encoding.
- `github.com/spf13/cobra` — command tree.
