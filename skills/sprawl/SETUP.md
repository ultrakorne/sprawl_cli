# sprawl — Setup

Read this only when the preflight in `SKILL.md` fails: `sprawl` isn't on PATH,
the user hasn't logged in, or no project key is set. Once the user is past
these steps, this file is not needed again.

The whole flow is: **install the binary → `sprawl login` → copy the project
key from the project's side panel → export it**. Device-flow login opens a
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
- The CLI prints the URLs it uses — **keep this terminal open**, the user
  needs the base URL for the next step.

If the user cancels (Ctrl+C) or the device code expires, have them re-run
`sprawl login`.

## 3. Get the project key

The token alone can't make a call — it has to be paired with a **project
key**, which confines every call to one project. That's the whole scoping
model: this repo / shell talks to that project and nothing else.

Direct the user to:

1. Open `<api-url>/tasks` in a browser.
2. Select the project this repo should work in.
3. Copy the **Project key** field from the project's side panel (2–32
   characters, e.g. `hobby`). If it's empty, they can set one there.

The project key is not a secret — it's fine to print, to show in a command,
and to commit in an `.envrc`.

## 4. Export it (user does this too)

The CLI reads it from `SPRAWL_PROJECT_KEY`, or `-p <key>` per command:

```bash
export SPRAWL_PROJECT_KEY=hobby        # this shell only
```

It is never written to disk by sprawl. For a repo that should always be
pinned to one project, put it in the repo's `.envrc` (direnv) or a wrapper
script rather than typing it per command.

## 5. Verify end-to-end

With the binary installed, login complete, and `SPRAWL_PROJECT_KEY`
exported:

```bash
sprawl whoami --format=json
# {"status":"ok","agent":{"id":…,"name":"…","emoji":"…","is_owner":…,"default_permission":"…"},
#  "project":{"id":…,"name":"Hobby","key":"hobby","level":"write_create"},"project_permissions":[…]}
```

`project` names the project you're confined to and the level you resolve to
there (a current server also sends `workspace` — the canvas the key pins — and
`workspaces`; older servers send `project_permissions` instead). `"level":"none"` means the key is valid but this agent has no access to
that project — the user needs to grant it on the settings page. A `null`
`project` means no key reached the server: re-check step 4.

A non-ok response here tells you what's still missing:

| Error | Fix |
|---|---|
| `command not found: sprawl` | Step 1 wasn't done, or binary isn't on PATH. |
| `"not logged in, run sprawl login"` | Step 2: the token is absent or stale. |
| `http_status: 401` | Token is wrong / expired — re-run `sprawl login`. |
| `http_status: 403`, `invalid_project_key` | The project key is a typo — no project of theirs has it. Re-check the Project key field on the project's side panel. |
| `http_status: 403`, `forbidden` | The key is valid but the target is outside that project, or the agent has no access to it. Not a typo — don't retry. |
| `http_status: 403`, `workspace_mismatch` | A `SPRAWL_WORKSPACE` exported in this shell names a workspace other than the key's. `unset SPRAWL_WORKSPACE` — the key pins the workspace. |
| Pre-flight "no project key or agent secret set" | Step 4: `SPRAWL_PROJECT_KEY` isn't exported in this shell. |

Once `whoami` returns ok, return to `SKILL.md` and proceed with the task the
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
unset SPRAWL_PROJECT_KEY SPRAWL_WORKSPACE    # in any shell where they're exported
```
