# sprawl — Setup

Read this only when the preflight in `SKILL.md` fails: `sprawl` isn't on PATH,
the user hasn't logged in, or they don't have an agent secret yet. Once the
user is past these steps, this file is not needed again.

The whole flow is: **install the binary → `sprawl login` → grab the agent
secret from the settings page → export it**. Device-flow login opens a
browser, so the user must do it — you can't complete it for them.

## 1. Install `sprawl`

Check first — it may already be installed:

```bash
command -v sprawl && sprawl version
```

If missing, the user picks one of these (ask them, don't guess):

### a) Build from source (this repo)

Requires Go 1.26.2+ (pinned in `mise.toml`). From the repo root:

```bash
make build                                   # produces dist/sprawl
sudo install -m 0755 dist/sprawl /usr/local/bin/sprawl
# or user-local:
install -m 0755 dist/sprawl ~/.local/bin/sprawl     # ensure ~/.local/bin is on PATH
```

### b) Release binary

If the project has a `goreleaser` release, download the archive for the host
OS/arch, extract, and place the `sprawl` binary somewhere on `PATH` with mode
0755.

### c) mise users

```bash
mise install                                 # picks up the Go version pin
```

…then build as above.

Verify:

```bash
sprawl version
# prints the version and the API URL baked into the binary
```

## 2. Log in (device flow — user does this)

This is **interactive**. It prints a URL and a user code, the user opens the
URL in a browser, approves, and the CLI polls until the token lands. Agents
can't complete this step; ask the user to run it themselves.

```bash
sprawl login
```

On success:

- A token is written to `~/.config/sprawl/config.toml` (mode 0600).
- The CLI prints the settings URL (`<api-url>/settings`) — **keep this
  terminal open**, the user needs that URL for the next step.

If the user cancels (Ctrl+C) or the device code expires, have them re-run
`sprawl login`.

## 3. Get an agent secret

Agent secrets scope per-agent permissions server-side. They are issued by the
owner on the sprawl settings page (`<api-url>/settings`), which `sprawl login`
printed in the step above.

Direct the user to:

1. Open the settings URL in a browser.
2. Create (or copy) an agent secret for **this** agent. It's the thing the
   server uses to decide what the caller can see and change.
3. Copy the secret value. It will only be shown once on creation — the user
   should treat it like a password.

## 4. Export the secret (user does this too)

The CLI reads the secret from `SPRAWL_AGENT_SECRET` or a `-s` flag. The env
var is preferred for long-lived shells — the flag form leaks via `ps auxe`
and shell history.

```bash
export SPRAWL_AGENT_SECRET=<paste secret here>     # this shell only
```

To persist across sessions, the user can add the export to their shell rc
(`~/.bashrc`, `~/.zshrc`), but that's their call — **sprawl itself never
writes the secret to disk**, and you shouldn't either. Don't offer to edit
their rc files without explicit instruction.

## 5. Verify end-to-end

With the binary installed, login complete, and `SPRAWL_AGENT_SECRET`
exported:

```bash
sprawl health --format=json
# {"status":"ok"}
```

A non-ok response here tells you what's still missing:

| Error | Fix |
|---|---|
| `command not found: sprawl` | Step 1 wasn't done, or binary isn't on PATH. |
| `"not logged in, run sprawl login"` | Step 2: the token is absent or stale. |
| `http_status: 401` | Token is wrong / expired — re-run `sprawl login`. |
| `http_status: 403` | Login is fine but the secret has no scope on `/health` — unlikely unless the secret is wrong. |
| Pre-flight "missing agent secret" | Step 4: env var isn't exported in this shell. |

Once `health` returns ok, return to `SKILL.md` and proceed with the task the
user actually asked about.

## Config file (reference only)

- Path: `~/.config/sprawl/config.toml` (or `$XDG_CONFIG_HOME/sprawl/`).
- Mode: 0600 (directory 0700).
- Only field currently stored: `token`. Do **not** add other fields — the API
  URL is baked into the binary on purpose, so the config stays minimal and
  portable between releases.

## Uninstall

```bash
rm -f /usr/local/bin/sprawl ~/.local/bin/sprawl
rm -rf ~/.config/sprawl
unset SPRAWL_AGENT_SECRET                    # in any shell where it's exported
```
