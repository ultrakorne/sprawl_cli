# Auto-update

Once-per-day version notice plus an explicit `sprawl update` subcommand. Scoped to the prod `sprawl` binary; `sprawl_dev` and any non-release build skip the check entirely. The notice is a single colored line on stderr — never noisy, never modal — and the update flow downloads the released tarball, verifies it against `checksums.txt`, and atomically replaces the running binary.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | UX, banner format, escape hatch, update flow, edge cases |
| [TECHNICAL.md](TECHNICAL.md) | Source files, cache shape, test seam |
