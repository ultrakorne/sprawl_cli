# Auto-update — Technical

## Source files

| File | Role |
|------|------|
| `internal/updater/updater.go` | `MaybeNotify`, `IsReleaseVersion`, cache I/O, banner formatting + TTY detection. |
| `internal/updater/github.go` | `RunUpdate` flow: releases-API fetch, asset resolution, tarball + checksum, extract, atomic replace. |
| `internal/cli/update.go` | Cobra wrapper around `updater.RunUpdate`. |
| `internal/cli/root.go` | `PersistentPreRunE` calls `updater.MaybeNotify`, skipping when `cmd.Name() == "update"`. |

## Cache file

Path: `<configDir>/update_check.json`, where `<configDir>` comes from `config.Dir(build.AppName)` (XDG-aware). Mode 0600, directory 0700, atomic temp+rename writes.

```json
{ "checked_at": "2026-04-29T10:00:00Z", "latest_version": "v0.2.0" }
```

The file is operational state, not user config. It is intentionally not part of `Config` / `config.toml` (per AGENTS.md, that schema stays minimal). Network errors still rewrite the cache with `checked_at = now` to back off for the next 24 hours; the previous `latest_version` is preserved across failures so a single transient failure doesn't lose a known target.

## Test seam

`internal/updater` exports an unexported package var:

```go
var baseURL = "https://api.github.com" // overridden in tests
```

Tests swap this for an `httptest.Server` URL via a `t.Cleanup`-scoped helper. There is no user-visible flag; the GitHub API root is fixed for end users.

The atomic-replace target is sourced through another package var, `resolveExecutable`, which tests stub to point at a temp file. This lets the e2e `TestRunUpdate_SuccessReplacesBinary` exercise the full download → verify → extract → swap chain without touching `os.Executable()`.

## Version detection

The release-vs-local distinction uses `golang.org/x/mod/semver`:

- `IsValid(v)` rejects empty/garbage and anything missing the `v` prefix.
- `Prerelease(v) == ""` and `Build(v) == ""` together rule out `git describe` output (`v0.1.0-1-gabc123-dirty`) and pre-release tags (`v0.1.0-rc1`).

Goreleaser tags are clean `vX.Y.Z`, so a real release passes; everything else is treated as a local build and skips both the notify and the update path.

## Wiring

`internal/cli/root.go` sets `PersistentPreRunE` on the root cobra command. The hook short-circuits when `cmd.Name() == "update"` and otherwise calls `MaybeNotify(cmd.Context(), cmd.ErrOrStderr())`. `MaybeNotify` returns nil unconditionally — it never propagates an error that could fail the user's actual command.
