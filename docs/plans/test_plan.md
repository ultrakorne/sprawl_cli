# sprawl CLI — test plan

Two layers. The CLI is small; three-layer pyramids are overkill.

Tests are pure-Go and require no running backend — all HTTP is mocked with `net/http/httptest`. Run with `make check` (fmt-check + vet + test), `make test`, or `make test-race`.

## Layer 1 — unit tests (no network)

### `internal/config`

- `Load`/`Save` round-trip.
- `Save` writes atomically (intermediate tmp file does not clobber an existing `config.toml` on failure).
- File mode is `0600`, directory mode is `0700` on create.
- Missing file → empty `Config`, no error.
- Empty/whitespace-only file → empty `Config`, no error.
- XDG resolution: honours `XDG_CONFIG_HOME`; falls back to `~/.config/<AppName>/`.
- Key off `AppName` so `sprawl` and `sprawl_dev` never collide. Use `t.TempDir` + `t.Setenv("XDG_CONFIG_HOME", ...)` in every test.

### `internal/client`

- `BaseURL()` precedence: `SPRAWL_API_URL` env > `build.APIURL`.
- Trailing-slash stripping on both sources.
- `APIError.Error()` formatting with and without `Code`; body truncation at 200 chars.
- `DevicePollError.Error()`.
- JSON decode of `DeviceGrant`, `Theme` — tolerate extra fields, reject missing required fields gracefully (wrapped in the `decode response` error).
- `do`/`doWithStatus` respect the `accept` predicate: non-2xx with `accept(200,400)` for device token returns no error; unexpected status returns `APIError`.
- Body size cap: response larger than 1 MiB is truncated, not streamed fully.

### `internal/cli` (output)

- `resolveFormat()` precedence: `--format` flag > `$SPRAWL_OUTPUT` env > default `toon`. Whitespace and case handled. Invalid value returns an error (not a panic, not a fallback).
- `renderPayload` produces expected bytes for each of text / json / toon.
- `reportErr`:
  - Text mode → error on stderr only; nothing on stdout.
  - JSON/TOON mode → structured `{status, error, http_status}` on stdout; nothing on stderr.
  - `APIError` → `http_status` + `error` fields populated from `Code`, falling back to `Body`.
  - Plain error → only `error` field, no `http_status`.
  - Returns the original error unchanged so cobra's `RunE` exits non-zero.

### `internal/cli` (auth)

- `resolveToken`: env > config > "not logged in" error, using `t.Setenv("SPRAWL_TOKEN", …)` and a scratch `XDG_CONFIG_HOME`.
- `resolveAgentSecret`: flag > env > error. Verify error wording (users will search for it).
- `newAuthedClient`: missing secret fails before any HTTP call is made (assert no outbound request by using a test server that would panic if hit).

## Layer 2 — HTTP integration (`httptest.Server`)

Spin up a fake backend, point the client at it via `SPRAWL_API_URL`, exercise every documented server response per endpoint. This is the CLI analogue of the server's controller matrix — it proves the CLI faithfully round-trips what the server returns.

### Shared helper

`internal/client/testhelper_test.go` (package-internal; not exported):

- `newTestServer(t, handler)` → `(*httptest.Server, cleanup)`, sets `SPRAWL_API_URL` for the test duration via `t.Setenv`.
- Request recorder that captures method, path, headers (`Authorization`, `X-Agent-Secret`), and body so tests can assert on them after the fact.
- Canned responders for common cases: `respond200JSON(t, map[string]any{…})`, `respondError(status int, code string)`, `respondRawBody(status int, body string)`.

### Scenario matrix

| Scenario | `POST /device` | `POST /device/token` | `GET /health` | `GET /theme` | `PATCH /theme` |
|---|:-:|:-:|:-:|:-:|:-:|
| 200 success | ✓ | ✓ | ✓ | ✓ | ✓ |
| 400 `authorization_pending` → poll continues | — | ✓ | — | — | — |
| 400 `access_denied` | — | ✓ | — | — | — |
| 400 `expired_token` | — | ✓ | — | — | — |
| 400 `invalid_grant` | — | ✓ | — | — | — |
| 400 with empty body | — | ✓ | — | — | — |
| 401 missing bearer | — | — | ✓ | ✓ | ✓ |
| 403 `invalid_agent_secret` | — | — | ✓ | ✓ | ✓ |
| 403 `forbidden` | — | — | — | — | ✓ |
| 404 `theme_not_found` | — | — | — | — | ✓ |
| 422 `theme_required` | — | — | — | — | ✓ |
| 5xx / non-JSON body | ✓ | ✓ | ✓ | ✓ | ✓ (one shared test covers the generic path) |
| Network error (server closed) | ✓ | ✓ | ✓ | ✓ | ✓ |

### Header invariants (critical)

- Every `/api/v1/*` request sends both `Authorization: Bearer <token>` and `X-Agent-Secret: <secret>`. Regression here is a silent auth bypass.
- `/api/auth/device` and `/api/auth/device/token` do **not** send either header (they're unauthenticated).
- `Content-Type: application/json` is only set when there is a body.
- `Accept: application/json` is set on every request.

## Deliberately out of scope

- **Subprocess tests.** Cobra wiring and format rendering are already exercised at the package level; running the compiled binary would triple test time for marginal value. Reconsider when commands grow complex stdin/pipe behaviour (phase 5's `--from-json -`).
- **Live-server integration.** The server has phase-3 controller tests that cover enforcement. Our job is "does the CLI surface what the server returned" — `httptest` proves that without needing a running Phoenix.
- **Coverage thresholds.** At this size the aim is "every behaviour in CLAUDE.md has at least one test"; percentages are not useful.

## Running

```sh
make check        # fmt-check + vet + test. Run before every commit.
make test         # tests only.
make test-race    # with -race. Use before cutting a release.
```

If `make check` fails, the change isn't done. Don't bypass — fix.
