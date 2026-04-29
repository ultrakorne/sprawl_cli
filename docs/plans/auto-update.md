# Auto-update for the sprawl CLI

## Context

The sprawl CLI is a Go binary distributed via goreleaser → GitHub releases. Today there's no way for an installed user to learn that a newer version exists, and no in-binary update path. We want:

1. A once-per-day passive check that, when a newer release exists, prints a single colored line at the start of the next command — humans see it, AI agents calling the CLI can ignore it.
2. An explicit `sprawl update` subcommand that downloads, verifies, and atomically replaces the running binary.
3. Logic scoped to the **prod `sprawl` binary only**. `sprawl_dev` is meant to be built from source against a local server; it should never check or update.
4. No CI/release automation in this change — the goreleaser flow stays manual (see `docs/RELEASING.md`).

This unblocks shipping fixes without users being stuck on stale binaries, while keeping the AGENTS.md invariant that AI agents using the CLI don't see surprising mid-session behavior (no auto-install — only a notice).

## Decision: repo will be public

`ultrakorne/sprawl_cli` will be flipped to a public GitHub repo before this feature ships. This plan is written against that assumption — no private-repo fallback, no backend proxy, no auth on the GitHub API or release-asset downloads. The updater calls `https://api.github.com/repos/ultrakorne/sprawl_cli/releases/latest` and the asset URLs without an `Authorization` header.

## How goreleaser works today (orientation)

The full release process lives in [`docs/RELEASING.md`](../RELEASING.md). The short version: `git tag vX.Y.Z && git push origin vX.Y.Z`, then `GITHUB_TOKEN=$(gh auth token) goreleaser release --clean`. Goreleaser reads the tag, builds 4 `sprawl` binaries (linux/darwin × amd64/arm64), tarballs them, writes `checksums.txt`, and uploads the release on `github.com/ultrakorne/sprawl_cli`. Asset name template: `sprawl_<version>_<goos>_<goarch>.tar.gz`. (`sprawl_dev` is intentionally not released — built from source via `make build-dev`.)

For a fresh build without uploading, use `goreleaser release --snapshot --clean`.

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
- `docs/features/` — new feature doc folder per the project-documentation skill convention. Cover: how the daily check works, the `SPRAWL_NO_UPDATE_CHECK` escape hatch, and how `sprawl update` verifies and swaps. The repo is public, so no private-repo caveat is needed.

## Verification

1. `make check` (fmt + vet + tests) — must pass per AGENTS.md.
2. `make test-race` — the cache file is written from `PersistentPreRunE`, which is fine, but verify there's no shared-state surprise.
3. **Notify path manual test**:
   - `make build` → `./dist/sprawl version` (baseline)
   - Manually edit the cache file to set `latest_version` to a higher version + `checked_at` to now, run `./dist/sprawl whoami` → expect a yellow banner on stderr.
   - Set `SPRAWL_NO_UPDATE_CHECK=1` and re-run → no banner.
   - Verify `./dist/sprawl_dev whoami` never prints a banner regardless of cache state.
4. **Update path manual test** (requires at least two tagged releases on the now-public GitHub repo — see `docs/RELEASING.md` for the release flow):
   - Tag and release `vA.B.C`, then later tag and release `vA.B.C+1`.
   - Install the older binary locally, run `./sprawl update --yes`, confirm version output bumps.
   - Force a checksum mismatch (point at a doctored tarball via a test seam or a `--release-url` debug flag) → expect abort, original binary intact.
   - Verify with the repo public: `curl -s https://api.github.com/repos/ultrakorne/sprawl_cli/releases/latest | jq .tag_name` returns 200 with the expected tag *without* an Authorization header. If this returns 404, the repo flip-to-public step did not happen — stop and surface that to the user.
5. **Snapshot test before tagging anything real**: `goreleaser release --snapshot --clean` to confirm the existing `.goreleaser.yaml` still builds clean once the new packages are in.
