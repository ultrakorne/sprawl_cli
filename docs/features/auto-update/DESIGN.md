# Auto-update — Design

## Overview

Two paths, both gated to the prod `sprawl` binary:

1. **Passive notice** — at the start of every command (except `update`) the CLI checks once per 24 hours whether a newer release exists and, if so, prints a single yellow line on stderr.
2. **Explicit `sprawl update`** — fetches the latest release, verifies SHA256 against `checksums.txt`, and atomically swaps the running binary.

Both paths are no-ops on `sprawl_dev` (built from source against localhost) and on any non-release build of `sprawl` (e.g., `make build` from a non-tagged commit, where `git describe` produces `v0.1.0-1-gabc-dirty`). Releases are detected as a clean `vX.Y.Z` tag with no pre-release or build suffix.

## Notice

Banner format:

```
sprawl 0.2.0 available (current: 0.1.0). Run `sprawl update`.
```

- Written to **stderr** so `sprawl task list --format=json | jq` keeps a clean stdout.
- Yellow ANSI when stderr is a TTY; plain when not, or when `NO_COLOR` is set.
- Printed at most once per 24 hours per cache window. The cache lives at `<configDir>/update_check.json` and stores `{checked_at, latest_version}`.
- Network errors are silent. The cache is rewritten with `checked_at = now` even on failure, which naturally backs off for the next 24 hours and avoids spamming the user.
- The `update` subcommand is exempt — we don't want to print "update available" right before running update.

### Escape hatch

`SPRAWL_NO_UPDATE_CHECK=1` skips the check entirely. Useful for CI, tightly scripted automation, or any agent that wants determinism.

## `sprawl update`

```
sprawl update [--yes|-y]
```

Flow:

1. **Refuse on dev binary**: prints `sprawl_dev is built from source; use 'make build-dev'.` and exits 0.
2. **Refuse on local builds**: any non-release `Version` (`dev`, empty, or with a prerelease/build suffix) prints `local build (version=…); install a release before running update.` and exits 0.
3. **Fetch** `GET https://api.github.com/repos/ultrakorne/sprawl_cli/releases/latest` (no auth — repo is public).
4. **No-op when up-to-date**: prints `sprawl <ver> is already the latest.` and exits 0.
5. **Confirm** `Update sprawl 0.1.0 → 0.2.0? [y/N]` unless `--yes`. Anything other than `y`/`yes` cancels.
6. **Download** the platform-matching tarball (`sprawl_<version>_<goos>_<goarch>.tar.gz`) and `checksums.txt` to a temp dir.
7. **Verify** the SHA256 of the tarball matches its row in `checksums.txt`. Mismatch aborts with a clear error and leaves the original binary untouched.
8. **Extract** the `sprawl` entry from the tarball. Path-traversal entries (anything that doesn't clean to exactly `sprawl`) are rejected.
9. **Atomic replace** at `os.Executable()`:
   - Resolve symlinks so we touch the real path.
   - Stage the new bytes as `<exe>.new`.
   - `Rename(<exe>, <exe>.old)`, then `Rename(<exe>.new, <exe>)`.
   - On failure of the second rename, roll back the first so the user is never left without a binary.
   - Best-effort cleanup of `<exe>.old`.
10. **Bust the notify cache** so the next invocation doesn't print a stale "update available" banner.
11. Print `Updated sprawl 0.1.0 → 0.2.0.` and exit 0.

Exit codes: `0` for success or any of the friendly no-op exits (dev binary, local build, already-latest, user-cancelled prompt). `1` for any genuine failure (network, checksum, tarball, rename). On failure, the original binary is intact.

## Why no auto-install

Agents are CLI consumers. Surprise behavior — a binary swapping itself out mid-session because someone tagged a release — is exactly the kind of thing that breaks scripted runs in unrepeatable ways. The notice is the strongest passive signal we'll send; the `update` subcommand is the only thing that ever writes to the binary.
