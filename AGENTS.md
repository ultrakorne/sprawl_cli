# sprawl — CLI for the sprawl API

HTTP client. Single static Go binary (two variants from one codebase), JSON on the wire, TOON / JSON / text on stdout. Used by the human owner and by AI agents. The server is live; the CLI wraps its endpoints.

## Documentation

Feature-level docs live under [`docs/`](docs/INDEX.md) and are discovered incrementally — start at `docs/INDEX.md`, then open the feature folder that matches the task (auth-and-config, output-formats, tasks, checklists, theme, whoami).

## Stack

- **CLI framework**: `github.com/spf13/cobra`
- **HTTP**: stdlib `net/http` + `encoding/json` — no extra client deps
- **Config**: TOML via `github.com/BurntSushi/toml`
- **Output**: TOON via `github.com/alpkeskin/gotoon` (default), JSON, plain text fallback
- **Interactive TUI**: `charm.land/bubbletea/v2` — see [Interactive prompts](#interactive-prompts) for when to reach for it
- **Releases**: `goreleaser`

## Two-binary build pattern (critical)

One codebase produces two binaries. The *only* difference is linker-injected values in `internal/build`:

| Binary | `APIURL` | `AppName` | Config dir |
|---|---|---|---|
| `sprawl` (prod) | `https://sprawl.today` | `sprawl` | `~/.config/sprawl/` |
| `sprawl_dev` | `http://localhost:4000` | `sprawl_dev` | `~/.config/sprawl_dev/` |

- **The API URL is never in config.** When prod moves, ship a new release. Do not add a `url` field or a `--url` flag.
- **No `--profile` / `--env` flag.** Binary choice is the environment switch.
- **One-off override**: `SPRAWL_API_URL=…` env var only. Never persists.

## Credential model (do not regress)

| Credential | Storage |
|---|---|
| `token` (device-flow result) | Config file `config.toml`, mode **0600**. |
| `agent_secret` | `SPRAWL_AGENT_SECRET` env var or `--agent-secret` / `-s` flag. **Never persisted to disk by sprawl.** |

Resolution order per request:
1. `SPRAWL_TOKEN` env → `config.toml` `token`. Missing → "not logged in, run `sprawl login`".
2. `--agent-secret` flag → `SPRAWL_AGENT_SECRET` env. Missing → **fail before the HTTP call**.

Every `/api/v1/*` call sends `Authorization: Bearer <token>` + `X-Agent-Secret: <secret>`.

## Invariants (don't break these)

1. Every structured-output subcommand honours `--format=text|json|toon` (persistent flag on root). Default is `toon`; session-wide override via `SPRAWL_OUTPUT`. Login is interactive and stays plain text regardless.
2. No command writes `agent_secret` to any file, log, or flag default.
3. No command prints the `token` or `agent_secret` to stdout / stderr.
4. URL is never read from config; only baked-in or `SPRAWL_API_URL` env override.
5. Two binaries share 100 % of the code; divergence happens only via `internal/build` vars.

## Repo layout

```
cmd/sprawl/          main.go — thin entry point
internal/build/      ldflag-injected vars
internal/cli/        cobra root + subcommands
internal/client/     stdlib net/http client
internal/config/     XDG-aware config.toml Load/Save
docs/                feature-level documentation (start at docs/INDEX.md)
Makefile             build / build-dev / build-all / run-dev / test / clean
.goreleaser.yaml     release config (stub)
mise.toml            Go version pin
```

## Common commands

```sh
make build-dev          # dist/sprawl_dev, localhost:4000 baked in
make build              # dist/sprawl, prod URL baked in
make build-all          # both
make run-dev ARGS="version"
make check              # fmt-check + vet + test. Run before declaring a task done.
make test-race          # tests + race detector (slower; run before releases)
make tidy fmt vet
```

Change the prod URL at build time without editing the Makefile: `make build PROD_URL=https://staging.example.com`.

## Interactive prompts

`charm.land/bubbletea/v2` is the chosen TUI lib. We already use it for the
three-stage selection in `sprawl skill install` (see
`internal/skill/prompt.go`); reach for it whenever a command needs more
than a single line of input.

**When to use bubbletea:**
- Multi-select / single-select pickers (checkboxes, radio lists).
- Forms with several fields where blank-line / comma-separated parsing
  would feel awkward.
- Anything that benefits from arrow-key navigation, live preview, or a
  spinner during async work.

**When NOT to use it:** a single y/N prompt (`scanner.Scan` is fine — see
`internal/updater/github.go` `confirm`), or non-interactive output (the
update banner, the login URL print). Mixing bubbletea programs with
line-by-line `fmt.Fprintln` output in the same flow gets ugly fast.

**Pattern to follow:**
1. Define a `tea.Model` with `Init() Cmd`, `Update(Msg) (Model, Cmd)`, and
   `View() View`. Use `tea.NewView(string)` to wrap rendered text.
2. Match key presses by string in `Update`:
   `if k, ok := msg.(tea.KeyPressMsg); ok { switch k.String() { ... } }`.
   Common keys: `"enter"`, `"esc"`, `"ctrl+c"`, `"up"`/`"k"`,
   `"down"`/`"j"`, `"space"` (also matches `" "`).
3. Wrap the program runner in a thin function (`runMultiSelect`, etc.)
   and expose the call site as a swappable `var fooFunc = runFoo` so
   tests can stub it without driving a fake TTY. See `prompt.go` for the
   shape — `promptChoiceFunc` and `promptConfirmFunc` are the seams.
4. Cancellation (`esc` / `ctrl+c`) returns `errPromptCancelled`; call
   sites should `errors.Is` and translate it into a friendly "Cancelled."
   message, not a stack-trace error.
5. Test models directly via `model.Update(msg)` — there's a small `key`
   helper in `prompt_test.go` for synthesising `tea.KeyPressMsg`. Don't
   spawn `tea.NewProgram` in tests.

The dependency adds ~7 MB to the binary, mostly transitive
(`charmbracelet/ultraviolet` + `x/ansi`). If you're tempted to add it for
a single-line prompt, default to a plain `bufio.Scanner` instead.

## Collaboration rules

do not commit to git

**Test before claiming done.** Every code change must pass `make check` (fmt-check + vet + test) before it's reported as complete. If `check` fails, the change isn't finished — fix it, don't wave it off. Never bypass hooks with `--no-verify` unless the user explicitly says so. Tests are mocked with `httptest`; no running backend is required.

When documenting a new feature or updating behaviour, use the `project-documentation` skill to add or update files under `docs/features/`. Keep `CLAUDE.md` lean — per-command detail belongs in the feature docs.
