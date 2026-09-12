# sprawl CLI Documentation

HTTP client for the sprawl task-management API, shipped as two binaries from one Go codebase — `sprawl` (prod URL baked in) and `sprawl_dev` (local URL baked in) — and used by the human owner and by AI agents. The CLI is a thin JSON-over-HTTP client; the server is the source of truth for validation and permissions.

## Tech Stack

- **Language**: Go (pinned in `mise.toml`), single static cgo-free binary
- **CLI**: `github.com/spf13/cobra`; **TUI**: `charm.land/bubbletea/v2`; **styling**: `charm.land/lipgloss/v2`
- **HTTP**: stdlib `net/http` + `encoding/json`; **config**: TOML (`github.com/BurntSushi/toml`)
- **Releases**: goreleaser on tag push

## Features

| Feature | Description |
|---------|-------------|
| [auth-and-config](features/auth-and-config/INDEX.md) | Device-flow login, credential resolution (bearer + project key and/or agent secret), two-binary build, config storage. |
| [workspaces](features/workspaces/INDEX.md) | The `--workspace` selector, `workspace list`, and the TUI's `w` switch — which canvas a call runs in. |
| [output-formats](features/output-formats/INDEX.md) | Uniform text / JSON rendering contract, item normalization, error envelope and hints. |
| [tasks](features/tasks/INDEX.md) | `task <id> / list / search / create / update / due / delete`. |
| [items](features/items/INDEX.md) | `item <id> / add / update / check / uncheck / delete` and the shared item table behind every item view; a note is a field of an item. |
| [item-state-and-pr](features/item-state-and-pr/INDEX.md) | Hand-set item state and GitHub PR number: `item state / pr`, the cross-task `queue`, the TUI's `s` / `p` / `o` keys. |
| [theme](features/theme/INDEX.md) | `theme get / set` — the owner's web-app theme. |
| [whoami](features/whoami/INDEX.md) | `whoami` — caller agent, workspace, confined project, permission levels. |
| [activity](features/activity/INDEX.md) | `activity` — completed tasks and items for one day. |
| [auto-update](features/auto-update/INDEX.md) | Once-per-day update notice and `sprawl update` (download, verify, atomic replace). |
| [interactive-tui](features/interactive-tui/INDEX.md) | Full-screen terminal UI (bare `sprawl` / `sprawl tui`) to browse tasks, work checklists, edit notes, switch workspace, and copy Markdown for LLMs. |

## Quick Links

- [CONTEXT.md](CONTEXT.md) — glossary of shared terms (workspace, selector vs factor, role vs level, task vs item, note, item state vs task status, PR number, queue)
- [README](../README.md) — install + usage
- [RELEASING](RELEASING.md) — tag, build, and publish a new version with goreleaser
