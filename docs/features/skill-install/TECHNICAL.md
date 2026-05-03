# Skill install — Technical

## Architecture

`internal/cli/skill.go` wires the `skill install` cobra command and hands stdin/stdout off to `internal/skill.Install`. The skill package owns the whole flow: prompt the user (`prompt.go`), expand the `Choice` into `Target`s (`targets.go`), download and gunzip the master tarball (`download.go`), write each target while parsing its frontmatter version (`write.go`, `frontmatter.go`), and upsert each row into `config.toml` via `config.Config.UpsertInstall`.

`sprawl update` joins this to the binary-update flow: `internal/cli/update.go` runs `updater.RunUpdate` first, then unconditionally calls `skill.Update` regardless of the binary result. Skill update reuses the same tarball-download / extract / write helpers but probes per-file frontmatter on `raw.githubusercontent.com` first (`remote.go`) so it can short-circuit no-op runs without downloading the full tarball.

The once-per-day notify path (`internal/updater/updater.go::MaybeNotify`) calls `skill.FetchRemoteVersions` alongside its existing release-tag probe; the cached `update_check.json` file gained three optional fields for the master-branch versions (skill, claude agent, opencode agent) so banners survive 24h without re-probing.

## Source files

| File | Role |
|------|------|
| `internal/cli/skill.go` | Cobra wiring for `skill`, `skill install`, and `skill uninstall`. |
| `internal/cli/update.go` | `update` runs `updater.RunUpdate` then `skill.Update`, joining errors. |
| `internal/cli/login.go` | After a successful login, suggests `sprawl skill install` when no installs are recorded; uses load-then-mutate so re-login preserves rows. |
| `internal/skill/install.go` | `Install` — orchestrates prompt → target resolution → download → write → record. |
| `internal/skill/uninstall.go` | `Uninstall` — list recorded rows, confirm, `os.RemoveAll` each path, drop the rows from config. |
| `internal/skill/prompt.go` | `multiSelectModel` / `singleSelectModel` (bubbletea v2 models), `runPromptChoice`, `runPromptConfirm`; lipgloss palette for the cursor / checkbox / hint styling. |
| `internal/skill/targets.go` | `Choice` / `Target`, `ResolveTargets`, `srcFor`, `dstFor` — the destination-path matrix. |
| `internal/skill/download.go` | `fetchMasterTarball`, `extractTarball` — GitHub API tarball, gzip + tar walk, top-prefix strip. |
| `internal/skill/write.go` | `writeTarget` (skill = wipe-and-rewrite directory, agent = single-file overwrite). |
| `internal/skill/frontmatter.go` | `ParseFrontmatterVersion` — line scanner for the `version:` field in `---`-delimited YAML frontmatter. |
| `internal/skill/remote.go` | `FetchRemoteVersions`, `RemoteVersions.VersionFor` — parallel probe of three raw frontmatter files for fast staleness checks. |
| `internal/skill/update.go` | `Update` — diff recorded versions vs remote, re-extract stale, persist new versions. |
| `internal/config/config.go` | `SkillInstall` schema, `Config.UpsertInstall`, `Config.RemoveInstall`. |
| `internal/updater/updater.go` | `MaybeNotify` extended to probe + cache skill/agent versions and emit a stale-install banner. |

## Target resolution

`dstFor(what, tool, scope, home, cwd)` is a switch over a `"<what>/<tool>/<scope>"` key returning the absolute path. Unknown combinations return `""`; callers feed values straight from the prompts so this is treated as unreachable. The local-scope label shown in the prompt includes the resolved cwd so the user sees exactly where files will land before confirming.

`srcFor(what, tool)` returns the repo-relative path inside the tarball. Skill source is the same path for both tools (`.claude/skills/sprawl`) — the directory is tool-agnostic. Agent source diverges per tool because the frontmatter shapes differ (Claude uses `tools:` / `skills:`, OpenCode has its own keys).

## Tarball handling

`fetchMasterTarball` hits `https://api.github.com/repos/ultrakorne/sprawl_cli/tarball/master` with a 30 s context timeout and the standard `Accept: application/vnd.github+json` header. Default `http.Client` follows the 302 to codeload transparently; no redirect plumbing needed.

`extractTarball` walks the gzip+tar reader, skipping non-regular entries. GitHub wraps the repo in a top-level prefix dir (`ultrakorne-sprawl_cli-<sha>/`); the extractor strips the first path segment from every entry so callers see clean repo-relative paths matching what `srcFor` returns.

`writeSkillDir` `os.RemoveAll`s the destination before unpacking to avoid leaving stale files from a prior install. `writeAgentFile` is a plain overwrite. Both parse the frontmatter `version:` from the freshly-written content (SKILL.md for skills, the agent file for agents) and return that string for the config bookkeeping; an empty result means the marker file lacked frontmatter and the recorded version is empty.

## Update flow

`skill.Update` does **two passes** over `cfg.SkillInstalls`:

1. Cheap probe: `FetchRemoteVersions(ctx)` fans out three concurrent GETs against `raw.githubusercontent.com` (5 s per-request timeout) and parses each frontmatter version. For each install record, compare `inst.Version` against `RemoteVersions.VersionFor(inst.Kind, inst.Tool)`. Up-to-date and unprobeable rows are reported and skipped; others go on the stale list.
2. If any are stale, download the full master tarball once and re-extract just those targets, updating `cfg.SkillInstalls[i].Version` to the freshly-parsed value (falling back to the remote-probe result if the parse came back empty).

A single failed write doesn't abort the rest — errors are joined and surfaced after the config is saved so successful writes are still recorded. `cfg.Save` runs unconditionally at the end so version bumps are persisted even on partial failure.

## Notify integration

`updater.MaybeNotify` was extended to probe + cache skill/agent versions alongside the release tag. Cache shape:

```json
{
  "checked_at": "...",
  "latest_version": "v0.2.0",
  "latest_skill_version": "0.1.0",
  "latest_claude_agent_version": "0.1.0",
  "latest_opencode_agent_version": "0.1.0"
}
```

The skill-version probe goes through a `fetchRemoteSkillVersions` package var (defaulting to `skill.FetchRemoteVersions`) so updater tests can stub it without spinning up servers for the skill package's hosts.

`hasStaleInstall(installs, kind, tool, remote)` reports whether any recorded install of that (kind, tool) has a `Version` differing from `remote`. An empty `remote` means "couldn't probe" and is treated as not-stale rather than nag-without-certainty. `tool == ""` matches any tool (used for skills, which don't differ by tool). One of three banners is printed depending on which combinations are stale.

A successful `sprawl update` calls `removeCache` on the binary path; the skill-update half doesn't bust the cache itself — the next 24 h window will re-probe and find everything in sync, so the banner naturally clears.

## Schema additions to `config.toml`

`Config` gained a `SkillInstalls []SkillInstall` field with the `toml:"skill_installs,omitempty"` tag, so existing configs without the table parse fine. `UpsertInstall(inst)` matches by `Path`, `RemoveInstall(path)` returns `bool` for "did anything change", and the existing atomic write path in `Save` carries the new field with no schema-version dance — the field is a non-breaking addition.

`onApproved` in `internal/cli/login.go` was changed from a blank-slate save to a load-then-mutate so the token rewrite preserves any existing `[[skill_installs]]` rows. This is the only place the regression is silently dangerous: every other writer goes through `UpsertInstall` / `RemoveInstall` on a freshly-loaded `Config`.

## Test seams

- `skill.baseURL` (download.go) — GitHub API root, swapped in `download_test.go` for `httptest.Server`.
- `skill.rawBaseURL` (remote.go) — raw content host, swapped in `update_test.go`.
- `updater.fetchRemoteSkillVersions` (updater.go) — probe seam, stubbed in `updater_test.go` so updater tests don't need a working raw-host fixture.
- `skill.promptChoiceFunc` / `skill.promptConfirmFunc` (prompt.go) — interactive prompts. `install_test.go` substitutes deterministic returns via `stubPrompts` / `stubPromptsCancelled` so `Install` end-to-end tests don't drive a fake TTY. The bubbletea models themselves are unit-tested in `prompt_test.go` by feeding synthetic `tea.KeyPressMsg`s directly into `model.Update`; no `tea.NewProgram` is spun up under test.

There is no user-visible flag; production hosts are fixed for end users.
