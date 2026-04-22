# Health — Technical

## Source Files

| File | Role |
|------|------|
| `internal/client/client.go` | `Health(ctx)` — minimal HTTP wrapper. |
| `internal/cli/health.go` | Cobra subcommand; resolves auth, renders envelope. |

## Noteworthy Behavior

- **Pre-flight check fails without an HTTP call.** `newAuthedClient` returns an error when `SPRAWL_AGENT_SECRET` (or `--agent-secret`) is missing; the command surfaces that error through the same `reportErr` path as wire errors, so the format is consistent.
