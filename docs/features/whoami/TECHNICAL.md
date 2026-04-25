# Whoami — Technical

## Source Files

| File | Role |
|------|------|
| `internal/client/client.go` | `Whoami(ctx)` + `Whoami` / `Agent` / `ProjectPermission` types. |
| `internal/cli/whoami.go` | Cobra subcommand; resolves auth, builds payload + text view. |

## Noteworthy Behavior

- **Pre-flight check fails without an HTTP call.** `newAuthedClient` returns an error when `SPRAWL_AGENT_SECRET` (or `--agent-secret`) is missing; the command surfaces that error through the same `reportErr` path as wire errors, so the format is consistent.
- **Text rendering groups by level.** `whoamiText` collapses `project_permissions` into one line per scope (`write_create` first, then `write`, then `read`) so a long override list stays scannable. Within a group, names follow the server's `project_id` ordering — no extra sort.
- **`status: "ok"` is preserved on output.** The client decodes the wire shape into `*Whoami` (dropping the redundant status flag), but the CLI re-injects `"status":"ok"` into the rendered payload so JSON / TOON consumers see the full server contract.
