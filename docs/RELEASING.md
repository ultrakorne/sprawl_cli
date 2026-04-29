# Releasing

The sprawl CLI ships as a single binary built by `goreleaser` from a Git tag. Releases are **manual** — there is no CI release workflow today. Tags are the trigger; goreleaser builds 4 `sprawl` binaries (linux/darwin × amd64/arm64), packages them as tarballs, writes `checksums.txt`, and uploads everything as a GitHub release on `ultrakorne/sprawl_cli`.

`sprawl_dev` is intentionally **not** released — it's a dev-only binary with a localhost URL baked in. Build it from source with `make build-dev`.

## Prerequisites (one-time)

- **goreleaser** on `PATH`. Pin a stable version in `mise.toml`, do *not* use `@latest` — aqua resolves it to a nightly whose attestation fails verification.
  ```sh
  mise ls-remote goreleaser | grep -v nightly | tail -5   # find the latest stable
  mise use -g goreleaser@<version>                         # e.g. 2.5.1
  goreleaser --version
  ```
- **`gh` authenticated** to `github.com` with `repo` scope (covers private repos):
  ```sh
  gh auth status
  gh auth refresh -s repo    # only if 'repo' is missing
  ```

## Release checklist

1. **Working tree clean and tests green.** Goreleaser refuses to release a dirty tree, and AGENTS.md requires `make check` before declaring a change done.
   ```sh
   git status                # must be clean
   make check                # fmt-check + vet + test
   make test-race            # extra confidence pre-release
   ```

2. **Pick the version.** Semver, `vX.Y.Z`. Look at the previous tag and the commit log since:
   ```sh
   git tag --sort=-v:refname | head -5
   git log $(git describe --tags --abbrev=0)..HEAD --oneline
   ```

3. **Snapshot build first.** Validates `.goreleaser.yaml` and produces local artifacts in `dist/` without uploading anything.
   ```sh
   goreleaser release --snapshot --clean
   ls dist/                  # expect 4 sprawl binaries + 4 tarballs + checksums.txt
   ```

4. **Tag and push.** The tag is what goreleaser reads to derive `{{ .Version }}` (`v0.2.0` → `0.2.0`).
   ```sh
   git tag v0.2.0
   git push origin v0.2.0
   ```

5. **Release.** `gh auth token` mints a token with the scopes already on your `gh` session — no need to create a PAT manually.
   ```sh
   export GITHUB_TOKEN=$(gh auth token)
   goreleaser release --clean
   ```

6. **Verify.**
   ```sh
   gh release view v0.2.0    # confirm artifacts + checksums uploaded
   ```

## Manual PAT alternative

If you don't want to use `gh auth token`, create a token at <https://github.com/settings/tokens>:

- **Classic**: scope `repo` (covers private repos).
- **Fine-grained**: repo `ultrakorne/sprawl_cli`, permission **Contents: Read and write**.

Export it as `GITHUB_TOKEN` before running `goreleaser release --clean`.

## Repo visibility and auto-update

The in-binary auto-update path (see [`features/auto-update`](features/auto-update/INDEX.md)) calls `https://api.github.com/repos/ultrakorne/sprawl_cli/releases/latest` and the asset URLs without an `Authorization` header — it relies on the repo being public. If the repo is ever flipped private, both the daily notice and `sprawl update` will silently fail (404 / network error) and a backend proxy or auth-bearing client will need to be wired in.

Releasing itself works either way — goreleaser authenticates with `GITHUB_TOKEN` regardless of visibility.

## Recovering from a bad release

A tag that goes out wrong is recoverable, but the tag itself is the source of truth — fix forward when possible.

```sh
gh release delete v0.2.0 --yes --cleanup-tag    # removes the release AND the remote tag
git tag -d v0.2.0                                # delete the local tag
# fix the issue, commit, then re-tag and re-run the release
```

Prefer cutting `v0.2.1` over re-using a published tag — anyone who already pulled `v0.2.0` will not see a re-tagged release.

## Testing Autoupdate locally
###  Banner + update against the real GitHub release

Once a release exists on ultrakorne/sprawl_cli (e.g. v0.1.0):

rm -f ~/.config/sprawl/update_check.json
make build VERSION=v0.0.1     # pretend we're behind
./dist/sprawl whoami           # banner fires from a real GitHub /releases/latest call
./dist/sprawl update --yes     # downloads v0.1.0, verifies SHA256, replaces dist/sprawl
./dist/sprawl version          # confirms the bump

Anything dirty (e.g. plain make build) refuses with the local build (version=…) message — that's by design.