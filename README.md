# Sprawl CLI

CLI for Sprawl <https://sprawl.today>

## Install

One-liner that grabs the latest release, verifies its checksum, and drops the binary into `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/ultrakorne/sprawl_cli/master/scripts/install.sh | bash
```

Pin a specific release or pick a different install dir via env vars:

```sh
curl -fsSL https://raw.githubusercontent.com/ultrakorne/sprawl_cli/master/scripts/install.sh \
  | SPRAWL_VERSION=v0.2.0 BIN_DIR=/usr/local/bin bash
```

(For a system-wide path like `/usr/local/bin`, prefix the command with `sudo` and pass `-E` so the env vars survive: `... | sudo -E env BIN_DIR=/usr/local/bin bash`.)

After the first install, future upgrades are in-place: `sprawl update` downloads the latest release, verifies SHA256, and atomically replaces the running binary.

Supported targets: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64. The script refuses to run without a SHA-256 verifier (`sha256sum` or `shasum`).

## Build from source

### Requirements

- Go **1.26.2** or newer. If you use [mise](https://mise.jdx.dev/), `mise install` inside the repo picks up the version pinned in `mise.toml`.
- For `sprawl_dev`: a running sprawl backend on `http://localhost:4201` (build with `PORT=…` for another port).

### Build

Two binaries ship from this codebase. The only difference between them is the API URL and config directory baked in at link time.

```sh
make build-dev            # → dist/sprawl_dev    (targets http://localhost:4201)
make build-dev PORT=4300  # when your backend is on another port
make build                # → dist/sprawl        (targets the prod URL)
make build-all            # both
```

Override the prod URL at build time without editing the Makefile:

```sh
make build PROD_URL=https://staging.example.com
```

Other useful targets: `make check`, `make test`, `make fmt`, `make vet`, `make tidy`, `make clean`. See [Testing](#testing) for the day-to-day loop.

### Install from source

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

`login` prints the settings URL (`<api-url>/auth-settings`) first — that's where you copy your owner agent secret. Then it starts the device grant: open the verification link, approve in the browser, and the token lands in `~/.config/sprawl_dev/config.toml` at mode 0600.

The token alone isn't enough to make a call. Every `/api/v1/*` request needs the bearer **plus at least one narrowing factor**, and neither is stored on disk:

```sh
export SPRAWL_PROJECT_KEY=acme                  # confine this shell to one project
# and/or
export SPRAWL_AGENT_SECRET=<your agent secret>  # act as that agent key

sprawl_dev whoami            # prints your agent, the project you're confined to, and your scope
```

Set neither and the CLI stops before the request with a message naming both.

### Which one do I want?

- **Project key** — the key shown in the project's side panel on `/tasks` (2–32 chars, e.g. `acme`). Put it in a repo's `.envrc` (direnv) or a wrapper script and that repo can only ever read and write that project: lists come back pre-filtered, `task create` needs no `--project-id`, everything else is a 403. It's a blast-radius control, not a secret — `whoami` and the TUI header show it.
- **Agent secret** — the 8-character value from `/auth-settings`. Use it when the client needs its **own identity**: MCP clients, or anything whose edits should carry a distinct attribution emoji. A project-key call with an owner token is attributed to *you*, exactly like a browser edit.
- **Both** — the server intersects them (effective permission is the minimum). A read-only agent key plus a project key gets read on that one project and nothing else.

Prod works identically, just with the `sprawl` binary and `~/.config/sprawl/`.

## Commands

| Command | What it does |
|---|---|
| `sprawl version` | Prints the version and the API URL in effect. |
| `sprawl login` | Runs the RFC 8628 device flow and saves the resulting token. |
| `sprawl whoami` | Calls `GET /api/v1/whoami` to identify the calling agent, name the project you're confined to (when a project key is set) and the level you resolve to there, and list any project-scoped permissions that elevate the default. Doubles as an auth-pipeline check. |
| `sprawl theme get` | Fetches the currently active UI theme id (e.g. `tokyo-night`). |
| `sprawl theme set <id>` | Sets the active theme by id. Ids are lowercase kebab-case (`tokyo-night`, `catppuccin-latte`, `gruvbox`); the server does no normalization, so an unknown id → 404. Owner-only, and the one command a project key can't authorize — a theme is user-level, so this always needs an agent secret. |
| `sprawl task list` | Lists every task the caller can read. Non-owner agents see only tasks their key resolves `:read` / `:write` / `:write_create` on. |
| `sprawl task <id>` | Fetches a task and its items: a title/description header, then the item table. Returns 404 when the id doesn't exist, 403 when the permission resolver says no. Add `--full` to pull each item's note body and expand it under its row. |
| `sprawl task search <query>` | Substring search on task **and item** titles (case-insensitive, server-side; notes are not searched). A lookup rather than a view: each hit renders as the task's title plus the ids and titles of the items that matched, which you then read with `item <id>` or `task <id>`. Empty query → 422. |
| `sprawl task create` | Creates a task. Flags: `--title`, `--description`, `--project-id`, `--from-json <path\|->`. Requires `write_create` at the relevant scope — `default_permission` for project-less create, project-scope for project-bound create. Under a project key the task lands in that project and `--project-id` is unnecessary. |
| `sprawl task update <id>` | Updates a task's `title` / `description`. Flags: `--title`, `--description`, `--from-json <path\|->`. Passing `--description ""` clears the field explicitly. |
| `sprawl task delete <id>` | Soft-deletes a task. Server reflows neighbor cards on the canvas. A 404 (already deleted or never visible) is treated as success — the CLI is idempotent. There is no API to restore a soft-deleted task. |
| `sprawl item <id>` | Fetches one item with its note — the detail view, so the note is always included and there is no `--full`. Human output is a single table row; `json` additionally carries a `task` stub (id, title, project). |
| `sprawl item add <task_id>` | Adds an item to a task. Flags: `--title`, `--notes`, `--from-json <path\|->`. Server assigns position (appended). Note the argument is a **task** id — the one item verb that takes one. |
| `sprawl item update <id>` | Updates an item's `title` and/or its note — the only way to write a note. Flags: `--title`, `--notes`, `--from-json <path\|->`. `--notes -` reads the body from stdin; `--notes ""` clears it. Use `check` / `uncheck` for completion. |
| `sprawl item check <id>` | Marks the item completed (`{"completed": true}`). Idempotent — no-op on an already-completed item. |
| `sprawl item uncheck <id>` | Marks the item not completed (`{"completed": false}`). Idempotent — no-op on an already-uncompleted item. |
| `sprawl item state <id> <ready\|progress\|review\|none>` | Sets the item's hand-set state, or clears it with `none`. These short names are the vocabulary — they're what reads emit too. States are mutually exclusive, and setting one **un-completes** the item. Separate route from `item update`, which ignores the field. |
| `sprawl item pr <id> <number\|none>` | Attaches a GitHub PR number to the item, or clears it. Independent of state and completion — only an explicit `none` removes it. The link is built client-side from the project's repo URL; without one the number renders bare. |
| `sprawl item delete <id>` | Hard-deletes an item. May flip the parent task's `completed_at`. A 404 is idempotent success like `task delete`; a **403** (the item exists but is outside your project key) is a real error. |
| `sprawl queue` | Lists items in a given state across every visible task — the "what can I pick up?" query, and the only item view that crosses tasks. Defaults to `--state ready`; also accepts `progress` / `review`. Every result is incomplete by construction (completing an item clears its state). `--full` expands each note. |
| `sprawl update` | Downloads the latest GitHub release, verifies SHA256, and atomically replaces the running binary. Refuses on `sprawl_dev` and on local builds. Pass `--yes` to skip the confirmation prompt. See [features/auto-update](docs/features/auto-update/INDEX.md). |

## AI-tool integration

The repo ships a sprawl **skill** (under `skills/sprawl/`) for the AI tools that follow the [agentskills.io](https://agentskills.io/specification) convention, and a `sprawl-bookkeeper` **sub-agent** (under `agents/`) in three host-specific flavours.

Install the skill with `gh` (works for Claude Code, OpenCode, Codex, Cursor, and most other supported hosts — see `gh skill install --help` for the full list):

```sh
# Project scope (default): drops it under the current repo
gh skill install ultrakorne/sprawl_cli sprawl

# User scope: available everywhere
gh skill install ultrakorne/sprawl_cli sprawl --scope user --agent claude-code
```

The sub-agent (`sprawl-bookkeeper`) is not a `gh skill` artefact, so install it by hand. See [`agents/README.md`](agents/README.md) for the per-host paths; the short version for Claude Code is:

```sh
mkdir -p ~/.claude/agents
curl -fsSL https://raw.githubusercontent.com/ultrakorne/sprawl_cli/master/agents/claude/sprawl-bookkeeper.md \
  -o ~/.claude/agents/sprawl-bookkeeper.md
```

All commands honour the `--format` flag (and `-h` / `--human`, a shorthand for `--format=text`). In `text` mode, read commands render aligned tables and write commands render a one-line `✓ …` summary; in `json` mode they return the server envelope (`{tasks:[…]}`, `{task:{…}}`, `{checklist_items:[…]}`, `{checklist_item:{…}}`) with the item shape **normalized** — the CLI does not pass items through untouched:

- **`state` is short-form on output** — `ready` / `progress` / `review` / `null`, never the server's `ready_to_pickup` / `in_progress` / `in_review`. Input still accepts both. This is what keeps an item's state from colliding with a task's `status`, which really is `in_progress`.
- **`pr_url` is added** — the assembled GitHub link, or `null` when the chain can't resolve (no PR number, no project, or no repo URL on it). None of those is an error.
- **`position` is dropped.** Array order already carries ordering; the server returns items in position order.
- **`notes` rides only where the bodies were fetched** — `item <id>` always, `task <id>` / `queue` only under `--full`. `has_notes` is always present.

The two delete commands return `{id:"…", deleted:true, existed:…}` instead — the server replies 204 with no body, but the CLI emits a small payload so json consumers always see structured output.

Text (`-h`) output is color-styled with [lipgloss](https://charm.land/lipgloss) using your terminal's own ANSI palette — cyan table headers with a `─` rule beneath them, traffic-light checklist progress (`0/x` red, in-progress yellow, `x/x` green), colored checkboxes, and item-state glyphs. PR numbers are OSC 8 hyperlinks where the project has a repo URL, so a ctrl/cmd-click opens the pull request. Styling is **text-only** and applied **only when writing to a terminal**: `json`, pipes, files, and `$NO_COLOR` all stay plain — including the hyperlinks.

The state glyphs are Nerd Font icons by default, matching the TUI and the web app's state pills. Set `SPRAWL_ICONS=plain` if your terminal font renders them as tofu; you get one-cell geometric shapes instead.

**`--full` gates the fetch *and* the render.** `task <id>` and `queue` always show a `NOTES` column (`o` / `-`, driven by `has_notes`), because "is there a note here?" is the cheap question. `--full` sends `?full=true`, which pulls every note **body** over the wire and expands each one under its row. Reach for it when you want to read the notes; leave it off when you only want to know they exist. `item <id>` has no `--full` — it is the detail view, so its note is always there.

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

### `item add <task_id>`

Wire body is `{"checklist_item": {"title": "...", "notes": "..."}}`. Server assigns position (appended).

```sh
sprawl item add 17 --title "write migration"
sprawl item add 17 --title "deploy" --notes "run after backfill"
echo '{"title":"smoke test","notes":"hit /whoami"}' | sprawl item add 17 --from-json -
```

### `item update <id>` / `check` / `uncheck`

```sh
sprawl item update 203 --title "renamed"
sprawl item check 203                          # idempotent
sprawl item uncheck 203                        # idempotent
```

### `item state <id>` / `pr` / `queue`

```sh
sprawl queue                                   # what's ready to pick up, across tasks
sprawl item state 203 progress                 # claim it
sprawl item pr    203 412                      # link the PR
sprawl item state 203 review                   # hand it back for review
sprawl item state 203 none                     # clear the state (PR number survives)
sprawl item pr    203 none                     # clear the PR number
sprawl queue --state review                    # what's waiting on a human
```

Setting a state on a **completed** item un-completes it — the two are mutually
exclusive server-side. Conversely, `item check` clears the state and keeps
the PR number.

### Writing a note

A note is a field of an item, not an object of its own, so `item update` writes it —
there is no `note` command.

```sh
sprawl item update 203 --notes "blocked on PR #418"
sprawl item update 203 --notes ""               # clears the note
cat long-notes.md | sprawl item update 203 --notes -
```

To read one back as raw text — piping into `less`, `rg`, a file — go through json:

```sh
sprawl item 203 --format=json | jq -r '.checklist_item.notes'
```

### `theme set <id>`

```sh
sprawl theme set tokyo-night                    # owner-only; unknown id → 404
```

## Flags and environment variables

Persistent flags (work on every command):

| Flag | Description |
|---|---|
| `--format text\|json` | Output format. Default is `json`. |
| `-h`, `--human` | Shorthand for `--format=text` (an explicit `--format` wins). `--help` still shows help. |
| `-s`, `--agent-secret <value>` | Agent secret for `/api/v1/*` calls. Overrides `$SPRAWL_AGENT_SECRET`. |
| `-p`, `--project-key <key>` | Confine the call to one project by its key. Overrides `$SPRAWL_PROJECT_KEY`. |

Environment variables:

| Variable | Purpose |
|---|---|
| `SPRAWL_AGENT_SECRET` | Agent secret used if `-s` is not passed. |
| `SPRAWL_PROJECT_KEY` | Project key used if `-p` is not passed. Confines every call to that project. At least one of this and `SPRAWL_AGENT_SECRET` must be set. |
| `SPRAWL_TOKEN` | Bearer token override. If unset, the token comes from `config.toml`. |
| `SPRAWL_OUTPUT` | Session-wide default for `--format` (`text` or `json`). |
| `SPRAWL_API_URL` | One-off API URL override. Use sparingly — the binary is the environment switch. |
| `SPRAWL_ICONS` | Set to `plain` when your terminal font has no [Nerd Font](https://www.nerdfonts.com/) glyphs, so the item-state icons render as geometric shapes (`▸ ◐ ◉`) instead of tofu boxes. Applies to the `STATE` column in `-h` output and to the TUI alike — they share one icon set. Default is the Nerd Font Material icons. |
| `SPRAWL_NO_UPDATE_CHECK` | Set to `1` to suppress the once-per-day "newer version available" notice on the prod `sprawl` binary. The notice is otherwise on stderr only and never blocks. |

Why JSON by default? The CLI's output is mostly consumed by LLMs and scripts, and JSON is the shape they can parse without a second thought — it pipes straight into `jq`. It is the server's envelope with the small, documented item normalization described above, not a separate format to learn. Pass `-h` (or `--format=text`) for human-friendly, color-styled output.

## Config file

Per binary, an XDG-aware TOML file:

- `sprawl`:     `~/.config/sprawl/config.toml`
- `sprawl_dev`: `~/.config/sprawl_dev/config.toml`

The only field currently stored is `token`. Neither the agent secret nor the project key is ever written here — both come from the environment (or a flag) at invocation time. File mode is `0600`; directory mode `0700`. Atomic writes mean an interrupted `login` won't truncate an existing file.

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

If you installed the sprawl skill with `gh skill install`, remove it the same
way (`gh skill list` / a manual `rm` of the host directory). The
`sprawl-bookkeeper` agent file (`~/.claude/agents/sprawl-bookkeeper.md` or the
equivalent for OpenCode / Codex) is also a plain delete.
