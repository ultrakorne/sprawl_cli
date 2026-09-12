# Output Formats

Uniform text / JSON rendering contract for every structured-output subcommand. Default is JSON; override per-invocation with `--format` or session-wide with `SPRAWL_OUTPUT`. Also defines the error envelope used when a command fails in JSON mode.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Rendering contract, error envelope, precedence |
| [TECHNICAL.md](TECHNICAL.md) | Source files, renderer plumbing, map round-trip |
