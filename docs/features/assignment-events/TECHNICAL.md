# Assignment Events — Technical

## Architecture

`internal/client/events.go` owns the transport: `ListAssignmentEvents` (the feed) and `GetAssignmentEvent` (one event), both under the workspace path prefix. `internal/cli/events.go` holds the `events` command tree and the `eventWatcher` loop. The inventory is the queue's read with an extra filter: `client.QueueFilter` / `ListQueue` in `internal/client/client.go`, flags in `internal/cli/queue.go`.

## Where things live

| File | Role |
|------|------|
| `internal/client/events.go` | `AssignmentEvent`, `AssignmentEventPage`, `ListAssignmentEvents`, `GetAssignmentEvent` |
| `internal/client/client.go` | `QueueFilter` / `ListQueue`; `ChecklistItem.AssignmentRevision` |
| `internal/cli/events.go` | `events watch` / `show`, `eventWatcher` (`run` / `head` / `poll` / `emit`), retry classification |
| `internal/cli/queue.go` | `--assignee`, `queueFilterFromFlags`, assignee headings |
| `internal/cli/output.go` | `invalid_cursor` guidance |

## Wire

- `GET /api/v1/assignment_events` with no params returns the head: no events, no wait, `next_cursor`. With `after=<cursor>&wait=<s>` it answers `{"events": [event + "cursor"], "next_cursor", "has_more"}` as soon as anything follows the cursor, or after `wait` seconds with an empty page. Expired cursor → `410 cursor_expired`; malformed or foreign → `422 invalid_cursor`.
- `GET /api/v1/assignment_events/:id` — `GetAssignmentEvent` decodes `{"assignment_event": …}`, `{"event": …}` or a bare object, and strips an `ae_` prefix.
- `GET /api/v1/checklist_items?assignee=me[&state=…][&full=true]` — same grouped shape as the by-state queue; neither `state` nor `assignee` is a `422 state_required`.

## Noteworthy

- **The feed request outlives the client's 15 s timeout by design.** `ListAssignmentEvents` copies the client with an `http.Client` timed at `wait + 15 s`, sharing the transport; every other call keeps the default.
- **Resume from `next_cursor`, not the last event's cursor.** The server advances `next_cursor` past events the scope filters out (another workspace, outside a confined project), so following it keeps an idle cycle query-free server-side. An event line's cursor falls back to `next_cursor` only when the event lacks its own and is the last on the page.
- **Retry vs fatal is one predicate**, `isTransientFeedErr`: anything that isn't a server answer (network, timeout, truncated or undecodable body) plus `408` / `429` / `5xx`. `410` is checked first and has its own path: fetch the head, emit `cursor_expired`, continue. The backoff restarts at 1 s on every `poll` call, so a recovered connection doesn't inherit a long delay.
- **A head without a cursor is fatal** (`errNoCursor`): polling `""` again would return immediately, forever.
- **`--wait` is 1–30, checked before any request.** `0` is legal on the wire but would turn the watch into a busy loop.
- **One `Write` per line, then `Flush` when the writer has one.** `os.Stdout` is unbuffered, so the flush matters only for wrapped writers; tests use a recorder that counts both.
- **Shutdown rides `main`'s signal context.** `cmd/sprawl/main.go` cancels the root context on SIGINT / SIGTERM; the watch returns `nil` whenever its context ended, so an interrupt mid-request or mid-backoff (`sleepCtx`) exits 0 without an error envelope. Stream lines and retry diagnostics go through `writeCtx`, which writes on a goroutine and gives up when the context ends, so a write blocked on a full pipe can't hold the exit; returning from `main` ends the process with that goroutine. After the first signal `main` restores default signal handling (`context.AfterFunc(ctx, stop)`), so a second one kills.
- **Event lines are typed structs, not maps**, so keys print in contract order (`type`, `cursor`, `event`; event fields as the server documents them) and the event's own `cursor` is omitted — the line carries it.
- **Assignee-only queues keep `STATE`.** `queueText` drops the column only when the state is the query.
- `assignment_revision` rides on every item map; `null` from a server that predates it.

## Tests

`internal/client/events_test.go` and `internal/cli/events_test.go` drive an `httptest` feed scripted per request; the script cancels the watch's context when it runs out, standing in for SIGINT. Keep-alives are off in those servers so a dropped-connection step can't be silently replayed by the transport onto the next step.
