# Follow your assignments

Your **inventory** is every incomplete item assigned to your agent key, in any
state; **assignment events** tell you when it changes. Both answer only for
your own key, in the workspace you work in.

```bash
sprawl queue --assignee me                     # the inventory, grouped by task
sprawl queue --assignee me --state ready       # …narrowed to one state
sprawl events watch                            # stream changes as JSON lines
sprawl events watch --after "$cursor"          # resume after a line you handled
sprawl events show <id>                        # one event: who assigned it, when
```

## The stream

`events watch` runs until interrupted and prints one JSON line per change:

```json
{"type":"start","cursor":"…"}
{"type":"assignment_event","cursor":"…","event":{"id":123,"change":"assigned","assignment_revision":4,"workspace_id":7,"task_id":1001,"checklist_item_id":5555,"actor":{"type":"user","id":3},"occurred_at":"…"}}
{"type":"cursor_expired","cursor":"…"}
```

- `assigned`: the item is now yours. Read it with `sprawl item <checklist_item_id>`
  (or `sprawl task <task_id> --full` for the whole task) before acting.
- `removed`: the item was taken off you or reassigned. Stop work on it.
- `start` (no `--after`) and `cursor_expired`: events before this point are
  not replayed. Run `sprawl queue --assignee me` and treat it as the truth.

Events carry ids only. An event is stale when its `assignment_revision` is
below the item's current `assignment_revision`; the item's `assignee` is what
holds now.

## Cursor discipline

You own the cursor; sprawl never stores it. Persist a line's `cursor` **after**
you have handled that line, and restart with `--after <cursor>`. A crash then
replays a line rather than losing one, so handle each event id idempotently.
Persist the `start` cursor before running the inventory, so an assignment made
in between lands in one or the other.

A cursor expires 30 days after the line that carried it, not after the last
poll: restarting with one from a line over 30 days old gets `cursor_expired`.
Treat that like `start` and run the inventory.

## Exits

Network errors and `5xx` are retried by the CLI (diagnostics on stderr). It
exits non-zero on `401` (ask the user to `sprawl login`), `403`, `404`, and
`422 invalid_cursor` (malformed or another key's cursor: drop `--after`, start
fresh, run the inventory). The error envelope is the last stdout line.
