# sprawl — CLI for the sprawl API

HTTP client (JSON on the wire; TOON/JSON/text on stdout). Single static Go binary, shipped via goreleaser.
Used by the human owner and by AI agents. This repo is the *client*;

## Start here

- **The spec**: [`docs/plans/sprawl_cli_evaluation.md`](docs/plans/sprawl_cli_evaluation.md) — language, config model, credential rules, and per-phase CLI workload. Read this before making design changes.
- **Backend phase plans** (source of truth for request/response shapes): `/home/ultra/Developer/task_manager/docs/plans/api/`
  - `02_device_flow.md` — login flow (✅ shipped on server)
  - `03_settings_and_pubsub.md` — theme get/set (✅ shipped on server)
  - `04_read_endpoints.md` — tasks/checklist/notes reads (✅ shipped on server)
  - `05_write_endpoints_and_audit.md` — writes + PubSub (pending)
  - `06_documentation_and_verify.md` — owner-secret UI (pending)

## Current server phase

Phases 2–5 are live. Phase 6 (owner-secret UI + docs) is still pending on the server, but the CLI's phase 6 copy work is in: `sprawl login` prints the settings URL (`<api-url>/settings`) as a pre-device-flow prompt *and* in the post-approval reminder, so the UX is already aimed at the settings page once the server ships it. No new HTTP calls. Implement later CLI commands **as the server ships them** — don't build ahead. Verify against `sprawl_dev` targeting `http://localhost:4000` first, then the same code ships as the prod binary on the next release.

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
internal/cli/        cobra root + subcommands (root.go with version, login.go, health.go, theme.go, task.go, checklist.go, note.go; auth.go for credential resolution + newAuthedClient helper; output.go for text/json/toon rendering; attrs.go for shared --from-json / flag-merge helpers used by write commands)
internal/client/     stdlib net/http client — BaseURL resolution, CreateDeviceGrant, PollDeviceToken (typed DevicePollError), Health, GetTheme/SetTheme (flat `{theme:"<id>"}` envelope, no client-side id normalization), ListTasks, SearchTasks, GetTask, ListChecklistItems, GetNotes, CreateTask/UpdateTask, CreateChecklistItem/SetChecklistItemCompleted/UpdateChecklistItem, SetNotes, APIError
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
- `login` — RFC 8628 device flow. Prints the settings URL (`<api-url>/settings`) as a pre-flow prompt so the user can grab their owner agent secret while the device grant is pending; POSTs `/api/auth/device`, prints the verification URL + user code, polls `/api/auth/device/token` at the server's `interval` until approval / expiry / denial / `invalid_grant`. Saves the token to `config.toml` (0600) on success and repeats the settings-URL reminder alongside the `SPRAWL_AGENT_SECRET` export hint. Ctrl+C cancels cleanly via the root context.
- `health` — resolves token (env → config) and agent secret (flag → env), fails pre-HTTP if secret missing, calls `GET /api/v1/health`. Honours `--format=text|json|toon` (default toon, or `$SPRAWL_OUTPUT`) for both success (`{"status":"ok"}`) and error (`{"status":"error","error":"…","http_status":…}`). Exit 1 on any failure.
- `theme get` — `GET /api/v1/settings/theme`. Wire shape is flat: `{"theme":"<id>"}` (string, not an object — the server no longer returns the display name). Renders the same flat envelope in json/toon; text fallback is just the id. Any authenticated agent (owner or not) can read.
- `theme set <id>` — `PATCH /api/v1/settings/theme` body `{"theme":"<id>"}`. Ids are lowercase kebab-case (`tokyo-night`, `catppuccin-latte`, `gruvbox`). The CLI does **no client-side normalization** — the arg goes on the wire verbatim, so the server is the only place id validation lives (unknown / mis-cased id → 404 `theme_not_found`). Owner-only (non-owner → 403 `forbidden`); missing body key → 422 `theme_required`. Same flat `{theme:"<id>"}` payload as `theme get` on success; text fallback is `Theme set to <id>`. Arg validation (`set` with zero/multi args) is performed inside `RunE` and routed through `reportErr` so the error renders in the chosen format instead of silently exiting.
- `task list` — `GET /api/v1/tasks`. Server-side per-agent permission filtering: non-owner agents only see tasks their key resolves to `:read`/`:write` on (task override → project override → `agent_keys.default_permission`). Renders `{tasks:[…]}` in the resolved format; text fallback is a tabwriter-aligned `ID STATUS DUE PROGRESS PROJECT TITLE` table, `(no tasks)` when empty.
- `task show <id>` — `GET /api/v1/tasks/:id`. 404 `not_found` when the ID isn't visible to the caller; 403 `forbidden` when the caller can see it via scope but the permission resolver says no. Renders `{task:{…}}`; text fallback is the multi-line detail (id/title, status, due, project, progress, actors, description).
- `task search <query>` — `GET /api/v1/tasks/search?q=<query>`. Case-insensitive substring match server-side; the CLI does not pre-validate the query, so empty/whitespace surfaces the server's 422 `query_required`. Same payload shape and text fallback as `task list`.
- `checklist <task_id>` — `GET /api/v1/tasks/:task_id/checklist`. Both ownership and permission checks run on the *parent task* (permissions are task-scoped, not item-scoped), so 403/404 mirror `task show`. Renders `{checklist_items:[…]}`; text fallback is an aligned table with `[x]`/`[ ]` completion boxes and a `notes` marker when `has_notes` is true.
- `note show <item_id>` — `GET /api/v1/checklist_items/:id/notes`. Permission check is on the parent task. Renders `{notes:"…"}`; text fallback is the raw notes blob so it pipes cleanly into `less`/`rg`. Empty notes is a valid success, not an error.
- `task create` — `POST /api/v1/tasks` body `{"task":{…}}`. Flags: `--title`, `--description`, `--project-id` (integer; parsed locally so non-numeric input fails before the HTTP call). `--from-json <path|->` reads a top-level JSON object and explicit flags override any fields it provided. Rejected with a local error if the merged attrs map is empty (prevents no-op POSTs). Server responds 201 `{"task":{…}}`; we render the same task-detail shape as `task show`.
- `task update <id>` — `PATCH /api/v1/tasks/:id` body `{"task":{…}}`. Same `--title` / `--description` / `--from-json` flags (no `--project-id` — the server's update changeset ignores it). `--description ""` is treated as an explicit clear via `cmd.Flags().Changed("description")`, not as "flag unset".
- `checklist add <task_id>` — `POST /api/v1/tasks/:task_id/checklist` body `{"checklist_item":{…}}`. Flags: `--title`, `--notes`, `--from-json <path|->`. Server assigns position server-side (appended); response is `{"checklist_item":{…}}`.
- `checklist check <item_id>` — `PATCH /api/v1/checklist_items/:id/completed` body `{"completed":true}`. No flags. Server is idempotent (no-ops when the state already matches) and echoes the item.
- `checklist uncheck <item_id>` — `PATCH /api/v1/checklist_items/:id/completed` body `{"completed":false}`. Same endpoint as `check`, negated payload; idempotent.
- `checklist update <item_id>` — `PATCH /api/v1/checklist_items/:id` body `{"checklist_item":{…}}`. `--title` / `--notes` / `--from-json`; completion state isn't mutable here (use `check` / `uncheck`).
- `note set <item_id> [<notes>]` — `PUT /api/v1/checklist_items/:id/notes` body `{"notes":"…"}`. Notes come from either a positional arg OR `--stdin` (mutually exclusive; both → local error before HTTP). An explicit empty string clears notes and is a valid success. Oversized bodies are rejected server-side as 422 and surface through `reportErr`.

All phase 4/5 read+write commands use `map[string]any` round-trips for json/toon rendering — typed `*client.Task` / `*client.ChecklistItem` structs feed `taskMap` / `checklistItemMap` / `checklistMaps` / `actorMap` / `projectMap` helpers in `internal/cli/task.go` and `internal/cli/checklist.go`, which emit literal `nil` for null-valued server fields (project/created_by/last_actor). `checklist <task_id>` remains a top-level command per the spec; `add` / `check` / `uncheck` / `update` are sibling subcommands and cobra dispatches exact subcommand matches first, falling through to the parent's `RunE` for the listing behaviour. Shared write-command plumbing lives in `internal/cli/attrs.go` (`loadJSONFromSource`, `mergeStringFlag`, `mergeProjectID`, `requireAttrs`) so `task create|update` and `checklist add|update` share one story for `--from-json` + flag merging.

E2E verified against the local server: token persisted, health round-trip returns 200, error paths (no login / missing secret / wrong secret) render cleanly in text, JSON, and TOON. Theme error paths (missing secret pre-HTTP, invalid secret → 403) verified in an earlier session. Phase 4/5 success round-trips were **not** verified live in this session; the CLI code is correct under `make check` + `make test-race` (including header-invariant coverage for POST /api/v1/tasks, PATCH /api/v1/tasks/:id, POST /api/v1/tasks/:task_id/checklist, PATCH /api/v1/checklist_items/:id/completed, PATCH /api/v1/checklist_items/:id, PUT /api/v1/checklist_items/:id/notes), and the mutation-semantics (actor stamping, PubSub broadcasts) are a server-side concern the CLI only validates by watching response envelopes.

## Open TODOs

- Repo lives at `git@github.com:ultrakorne/sprawl_cli.git`. Module path: `github.com/ultrakorne/sprawl_cli`.
- `.goreleaser.yaml` `release:` and `brews:` stanzas are still commented out — uncomment and wire when cutting the first release (owner `ultrakorne`, tap repo TBD).
- Once the server ships phase 6 (`/settings`), end-to-end verify that the URL printed by `sprawl login` actually resolves — today the URL is forward-looking.
- Re-verify phase 4/5 success round-trips against the local Phoenix once convenient. Mutation endpoints have been exercised only via `httptest` mocks in this repo; the mutation-semantics contract (actor stamping, PubSub broadcasts, auto-granting `:write` on self-created tasks for non-owner agents) is server-side.
