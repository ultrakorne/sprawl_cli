# Tasks

Read and write commands for tasks: `task list`, `task show`, `task search`, `task create`, `task update`, `task due`, `task delete`. Each wraps a single `/api/v1/tasks*` endpoint. Server-side per-agent permission filtering means non-owner agents only see tasks their key resolves `:read` / `:write` / `:write_create` on.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Commands, UX, permission model |
| [TECHNICAL.md](TECHNICAL.md) | Source files, wire shapes, `--from-json` plumbing |
