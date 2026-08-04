# Items

Commands for the checklist items under a task, and the note each one carries: `item <id>` (show), `item add`, `item update`, `item check`, `item uncheck`, `item delete`. Permission is enforced on the *parent task* — items inherit it.

`item` is one of the CLI's two nouns, the other being `task`. A note is a **field of an item**, not an object of its own, so there is no `note` command: `item update --notes` writes it and `item <id>` reads it back. The `checklist` and `note` command trees this replaced were removed outright — no aliases, no deprecation shim.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Commands, UX, the check / uncheck split, the note as a field |
| [TECHNICAL.md](TECHNICAL.md) | Source files, the shared renderer, wire shapes, subcommand routing |

## See also

- [tasks](../tasks/INDEX.md) — `task <id>` renders the same item table defined here, under a title/description header.
- [item-state-and-pr](../item-state-and-pr/INDEX.md) — the `item state` / `item pr` sibling subcommands, the two extra fields they write, and the cross-task `queue` read. Completion interacts with state: checking an item clears it.
- [output-formats](../output-formats/INDEX.md) — the normalization every item payload goes through (short-form `state`, added `pr_url`, dropped `position`).
