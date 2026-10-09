# Items

Checklist items are the actionable children of a task, each with one optional note and assignee. The CLI reads and writes them through `item`, while task detail and queue share its renderer. Assignment uses eligible workspace actors; permission is inherited from the parent task.

## Documents

| Document | Purpose |
|----------|---------|
| [DESIGN.md](DESIGN.md) | Item commands, assignment, notes, and shared output |
| [TECHNICAL.md](TECHNICAL.md) | Request ownership, rendering, and field-presence contracts |
| [Workspaces](../workspaces/INDEX.md) | Selection and actor discovery |
| [Item state and PR](../item-state-and-pr/INDEX.md) | State, completion, PR number, and queue |
| [Assignment events](../assignment-events/INDEX.md) | Assignment feed, event read, and `queue --assignee me` |
