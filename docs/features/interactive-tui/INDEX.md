# Interactive TUI

A full-screen terminal UI for browsing and editing sprawl tasks, launched by a bare `sprawl` on a TTY or via the explicit `sprawl tui` subcommand. Browse tasks, drill into a task's checklist, toggle items, flag their state and PR number, view/edit notes, do full task/item CRUD, search, and copy task/item context as Markdown to the system clipboard for pasting into an LLM. Built on `charm.land/bubbletea/v2`; colors come exclusively from the terminal's ANSI palette (indices 0-15), so the UI adopts the user's terminal theme — the same philosophy as the CLI's text output.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | What it is, why it exists, launch rules, screens, user flows, keymap |
| [TECHNICAL.md](TECHNICAL.md) | Package layout, bubbletea v2 model, screens/overlays, data flow, credential handling, OSC 52 copy, `$EDITOR` flow |

## See also

- [CONTEXT.md](../../CONTEXT.md) — glossary: **terminal palette** vs **app theme**, **task** vs **item**, **note**, **item state** vs **task status**.
- [items](../items/INDEX.md) — the TUI writes notes through the same `PATCH /checklist_items/:id` the `item update` command uses, and renders item states from the same shared icon set.
- [auth-and-config](../auth-and-config/INDEX.md) — the token / agent-secret resolvers the TUI reuses.
- [tasks](../tasks/INDEX.md) and [items](../items/INDEX.md) — the CLI commands the TUI mirrors.
- [item-state-and-pr](../item-state-and-pr/INDEX.md) — what the `s` / `p` keys write and the rules they obey.
