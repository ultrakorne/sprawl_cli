# Auto-update for the sprawl CLI

## Context

The sprawl CLI is a Go binary distributed via goreleaser → GitHub releases. Today there's no way for an installed user to learn that a newer version exists, and no in-binary update path. We want:

1. A once-per-day passive check that, when a newer release exists, prints a single colored line at the start of the next command — humans see it, AI agents calling the CLI can ignore it.
2. An explicit `sprawl update` subcommand that downloads, verifies, and atomically replaces the running binary.
3. Logic scoped to the **prod `sprawl` binary only**. `sprawl_dev` is meant to be built from source against a local server; it should never check or update.
4. No CI/release automation in this change — the goreleaser flow stays manual.

This unblocks shipping fixes without users being stuck on stale binaries, while keeping the AGENTS.md invariant that AI agents using the CLI don't see surprising mid-session behavior (no auto-install — only a notice).

## How goreleaser works today (orientation)

You already have `.goreleaser.yaml` configured. The manual release flow is:

1. **Tag**: `git tag v0.1.0 && git push --tags`
2. **Run** (locally): `goreleaser release --clean` with `GITHUB_TOKEN` exported (token needs `repo` scope on `ultrakorne/sprawl_cli`)
3. **Goreleaser then**:
   - Reads tag → derives `{{.Version}}` (`v0.1.0` → `0.1.0`) and `{{.ShortCommit}}`, `{{.Date}}`
   - Builds both `sprawl` and `sprawl_dev` for `linux/darwin × amd64/arm64` (8 binaries) with the ldflags injecting `internal/build.Version/Commit/Date`
   - Packages each binary into its own tarball (e.g. `sprawl_0.1.0_linux_amd64.tar.gz`, `sprawl_dev_0.1.0_linux_amd64.tar.gz`)
   - Writes `checksums.txt` with SHA256s of every artifact
   - Creates a GitHub release at `github.com/ultrakorne/sprawl_cli`, uploads all artifacts + checksums + auto-generated changelog
4. **Snapshot** for testing without tagging: `goreleaser release --snapshot --clean` builds locally without uploading.

### Does this work if the repo is private?

**The release step itself, yes.** Goreleaser authenticates with your `GITHUB_TOKEN`; private vs public doesn't matter for *creating* a release.

**But auto-update breaks on a private repo.** This is the catch:

- The CLI's update check needs to call `https://api.github.com/repos/ultrakorne/sprawl_cli/releases/latest`. On a private repo this returns **404 to unauthenticated requests** — GitHub doesn't even confirm the repo exists.
- Downloading the tarball asset needs an auth header too.
- Realistic options if the repo stays private:
  1. **Make the repo public** — simplest. The CLI is already reachable; the source being public doesn't expose anything new.
  2. **Proxy through the sprawl backend** — add `GET /api/v1/release/latest` (returns version + tarball URL) and `GET /api/v1/release/<version>/<asset>` on the sprawl server. The CLI already authenticates to that server, so private-repo gating becomes a non-issue. More work, but cleanest for keeping source private.
  3. **Ship a token in the binary** — don't. Anything baked into a Go binary is recoverable with `strings`.

For this plan I'll assume option 1 (public repo) since that's the path of least resistance. If you want option 2 instead, the only thing that changes is the URL and auth header in `internal/updater/github.go`.

## Approach

### 1. Daily background version check (notify)

New package: `internal/updater/`

- **`updater.MaybeNotify(ctx, w io.Writer)`** — called from the root command's `PersistentPreRunE` before any subcommand runs. Always returns nil; never blocks the command on failure.
  - Skip if `build.AppName != "sprawl"` (no-op on dev binary, no-op on `version` subcommand if we want — but easier to keep it on every subcommand, except we also skip `update` itself to avoid double output).
  - Skip if `build.Version == "dev"` or empty (local builds).
  - Skip if env var `SPRAWL_NO_UPDATE_CHECK=1` is set (escape hatch for CI/agents).
  - Read `<configDir>/update_check.json` → `{ checked_at: RFC3339, latest_version: "v0.1.2" }`.
  - If `checked_at` is within the last 24h: use the cached `latest_version` for the comparison. If newer than `build.Version`, print the banner. Don't print twice in one day for the same version (track `notified_at` separately or just rely on the cache window).
  - If older than 24h: spawn the check inline with a tight timeout (2s). On any error → silently extend the cache window by ~1h and return (don't spam the user with network errors at the top of every command).
  - On success: write the cache atomically (mirror the temp+rename pattern in `internal/config/config.go:66-98`).
- **`updater.fetchLatest(ctx)`** — `GET https://api.github.com/repos/ultrakorne/sprawl_cli/releases/latest` with `Accept: application/vnd.github+json` and a 2s timeout. Parse `tag_name`. Stdlib only.
- **Banner format** — printed to stderr (so `sprawl task list --format=json` piped to `jq` is unaffected on stdout). Yellow ANSI when stderr is a TTY, plain otherwise. Honor `NO_COLOR` env var. Single line:
  ```
  sprawl 0.2.0 available (current: 0.1.0). Run `sprawl update`.
  ```
- **Version comparison** — semver-aware. The tags are `vX.Y.Z`; strip the `v`. Tiny comparator (~30 LOC) — no need for a dep. If you want a dep anyway, `golang.org/x/mod/semver` is stdlib-adjacent and the Go team ships it.

### 2. `sprawl update` subcommand

New file: `internal/cli/update.go`. Registered in `internal/cli/root.go:42` next to the other `root.AddCommand` calls.

- **Refuse on dev binary**: if `build.AppName != "sprawl"`, print "sprawl_dev is built from source; use `make build-dev`." and exit 0.
- **Resolve target asset**: query the same GitHub releases endpoint, pick the asset matching `sprawl_<version>_<goos>_<goarch>.tar.gz` based on `runtime.GOOS` / `runtime.GOARCH`.
- **No-op if up-to-date**: print "sprawl 0.1.0 is already the latest." and exit.
- **Confirmation prompt** unless `--yes` / `-y`: "Update sprawl 0.1.0 → 0.2.0? [y/N]".
- **Download** the tarball + the `checksums.txt` to a temp dir.
- **Verify** the SHA256 of the tarball matches the entry in `checksums.txt`. Abort on mismatch.
- **Extract** the `sprawl` binary from the tarball using stdlib `archive/tar` + `compress/gzip`.
- **Atomic replace**: resolve the running binary path with `os.Executable()`, then:
  1. Write the new binary next to it as `sprawl.new` (mode 0755).
  2. Rename current `sprawl` → `sprawl.old` (preserves it if the rename to the new name fails).
  3. Rename `sprawl.new` → `sprawl`.
  4. Best-effort `os.Remove("sprawl.old")` (skip if it fails — leftover .old won't break anything).
  This is the standard POSIX self-update dance and survives if the user is currently executing the binary (the kernel keeps the old inode alive until the process exits).
- **Print** "Updated sprawl 0.1.0 → 0.2.0." and exit.
- **Error handling**: any failure leaves the original binary untouched. Print a clear error and exit 1.

Stdlib only — `net/http`, `archive/tar`, `compress/gzip`, `crypto/sha256`. Matches the existing no-extra-deps pattern in `internal/client/client.go`.

### 3. Cache file location

`<configDir>/update_check.json`, where `configDir` comes from `config.Dir(build.AppName)` (reusing `internal/config/config.go:26-35`). Don't extend `Config` / `config.toml` — the schema there is intentionally minimal per AGENTS.md, and update-check state is operational, not user config.

### 4. Wiring into root

In `internal/cli/root.go`:

- Set `root.PersistentPreRunE` to call `updater.MaybeNotify(cmd.Context(), cmd.ErrOrStderr())`. Make sure to skip when the subcommand is `update` itself (use `cmd.Name()` check) so we don't print "update available" right before running update.
- Add `root.AddCommand(newUpdateCmd())`.

## Critical files to modify

- `internal/cli/root.go` — wire `PersistentPreRunE` + register `newUpdateCmd`.
- `internal/cli/update.go` — **new**, the `sprawl update` cobra command.
- `internal/updater/updater.go` — **new**, `MaybeNotify` + cache I/O.
- `internal/updater/github.go` — **new**, GitHub releases API client + asset resolution + tarball extraction + checksum verification.
- `internal/updater/semver.go` — **new**, tiny `Compare(a, b string) int` for `vX.Y.Z` strings (or pull `golang.org/x/mod/semver`).
- `internal/updater/updater_test.go`, `internal/updater/github_test.go` — `httptest`-mocked tests (matches AGENTS.md guidance: no live backend in tests).
- `docs/features/` — new feature doc folder per the project-documentation skill convention. Cover: how the daily check works, the `SPRAWL_NO_UPDATE_CHECK` escape hatch, how `sprawl update` verifies and swaps, and the private-repo caveat.

## Verification

1. `make check` (fmt + vet + tests) — must pass per AGENTS.md.
2. `make test-race` — the cache file is written from `PersistentPreRunE`, which is fine, but verify there's no shared-state surprise.
3. **Notify path manual test**:
   - `make build` → `./dist/sprawl version` (baseline)
   - Manually edit the cache file to set `latest_version` to a higher version + `checked_at` to now, run `./dist/sprawl whoami` → expect a yellow banner on stderr.
   - Set `SPRAWL_NO_UPDATE_CHECK=1` and re-run → no banner.
   - Verify `./dist/sprawl_dev whoami` never prints a banner regardless of cache state.
4. **Update path manual test** (requires at least one tagged release on GitHub):
   - `git tag v0.1.0 && git push --tags && goreleaser release --clean` to publish.
   - `git tag v0.1.1 && git push --tags && goreleaser release --clean` for a second.
   - Install the older binary locally, run `./sprawl update --yes`, confirm version output bumps.
   - Force a checksum mismatch (point at a doctored tarball via a test seam or a `--release-url` debug flag) → expect abort, original binary intact.
5. **Snapshot test before tagging anything real**: `goreleaser release --snapshot --clean` to confirm the existing `.goreleaser.yaml` still builds clean once the new packages are in.

## Out of scope (intentionally)

- GitHub Actions release-on-tag workflow (user opted to keep manual).
- Homebrew tap finishing — orthogonal; if added later, mac users still benefit from the in-binary updater for non-brew installs.
- Auto-install on launch (rejected — bad for AI-agent callers).
- `sprawl_dev` updating from source (it's a dev binary; `make build-dev` is the update path).
- Signing beyond SHA256 checksums (sigstore/cosign can be added later if needed).
