# Interactive TUI

A full-screen terminal UI for browsing and editing sprawl tasks, launched by a bare `sprawl` on a TTY or by `sprawl tui`. Browse tasks, drill into a task's checklist, toggle items, flag their state and PR number, view and edit notes, do task and item CRUD, search, switch workspace, and copy task or item context as Markdown to the clipboard for pasting into an LLM. Built on bubbletea v2; every color is a terminal-palette index, so the UI adopts the user's terminal theme exactly as the CLI's text output does.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Launch rules, credentials, screens, user flows, keymap, the decisions behind them |
| [TECHNICAL.md](TECHNICAL.md) | Package layout, the bubbletea model, data flow, credential handling, clipboard and editor mechanics |
