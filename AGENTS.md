# sprawl — CLI for the sprawl API

HTTP client. Single static Go binary (two variants from one codebase), JSON on the wire, JSON / text on stdout. Used by the human owner and by AI agents. The server is live; the CLI wraps its endpoints.

## Documentation

Feature-level docs live under [`docs/`](docs/INDEX.md) and are discovered incrementally — start at `docs/INDEX.md`, then open the feature folder that matches the task (auth-and-config, output-formats, tasks, items, item-state-and-pr, interactive-tui, activity, auto-update, theme, whoami). `docs/CONTEXT.md` is the glossary — check it before naming a concept in code or docs.

## Stack

- **CLI framework**: `github.com/spf13/cobra`
- **HTTP**: stdlib `net/http` + `encoding/json` — no extra client deps
- **Config**: TOML via `github.com/BurntSushi/toml`
- **Output**: JSON (default), plain text fallback
- **Releases**: `goreleaser`

## Two-binary build pattern (critical)

One codebase produces two binaries. The *only* difference is linker-injected values in `internal/build`:

| Binary | `APIURL` | `AppName` | Config dir |
|---|---|---|---|
| `sprawl` (prod) | `https://sprawl.today` | `sprawl` | `~/.config/sprawl/` |
| `sprawl_dev` | `http://localhost:4201` | `sprawl_dev` | `~/.config/sprawl_dev/` |

- **The API URL is never in config.** When prod moves, ship a new release. Do not add a `url` field or a `--url` flag.
- **No `--profile` / `--env` flag.** Binary choice is the environment switch.
- **One-off override**: `SPRAWL_API_URL=…` env var only. Never persists.
- **The dev port is a build argument**: `make build-dev PORT=4300` when the backend isn't on the default 4201.

## Credential model (do not regress)

| Credential | Storage |
|---|---|
| `token` (device-flow result) | Config file `config.toml`, mode **0600**. |
| `agent_secret` | `SPRAWL_AGENT_SECRET` env var or `--agent-secret` / `-s` flag. **Never persisted to disk by sprawl.** |
| `project_key` | `SPRAWL_PROJECT_KEY` env var or `--project-key` / `-p` flag. **Never persisted to disk by sprawl** — per-repo confinement is the shell's job (direnv / `.envrc` / a wrapper). |

The bearer is the credential; the other two are **narrowing factors**. The
server requires the bearer plus **at least one** of them, and intersects both
when both are sent (effective permission is the min — a project key can only
narrow, never widen).

Resolution order per request:

1. `SPRAWL_TOKEN` env → `config.toml` `token`. Missing → "not logged in, run `sprawl login`".
2. `--agent-secret` flag → `SPRAWL_AGENT_SECRET` env.
3. `--project-key` flag → `SPRAWL_PROJECT_KEY` env (trimmed; the server case-folds).
4. Neither 2 nor 3 → **fail before the HTTP call**, naming both. The server's
   `403 second_factor_required` can't say which one the user meant, so we don't
   let it get that far.

Every `/api/v1/*` call sends `Authorization: Bearer <token>` plus whichever of
`X-Agent-Secret: <secret>` / `X-Project-Key: <key>` is configured.

Under a project key the server pre-filters reads to that project, lands creates
in it without a `project_id`, and 403s anything outside it. Two consequences in
the CLI: `task create --project-id` is optional (and only honoured when it names
the confined project), and **`theme set` deliberately drops the project key and
requires an agent secret** — a theme is user-level, and the server rejects any
theme write that carries a project key.

## Invariants (don't break these)

1. Every structured-output subcommand honours `--format=text|json` (persistent flag on root). Default is `json`; session-wide override via `SPRAWL_OUTPUT`. `-h` / `--human` is a shorthand for `--format=text` (an explicit `--format` wins). Login is interactive and stays plain text regardless. **Styling is text-only:** human (`text`) output is color-styled with lipgloss using the terminal's own ANSI palette, and only when stdout is a real TTY. `json` is a machine format and is never styled; piped / redirected / `$NO_COLOR` output degrades to plain text identical to the unstyled rendering.
2. No command writes `agent_secret` or `project_key` to any file, log, or flag default.
3. No command prints the `token` or `agent_secret` to stdout / stderr. (The project key is *not* a credential and may be shown — `whoami` and the TUI header do.)
4. URL is never read from config; only baked-in or `SPRAWL_API_URL` env override.
5. Two binaries share 100 % of the code; divergence happens only via `internal/build` vars.
6. Missing narrowing factor fails locally, never at the server: no request goes out without an agent secret or a project key.

## Repo layout

```
cmd/sprawl/          main.go — thin entry point
internal/build/      ldflag-injected vars
internal/cli/        cobra root + subcommands
internal/client/     stdlib net/http client
internal/config/     XDG-aware config.toml Load/Save
internal/updater/    `sprawl update` + once-per-day version notice
skills/sprawl/       agent-skills bundle (installed via `gh skill install`)
agents/              sprawl-bookkeeper sub-agent (one file per host; manual install)
docs/                feature-level documentation (start at docs/INDEX.md)
Makefile             build / build-dev / build-all / run-dev / test / clean
.goreleaser.yaml     release config (stub)
mise.toml            Go version pin
```

## Common commands

```sh
make build-dev          # dist/sprawl_dev, localhost:4201 baked in (override: PORT=…)
make build              # dist/sprawl, prod URL baked in
make build-all          # both
make run-dev ARGS="version"
make check              # fmt-check + vet + test. Run before declaring a task done.
make test-race          # tests + race detector (slower; run before releases)
make tidy fmt vet
```

Change the prod URL at build time without editing the Makefile: `make build PROD_URL=https://staging.example.com`.

## Interactive prompts

Command output stays non-interactive apart from `sprawl login`, which uses
plain `bufio.Scanner`-style line prompts (see `internal/updater/github.go`
`confirm` for the pattern).

The interactive terminal UI lives in `internal/tui` (built on
`charm.land/bubbletea/v2`). It launches on a bare `sprawl` when stdin+stdout are
a TTY, and via the explicit `sprawl tui` subcommand (`sprawl tui` on a non-TTY
errors clearly; a bare non-TTY `sprawl` still prints help). The TUI reuses
`internal/client` and the `internal/cli` credential resolvers; its colors come
exclusively from the terminal's ANSI palette (indices 0–15), same as the CLI's
text styling. The agent-secret prompt is masked and kept in memory only — never
written to disk, logged, or echoed (invariants #2/#3). Clipboard copy uses
OSC 52 (`tea.SetClipboard`) and `$EDITOR` editing uses `tea.ExecProcess`, so the
single static, cgo-free binary promise is preserved.

## Collaboration rules

Do not write co authoted by claude or anything like that if you commit

**Test before claiming done.** Every code change must pass `make check` (fmt-check + vet + test) before it's reported as complete. If `check` fails, the change isn't finished — fix it, don't wave it off. Never bypass hooks with `--no-verify` unless the user explicitly says so. Tests are mocked with `httptest`; no running backend is required.

When documenting a new feature or updating behaviour, use the `project-documentation` skill to add or update files under `docs/features/`. Keep `CLAUDE.md` lean — per-command detail belongs in the feature docs.
