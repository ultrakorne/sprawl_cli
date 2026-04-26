# Checklists & Notes

Commands for checklist items under a task and their free-form notes blob: `checklist <task_id>` (list), `checklist add`, `checklist check`, `checklist uncheck`, `checklist update`, `checklist delete`, and the top-level `note show` / `note set`. Permission is enforced on the *parent task* — items and notes inherit.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Commands, UX, check / uncheck split, notes-via-stdin |
| [TECHNICAL.md](TECHNICAL.md) | Source files, wire shapes, subcommand routing |
