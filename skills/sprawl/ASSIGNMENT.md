# Assign, reassign, or clear an item

Assignment names who an item belongs to; item state names where the work is.
Use an item ID for assignment. Tasks have no assignee; `item add` takes the
parent task ID because it creates the item.

1. Discover targets with `sprawl workspace actors --format=json`. It returns
   `{type, id, label, marker}` for eligible people and agent keys. In another
   workspace, use the same `--workspace <id>` on discovery and writes. A
   positional `workspace actors <id>` only queries that roster.
2. Choose the returned pair that matches the intended person or key. Labels
   help selection; the typed ID is the write target. IDs can overlap between
   types. Use `agent_key`, even though audit `last_actor.type` can say `agent`.
3. Run the requested action and inspect the returned `checklist_item.assignee`:

   ```bash
   sprawl item assign 203 user:3
   sprawl item assign 203 agent_key:7          # also reassigns
   sprawl item unassign 203
   sprawl item add 119 --title "Write docs" --assignee agent_key:7
   sprawl item update 203 --title "Review docs" --assignee user:3
   sprawl item update 203 --unassign
   ```

`--from-json` accepts an `assignee` object or null. Omission preserves the
assignment (creates unassigned); null clears it; `{type, id}` assigns one actor.
Explicit assignment flags override JSON; `--assignee` and `--unassign` conflict.
IDs are decimal integers from 1 to 2147483647.

To find an actor's assigned work, match both type and ID in item responses.
`queue` filters by state, so it shows only assignments in that state. For
assignments across states, list tasks and read their item lists with `task <id>`.
Read-only callers can inspect stored pairs without requesting the write-only roster.

Assignment preserves completion, state, notes, and PR number. Reads show the
stored pair or null without fetching a roster. A stored pair absent from a
fresh roster can be stale; treat it as unassigned when selecting work.
Discovery requires write access and does not reserve eligibility. On
`invalid_assignee`, refresh discovery before proposing another target; a
refused write leaves the action incomplete. Creation requires a backend with
create-time assignment support; verify the returned pair before claiming it
was assigned.
