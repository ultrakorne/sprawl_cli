# Follow your assignments

Your **inventory** is `sprawl queue --assignee me`: every incomplete item
assigned to your agent key, in any state (`--state` narrows it). **Assignment
events** tell you when it changes. Both answer only for your own key, in the
workspace you work in.

To **reconcile** is to run the inventory and treat its result as the truth.
Events from before a reconcile point are never replayed.

## Run a listener

`events watch` runs until interrupted and prints one JSON line per change. A
listener is a pipeline, so pass `--format=json`: a `SPRAWL_OUTPUT=text` in the
environment turns the stream into human lines.

You own the cursor; sprawl never stores it. A line is **handled** once you
have acted on it and then persisted its `cursor`.

1. Start fresh with `sprawl events watch --format=json`. The first line is
   `{"type":"start","cursor":"…"}`: persist that cursor, *then* reconcile, so
   an assignment made in between lands in one or the other.
2. Handle each `assignment_event` line (see [Events](#events)).
3. On restart, resume with `sprawl events watch --format=json --after "$cursor"`.
   A crash replays a line rather than losing one, so handle each event `id`
   idempotently.
4. On `{"type":"cursor_expired","cursor":"…"}`, persist that cursor and
   reconcile; the stream carries on. A cursor expires 30 days after the line
   that carried it, however recently the listener polled, so a long-idle
   listener meets this on restart.

## Events

```json
{"type":"assignment_event","cursor":"…","event":{"id":123,"change":"assigned","assignment_revision":4,"workspace_id":7,"task_id":1001,"checklist_item_id":5555,"actor":{"type":"user","id":3},"occurred_at":"…"}}
```

- `assigned`: the item is now yours. Read it with `sprawl item <checklist_item_id>`
  (or `sprawl task <task_id> --full` for the whole task) before acting.
- `removed`: the item was taken off you or reassigned. Stop work on it.

Events carry ids only. An event is stale when its `assignment_revision` is
below the item's current `assignment_revision`; the item's `assignee` is what
holds now.

`sprawl events show <id>` reads one event back. It is the evidence for who
assigned an item: the event's `actor` stays fixed, while the item's
`last_actor` moves with every later edit.

## Exits

The CLI retries network errors, timeouts, `408`, `429` and `5xx` itself
(diagnostics on stderr). Any other refusal ends the stream with a non-zero
exit and the error envelope as the last stdout line:

- `401`, `403`: as in the skill's Preflight.
- `404`: the server predates assignment events. Tell the user.
- `422 invalid_cursor`: a malformed cursor or another key's. Drop `--after`
  and start again at step 1.
