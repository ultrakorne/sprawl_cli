# Item State & PR Number

Two additive fields on a checklist item — a hand-set **item state** (`ready_to_pickup` / `in_progress` / `in_review`, or none) and a **PR number** linking the item to a GitHub pull request. Together they turn a checklist into a live coordination board: agents claim work, hand it back for review, and the human sees who is on what without asking. Surfaced by `checklist state` / `checklist pr` (writes), the top-level `queue` (the cross-task "what can I pick up?" read), the `s` / `p` keys in the interactive TUI, and as extra columns on every existing checklist read.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | What the fields mean, the state lifecycle, the agent hand-off protocol, command UX |
| [TECHNICAL.md](TECHNICAL.md) | Dedicated route, wire shapes, PR-link resolution, optimistic-update contract |
| [FLOW.mermaid](FLOW.mermaid) | The state lifecycle and how it interacts with completion |

## See also

- [CONTEXT.md](../../CONTEXT.md) — glossary: **item state** vs **task status**, **PR number**, **queue**, **repo URL**.
- [checklists](../checklists/INDEX.md) — the item and note commands these fields ride on.
- [interactive-tui](../interactive-tui/INDEX.md) — the `s` / `p` keys and the state column.
- [tasks](../tasks/INDEX.md) — `task <id> --full` renders the fields, and the task's project supplies the repo URL that resolves PR links.
