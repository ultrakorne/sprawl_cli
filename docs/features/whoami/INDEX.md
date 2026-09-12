# Whoami

`sprawl whoami` — authenticated identity probe against `GET /api/v1/whoami`. Returns the calling agent (id, name, emoji, owner flag, default permission), the workspace this session's task calls run in plus every workspace the user can reach, and the project this session is confined to when a project key is set (name, key, the level you resolve to there, and its GitHub repo URL when it has one). Doubles as a liveness check for the auth pipeline.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Behaviour, response shape, exit codes |
| [TECHNICAL.md](TECHNICAL.md) | Source file pointers |
