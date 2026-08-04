# Tasks

Read and write commands for tasks: `task <id>` (show), `task list`, `task search`, `task create`, `task update`, `task due`, `task delete`. Each wraps a single `/api/v1/tasks*` endpoint. A bare positional id shows one task (mirroring `item <id>`); there is no `task show` subcommand. `task <id>` renders a title/description header plus the shared item table; `--full` additionally pulls every item's note body and expands it under its row. Server-side per-agent permission filtering means non-owner agents only see tasks their key resolves `:read` / `:write` / `:write_create` on.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Commands, UX, permission model |
| [TECHNICAL.md](TECHNICAL.md) | Source files, wire shapes, `--from-json` plumbing |

## See also

- [items](../items/INDEX.md) — the item table `task <id>` renders is defined there, and every single-item verb lives under `item`.
- [item-state-and-pr](../item-state-and-pr/INDEX.md) — a task's nested project supplies the repo URL that resolves its items' PR links.
- [CONTEXT.md](../../CONTEXT.md) — a task's derived **status** is not an item's hand-set **state**.
