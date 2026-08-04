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
| [auth-and-config](features/auth-and-config/INDEX.md) | Device-flow login, credential resolution (bearer + project key and/or agent secret), two-binary build pattern, config storage. |
| [output-formats](features/output-formats/INDEX.md) | Uniform text / JSON / TOON rendering contract and error envelope. |
| [tasks](features/tasks/INDEX.md) | `task list / show / search / create / update`. |
| [items](features/items/INDEX.md) | `item <id>` (show, note included) plus `item add / update / check / uncheck / delete`, and the shared item table behind every item view. A note is a field of an item — there is no `note` command. |
| [item-state-and-pr](features/item-state-and-pr/INDEX.md) | Hand-set item state + GitHub PR number on an item: `item state / pr`, the cross-task `queue` read, and the TUI's `s` / `p` keys. |
| [theme](features/theme/INDEX.md) | `theme get / set` — owner-only UI theme. |
| [whoami](features/whoami/INDEX.md) | `whoami` identity probe — caller agent, confined project, elevated project permissions. |
| [activity](features/activity/INDEX.md) | `activity` — daily completion log (completed tasks + completed items) for a single day. |
| [auto-update](features/auto-update/INDEX.md) | Once-per-day update notice + `sprawl update` (download, verify, atomic replace). |
| [interactive-tui](features/interactive-tui/INDEX.md) | Full-screen terminal UI (bare `sprawl` / `sprawl tui`) to browse tasks, work checklists, edit notes, and copy Markdown context for LLMs. |

## Quick Links

- [CONTEXT.md](CONTEXT.md) — glossary of shared terms (terminal palette vs app theme, task vs item, note, item state vs task status, PR number, queue)
- [README](../README.md) — install + usage
- [RELEASING](RELEASING.md) — tag, build, and publish a new version with goreleaser
