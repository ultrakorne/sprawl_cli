# Item State & PR Number

Two additive fields on an item — a hand-set **item state** (`ready` / `progress` / `review`, or none) and a **PR number** linking the item to a GitHub pull request. Together they turn a checklist into a live coordination board: agents claim work, hand it back for review, and the human sees who is on what without asking. Surfaced by `item state` / `item pr` (writes), the top-level `queue` (the cross-task "what can I pick up?" read), the `s` / `p` keys in the interactive TUI, and as a `STATE` column on every item table.

The short names are the vocabulary in **both** directions: they are what you type and what every read emits. The server's own spellings (`ready_to_pickup` / `in_progress` / `in_review`) are still accepted as input, but no CLI output uses them — see [output-formats](../output-formats/INDEX.md).

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | What the fields mean, the state lifecycle, the agent hand-off protocol, command UX |
| [TECHNICAL.md](TECHNICAL.md) | Dedicated route, wire shapes, PR-link resolution, optimistic-update contract |
| [FLOW.mermaid](FLOW.mermaid) | The state lifecycle and how it interacts with completion |

## See also

- [CONTEXT.md](../../CONTEXT.md) — glossary: **item state** vs **task status**, **PR number**, **queue**, **repo URL**.
- [items](../items/INDEX.md) — the item commands these fields ride on.
- [interactive-tui](../interactive-tui/INDEX.md) — the `s` / `p` keys and the state column.
- [tasks](../tasks/INDEX.md) — `task <id> --full` renders the fields, and the task's project supplies the repo URL that resolves PR links.
