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

`login` prints the settings URL (`<api-url>/auth-settings`) first — that's where you copy your owner agent secret. Then it starts the device grant: open the verification link, approve in the browser, and the token lands in `~/.config/sprawl_dev/config.toml` at mode 0600. The agent secret is **not** stored there — you supply it per-shell via `SPRAWL_AGENT_SECRET` (or per-command via `-s` / `--agent-secret`).

```sh
export SPRAWL_AGENT_SECRET=<your agent secret>
sprawl_dev whoami            # prints your agent name + elevated project permissions
```

Prod works identically, just with the `sprawl` binary and `~/.config/sprawl/`.

## Commands

| Command | What it does |
|---|---|
| `sprawl version` | Prints the version and the baked-in API URL. |
| `sprawl login` | Runs the RFC 8628 device flow and saves the resulting token. |
| `sprawl whoami` | Calls `GET /api/v1/whoami` to identify the calling agent and list any project-scoped permissions that elevate the default. Doubles as an auth-pipeline check. |
| `sprawl theme get` | Fetches the currently active UI theme id (e.g. `tokyo-night`). |
| `sprawl theme set <id>` | Sets the active theme by id. Ids are lowercase kebab-case (`tokyo-night`, `catppuccin-latte`, `gruvbox`); the server does no normalization, so an unknown id → 404. Owner-only. |
| `sprawl task list` | Lists every task the caller can read. Non-owner agents see only tasks their key resolves `:read` / `:write` / `:write_create` on. |
| `sprawl task show <id>` | Fetches a single task by id. Returns 404 when the id isn't visible, 403 when the permission resolver says no. |
| `sprawl task search <query>` | Substring search on task title (case-insensitive, server-side). Empty query → 422. |
| `sprawl task create` | Creates a task. Flags: `--title`, `--description`, `--project-id`, `--from-json <path\|->`. Requires `write_create` at the relevant scope — `default_permission` for project-less create, project-scope for project-bound create. |
| `sprawl task update <id>` | Updates a task's `title` / `description`. Flags: `--title`, `--description`, `--from-json <path\|->`. Passing `--description ""` clears the field explicitly. |
| `sprawl checklist <task_id>` | Lists checklist items for a task. Ownership and permission are both checked on the parent task. |
| `sprawl checklist add <task_id>` | Adds an item. Flags: `--title`, `--notes`, `--from-json <path\|->`. Server assigns position (appended). |
| `sprawl checklist check <item_id>` | Marks the item completed (`{"completed": true}`). Idempotent — no-op on an already-completed item. |
| `sprawl checklist uncheck <item_id>` | Marks the item not completed (`{"completed": false}`). Idempotent — no-op on an already-uncompleted item. |
| `sprawl checklist update <item_id>` | Updates an item's `title` / `notes`. Flags: `--title`, `--notes`, `--from-json <path\|->`. Use `check` / `uncheck` for completion. |
| `sprawl note show <item_id>` | Prints the raw notes blob for a checklist item. Empty string is a legitimate success. |
| `sprawl note set <item_id> [<notes>]` | Replaces the notes blob. Pass the text as a positional arg or via `--stdin` (mutually exclusive). Empty string clears notes. |

All commands honour the `--format` flag. In `text` mode, list commands render tabwriter-aligned tables and write commands render a compact summary line; in `json` / `toon` mode they return the server envelope unchanged (`{tasks:[…]}`, `{task:{…}}`, `{checklist_items:[…]}`, `{checklist_item:{…}}`, `{notes:"…"}`).

## Write command examples

Every write command takes the same two input paths: explicit flags, or the full attrs object via `--from-json <path|->`. When both are used, explicit flags override fields parsed from the JSON source — pipe a template, tweak one field on the command line.

### `task create`

Wire body is `{"task": {"title": "...", "description": "...", "project_id": N}}`. `project_id` is a field of the inner object; the CLI offers `--project-id` as a convenience (integer-parsed locally so bad input fails before the HTTP call).

```sh
# Flags only, no project — requires default_permission = write_create.
sprawl task create --title "draft spec" --description "outline the v2 API"

# Flags only, attached to a project.
sprawl task create --title "wire up CI" --project-id 42

# Full attrs object on stdin.
echo '{"title":"triage","description":"go through the backlog","project_id":42}' \
  | sprawl task create --from-json -

# Template on stdin, title overridden on the CLI.
echo '{"title":"draft","project_id":42}' \
  | sprawl task create --from-json - --title "final"

# Template from a file.
sprawl task create --from-json ./task.json
```

### `task update <id>`

Accepts `title` / `description`. `project_id` is intentionally not wired here — the server's update changeset ignores it, so surfacing it would mislead.

```sh
sprawl task update 17 --title "renamed"
sprawl task update 17 --description ""          # clears the field (distinct from "flag unset")
echo '{"description":"rewritten"}' | sprawl task update 17 --from-json -
```

### `checklist add <task_id>`

Wire body is `{"checklist_item": {"title": "...", "notes": "..."}}`. Server assigns position (appended).

```sh
sprawl checklist add 17 --title "write migration"
sprawl checklist add 17 --title "deploy" --notes "run after backfill"
echo '{"title":"smoke test","notes":"hit /whoami"}' | sprawl checklist add 17 --from-json -
```

### `checklist update <item_id>` / `check` / `uncheck`

```sh
sprawl checklist update 203 --title "renamed"
sprawl checklist check 203                     # idempotent
sprawl checklist uncheck 203                   # idempotent
```

### `note set <item_id>`

```sh
sprawl note set 203 "blocked on PR #418"
sprawl note set 203 ""                          # clears notes
cat long-notes.md | sprawl note set 203 --stdin
```

### `theme set <id>`

```sh
sprawl theme set tokyo-night                    # owner-only; unknown id → 404
```

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
