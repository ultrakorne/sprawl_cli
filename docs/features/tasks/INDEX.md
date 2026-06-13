# Tasks

Read and write commands for tasks: `task <id>` (show), `task list`, `task search`, `task create`, `task update`, `task due`, `task delete`. Each wraps a single `/api/v1/tasks*` endpoint. A bare positional id shows one task (mirroring `checklist <task_id>`); there is no `task show` subcommand. `task <id> --full` embeds the task's checklist items + notes in one call. Server-side per-agent permission filtering means non-owner agents only see tasks their key resolves `:read` / `:write` / `:write_create` on.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Commands, UX, permission model |
| [TECHNICAL.md](TECHNICAL.md) | Source files, wire shapes, `--from-json` plumbing |
