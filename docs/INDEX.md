# sprawl CLI Documentation

HTTP client for the sprawl task-management API. Ships as two binaries from one Go codebase — `sprawl` (prod URL baked in) and `sprawl_dev` (local URL baked in) — and is used by humans and AI agents. The CLI is a thin JSON-over-HTTP client; the server is the source of truth for validation and permissions.

## Tech Stack

- **Language**: Go (pinned via `mise.toml`)
- **Framework**: `github.com/spf13/cobra`
- **HTTP**: stdlib `net/http` + `encoding/json`
- **Config**: TOML (`github.com/BurntSushi/toml`)
- **Output**: TOON (`github.com/alpkeskin/gotoon`), JSON, text
- **Releases**: `goreleaser`

## Features

| Feature | Description |
|---------|-------------|
| [auth-and-config](features/auth-and-config/INDEX.md) | Device-flow login, credential resolution, two-binary build pattern, config storage. |
| [output-formats](features/output-formats/INDEX.md) | Uniform text / JSON / TOON rendering contract and error envelope. |
| [tasks](features/tasks/INDEX.md) | `task list / show / search / create / update`. |
| [checklists](features/checklists/INDEX.md) | `checklist list / add / check / uncheck / update` and `note show / set`. |
| [theme](features/theme/INDEX.md) | `theme get / set` — owner-only UI theme. |
| [whoami](features/whoami/INDEX.md) | `whoami` identity probe — caller agent + elevated project permissions. |
| [activity](features/activity/INDEX.md) | `activity` — daily completion log (completed tasks + completed checklist items) for a single day. |
| [auto-update](features/auto-update/INDEX.md) | Once-per-day update notice + `sprawl update` (download, verify, atomic replace). |

## Quick Links

- [README](../README.md) — install + usage
- [RELEASING](RELEASING.md) — tag, build, and publish a new version with goreleaser
