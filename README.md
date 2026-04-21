# sprawl

Command-line client for the sprawl API. A single static Go binary — one for prod, one for dev — that authenticates via device flow

## Requirements

- Go **1.26.2** or newer. If you use [mise](https://mise.jdx.dev/), `mise install` inside the repo picks up the version pinned in `mise.toml`.
- For `sprawl_dev`: a running sprawl backend on `http://localhost:4000`.

## Build

Two binaries ship from this codebase. The only difference between them is the API URL and config directory baked in at link time.

```sh
make build-dev   # → dist/sprawl_dev    (targets http://localhost:4000)
make build       # → dist/sprawl        (targets the prod URL)
make build-all   # both
```

Override the prod URL at build time without editing the Makefile:

```sh
make build PROD_URL=https://staging.example.com
```

Other useful targets: `make check`, `make test`, `make fmt`, `make vet`, `make tidy`, `make clean`. See [Testing](#testing) for the day-to-day loop.

## Install

There's no `make install`. Copy the binary onto your `PATH`:

```sh
sudo install -m 0755 dist/sprawl /usr/local/bin/sprawl
sudo install -m 0755 dist/sprawl_dev /usr/local/bin/sprawl_dev
```

Or, if you prefer a user-local install:

```sh
install -m 0755 dist/sprawl ~/.local/bin/sprawl
```

(Make sure `~/.local/bin` is on your `PATH`.)

## First run

The dev binary is the right one for local hacking. Start with it.

```sh
sprawl_dev version           # confirms the URL baked into this build
sprawl_dev login             # device flow: opens a URL, you approve in the browser
```

After `login`, the token is saved to `~/.config/sprawl_dev/config.toml` at mode 0600. The agent secret is **not** stored there — you supply it per-shell via `SPRAWL_AGENT_SECRET` (or per-command via `-s` / `--agent-secret`).

```sh
export SPRAWL_AGENT_SECRET=<your agent secret>
sprawl_dev health            # should print "status: ok"
```

Prod works identically, just with the `sprawl` binary and `~/.config/sprawl/`.

## Commands

| Command | What it does |
|---|---|
| `sprawl version` | Prints the version and the baked-in API URL. |
| `sprawl login` | Runs the RFC 8628 device flow and saves the resulting token. |
| `sprawl health` | Calls `GET /api/v1/health` to verify the full auth pipeline. |
| `sprawl theme get` | Fetches the currently active UI theme. |
| `sprawl theme set <name>` | Sets the active theme by name (case-insensitive). Owner-only. |

More commands land as the backend adds endpoints.

## Flags and environment variables

Persistent flags (work on every command):

| Flag | Description |
|---|---|
| `--format text\|json\|toon` | Output format. Default is `toon`. |
| `-s`, `--agent-secret <value>` | Agent secret for `/api/v1/*` calls. Overrides `$SPRAWL_AGENT_SECRET`. |

Environment variables:

| Variable | Purpose |
|---|---|
| `SPRAWL_AGENT_SECRET` | Agent secret used if `-s` is not passed. |
| `SPRAWL_TOKEN` | Bearer token override. If unset, the token comes from `config.toml`. |
| `SPRAWL_OUTPUT` | Session-wide default for `--format` (`text`, `json`, or `toon`). |
| `SPRAWL_API_URL` | One-off API URL override. Use sparingly — the binary is the environment switch. |

Why TOON by default? The CLI's output is mostly consumed by LLMs, and TOON is 30–60 % cheaper than JSON in tokens while staying lossless. Pass `--format=text` for a human-friendly string or `--format=json` if you're piping into `jq`.

## Config file

Per binary, an XDG-aware TOML file:

- `sprawl`:     `~/.config/sprawl/config.toml`
- `sprawl_dev`: `~/.config/sprawl_dev/config.toml`

The only field currently stored is `token`. File mode is `0600`; directory mode `0700`. Atomic writes mean an interrupted `login` won't truncate an existing file.

## Testing

The suite is pure-Go and needs no running backend — HTTP calls are mocked with `httptest`, so `make test` is safe on any machine.

```sh
make check        # fmt-check + vet + test. Run before every commit.
make test         # tests only.
make test-race    # with -race; slower, use before cutting a release.
```

The expectation is: **every change runs `make check` before it's considered done.** If `check` fails, the change isn't finished — fix it, don't skip it. Don't bypass pre-commit hooks (`--no-verify` is off-limits) if one ever gets added.

Tests live next to the code they cover (`internal/client/*_test.go`, `internal/cli/*_test.go`, `internal/config/*_test.go`). The controller matrix — the set of server responses each endpoint must round-trip correctly (200, 401, 403, 404, 422, network errors) — is encoded in `internal/cli/*_test.go` and reused as new endpoints land.

## Uninstall

```sh
rm -f /usr/local/bin/sprawl /usr/local/bin/sprawl_dev
rm -rf ~/.config/sprawl ~/.config/sprawl_dev
```
