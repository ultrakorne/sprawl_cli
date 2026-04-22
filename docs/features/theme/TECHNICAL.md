# Theme — Technical

## Source Files

| File | Role |
|------|------|
| `internal/client/client.go` | `GetTheme`, `SetTheme` — flat envelope round-trip. |
| `internal/cli/theme.go` | `theme get` / `theme set <id>` cobra subcommands. |

## Noteworthy Behavior

- **Arg validation runs inside `RunE` and routes through `reportErr`** so `sprawl theme set` with zero or multi args renders the error in the chosen format instead of cobra's default stderr path.
- **No id munging.** Whatever the caller types is what the server sees; the server's 404 is the only source of truth for what's valid.
