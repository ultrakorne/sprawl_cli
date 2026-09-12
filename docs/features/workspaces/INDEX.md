# Workspaces

A workspace is an independent canvas of tasks and projects — the user's own, or one shared with them. Every task, item, queue and activity call runs in exactly one workspace: by default the one the credentials resolve to, otherwise the one named by `--workspace` / `-w` / `SPRAWL_WORKSPACE`. The selector only picks the canvas; it never stands in for a project key or an agent secret. `workspace list` discovers ids, `whoami` names the workspace a session runs in, and the TUI's `w` key switches for the rest of a session.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | The selector, `workspace list`, what `whoami` and the TUI show, the decisions behind "selector, not factor" |
| [TECHNICAL.md](TECHNICAL.md) | Where resolution, the path prefix and the TUI switch live; the contracts a reader cannot infer from the code |
