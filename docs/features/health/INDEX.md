# Health

`sprawl health` — authenticated liveness check against `GET /api/v1/health`. Primary use case is sanity-checking a login + agent-secret combo; secondarily, scripting a "is sprawl reachable?" probe.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Behaviour, pre-flight check, exit codes |
| [TECHNICAL.md](TECHNICAL.md) | Source file pointers |
