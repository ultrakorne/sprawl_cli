# Skill install

`sprawl skill install` — interactive installer that drops the bundled sprawl skill and the `sprawl-bookkeeper` agent into the directories Claude Code, OpenCode, and Codex load from. Source is the master branch on GitHub (downloaded as a tarball at install time); each landed copy is recorded in `config.toml` so `sprawl update` can refresh it later. The once-per-day notify path also probes master-branch version markers and surfaces a separate banner when any recorded install is stale.

`sprawl skill uninstall` is the inverse: it lists every recorded install, confirms once, then deletes each path and clears its config row. There is no per-target selection — the bookkeeping already names every place a copy landed, so the simple thing is to clear them all.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Prompt flow, install scopes / paths, recorded-install model, refresh story |
| [TECHNICAL.md](TECHNICAL.md) | Source files, target resolution, version probing, update path |
