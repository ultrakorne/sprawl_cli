# Skill install — Design

## Overview

The repo ships two AI-tool artefacts alongside the CLI: the **`sprawl` skill** (a directory of guidance / setup files at `.claude/skills/sprawl/`) and the **`sprawl-bookkeeper` agent** (a single-file sub-agent definition, in two flavours — `.claude/agents/sprawl-bookkeeper.md` and `.opencode/agents/sprawl-bookkeeper.md`, because the frontmatter schemas differ). `sprawl skill install` lets the user drop these into the directories Claude Code or OpenCode loads from, without cloning the repo or hand-copying files.

The command is interactive only. The prompts are bubbletea TUI screens — arrow / vim keys to move, space to toggle a multi-select row, enter to confirm, esc or ctrl+c to cancel. There is no flag-driven mode and no piped-input mode: the prompt models consume key events, not lines, so feeding stdin from a script does not work. Anyone scripting an install today has to clone the repo and run `scripts/install-skill.sh` — that's an acceptable cost since the matrix has only sixteen states.

## Install matrix

Three picks, in order. The first two are multi-select; the third is single-select.

1. **What** — `sprawl skill`, `sprawl-bookkeeper agent`, or both.
2. **For which AI tools** — Claude Code, OpenCode, or both.
3. **Scope** — `global` (the user's home) or `local` (the current working directory).

Multi-select rows start all-checked. Pressing enter on the first stage with the defaults intact picks both items; an empty selection (everything toggled off) is refused — the user must toggle at least one row back on or hit esc to cancel the flow. The single-select scope cursor starts on `Global` so a default-everything install lands in the user's home dir.

A `Choice{What, Tools, Scope}` expands to one `Target` per (what × tool) pair. A confirmation summary lists every absolute destination path before any download or write happens.

### Destination paths

| What | Tool | Global path | Local path (under cwd) |
|------|------|-------------|------------------------|
| skill | Claude Code | `~/.claude/skills/sprawl/` | `<cwd>/.claude/skills/sprawl/` |
| skill | OpenCode | `~/.config/opencode/skills/sprawl/` | `<cwd>/.opencode/skills/sprawl/` |
| agent | Claude Code | `~/.claude/agents/sprawl-bookkeeper.md` | `<cwd>/.claude/agents/sprawl-bookkeeper.md` |
| agent | OpenCode | `~/.config/opencode/agents/sprawl-bookkeeper.md` | `<cwd>/.opencode/agents/sprawl-bookkeeper.md` |

The path asymmetry between OpenCode global (`~/.config/opencode/...`) and OpenCode local (`<cwd>/.opencode/...`) mirrors the directories OpenCode itself reads from in those two scopes — it's not a sprawl convention.

## Source: master tarball, not a git clone

Each `install` (and each `update`) downloads `https://api.github.com/repos/ultrakorne/sprawl_cli/tarball/master`, expands it in-memory, and writes only the entries the chosen targets need. The repo is public and the call is unauthenticated; if the repo ever flips private, install / update silently fails the same way auto-update does.

This means the user gets whatever's on master at install time, not whatever's bundled into the binary they're running — by design. The skill and agent files iterate independently of the CLI release cadence, and a fresh skill install shouldn't require a binary release.

## Recorded installs and refresh

Every successful per-target write upserts a row into `[[skill_installs]]` in `config.toml`:

```toml
[[skill_installs]]
kind = "skill"            # "skill" | "agent"
name = "sprawl"           # "sprawl" | "sprawl-bookkeeper"
tool = "claude"           # "claude" | "opencode"
scope = "global"          # "global" | "local"
path = "/Users/x/.claude/skills/sprawl"
version = "0.1.0"         # parsed from the freshly-written frontmatter
```

`path` is the install identity: re-installing the same target replaces the existing row in place rather than appending a duplicate.

`sprawl update` then has two halves: (1) the binary-update flow described in [auto-update](../auto-update/DESIGN.md), and (2) for every recorded `skill_installs` row, compare `version` against the current frontmatter on master and re-extract the stale ones from a single tarball download. The version pinning means a no-op `update` makes no destructive writes — directories are wiped and rewritten only when something actually changed.

## Stale-skill banner

The once-per-day notify path (`updater.MaybeNotify`) was extended to probe the three master-branch frontmatter files (`SKILL.md`, both `sprawl-bookkeeper.md` flavours) in parallel and cache the versions next to `latest_version` in `update_check.json`. When any recorded install is older than its remote, a single yellow stderr line is printed:

- `sprawl skill update available — run \`sprawl update\`.`
- `sprawl-bookkeeper agent update available — run \`sprawl update\`.`
- Combined form when both are stale.

Same gating as the binary banner: prod `sprawl` only, only when `IsReleaseVersion(build.Version)`, suppressible with `SPRAWL_NO_UPDATE_CHECK=1`. A user who hasn't run `skill install` has no recorded rows and never sees this banner.

## UX details

- **`login` nudges first-time users** toward `sprawl skill install` when no `[[skill_installs]]` rows exist. Re-login keeps existing rows: `onApproved` does a load-then-mutate so the token write doesn't wipe bookkeeping.
- **Skill writes wipe the destination directory** before unpacking. A skill is a *directory* of files; if a file was removed upstream, leaving the stale copy on disk would mislead the loader. Agents are single-file overwrites.
- **Per-target failures don't abort the run.** Each target prints `✓` or `✗` with its own error; the command exits non-zero only if at least one target failed. Successful writes are still recorded so the next `update` can resume.
- **Frontmatter version parser is a hand-rolled scanner**, not a YAML lib. The version field is a single top-level scalar; pulling in a YAML dependency for a one-line lookup would violate the no-extra-deps stance.

## Uninstall

`sprawl skill uninstall` is the symmetric tear-down. It loads `config.toml`, prints every recorded `[[skill_installs]]` row (`name (tool, scope) ← path`), asks once for confirmation through the same bubbletea Yes/No prompt the install confirmation uses, then deletes each `path` (`os.RemoveAll`, which copes with both skill directories and single agent files) and drops its row.

There is no per-target picker by design — the bookkeeping already enumerates every place a copy landed, and the install matrix is small enough that "remove the lot" is the only useful default. A user who wants finer control can edit `config.toml` and remove rows by hand before re-running `uninstall`.

Per-target failures don't abort the rest. A removal that fails (e.g. permissions) prints `✗ <path>: <err>` and keeps its config row so the user can retry; successful removals are dropped from the config regardless. An empty `[[skill_installs]]` table prints `Nothing installed.` and exits cleanly without prompting.

## Why a separate command, not bundled into `login`

`login` already does one thing — device-flow authentication — and the user might run it many times (token expiry, fresh shells on shared machines). Auto-installing skills there would either (a) re-extract the same files repeatedly or (b) need its own "already installed" gate, both of which are worse than a one-time explicit command with a clear name.

## Why source from master, not from the running binary

The skill and agent files are AI-tool guidance, not Go code; embedding them into the binary would couple their cadence to release cuts, prevent fixing prompt regressions without a re-release, and inflate the binary for content that's only useful to a subset of users (those running an AI tool). Master-branch sourcing keeps the skill iteration loop fast and the binary lean.
