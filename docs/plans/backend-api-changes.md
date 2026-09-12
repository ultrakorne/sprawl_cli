# Backend API changes — CLI consolidation

Written from the CLI side — the CLI is the only known consumer of the retired routes, but **verify against the web app / LiveView before deleting anything.**

---

## Part 1 — Add: `GET /api/v1/checklist_items/:id`

### Why it's needed

Every single-item verb already takes a bare item id — `checklist check 203`, `state`, `pr`, `delete`, `notes`. But there is **no way to read that item back**. The only single-item read is `GET /checklist_items/:id/notes`, which returns the note blob and nothing else — no title, completion, state, or PR number. So an item id can be written to but never inspected, and it can't even be resolved to its parent task.

### Shape

Mirror the element shape that `GET /api/v1/checklist_items?state=…` (the queue index) already returns — same serializer, plus `notes`. No new rendering concepts.

```
GET /api/v1/checklist_items/203
```

```jsonc
{"checklist_item": {
  "id": 203,
  "title": "add the migration",
  "completed": false,
  "position": 2,
  "state": "in_progress",        // wire spelling; the CLI shortens on output
  "pr_number": 412,
  "has_notes": true,
  "notes": "blocked on the backfill\nretry after 2026-08-05",
  "last_actor": {"type": "agent", "id": 7, "emoji": "🦊"},
  "task": {
    "id": 119,
    "title": "Ship the CLI consolidation",
    "project": {"id": 42, "name": "sprawl_cli", "key": "…", "color": "…",
                "github_url": "https://github.com/ultrakorne/sprawl_cli"}
  }
}}
```

### Requirements

- **Envelope**: `{"checklist_item": {…}}` — singular, matching what the write routes already return.
- **`notes` always included.** This is the detail read; there is no `?full` variant. `null` when empty (the CLI's "empty ⇒ null" contract).
- **`task` stub required** — `id`, `title`, `project`. **Required even though `sprawl item <id>` does not display the task**: the stub is what carries the `project`, and the project's `github_url` is what turns `pr_number` into a clickable link. Drop the stub and the PR link silently stops resolving on this route. The stub also rides in the CLI's json output, where it's the only thing tying an item id back to its task.
- **`project.github_url` must be present**, `null` when the project has none. A project without a repo is normal, not an error.
- **Permission inherits from the parent task**, exactly like every other `/checklist_items/*` route: `403` when the caller can't read the parent, `404` when the item doesn't exist. Under a project key, an item outside the confined project is **`403`, not `404`** — matching `GET /tasks/:id`, which `project_key_test.exs:73` already pins. *(An earlier draft of this line said `404`; that was wrong, and the "exactly like every other route" clause is what governs. `isNotFoundAPIError` deliberately does not catch it: a 403 means the item exists and is out of reach, so treating it as an idempotent-delete success would claim a delete that never happened.)*
- **`404` returns the standard error envelope** (`{"error": "not_found"}`) — the CLI's `isNotFoundAPIError` keys on the `error` field, not just the status.
- **No new state vocabulary.** Keep sending `ready_to_pickup` / `in_progress` / `in_review`; the CLI maps to short names at its own boundary.

---

## Part 2 — Retire

### 2.1 `GET /api/v1/tasks/:task_id/checklist` — fully dead

The CLI was the only caller (`ListChecklistItems`). It's replaced by `GET /tasks/:id?full=true`, which returns the same items **plus the project** — the reason for the switch, since the checklist route returns no project and therefore can't produce a PR link.

`?full=true` on this route dies with it.

⚠️ Check the LiveView / web app doesn't hit it.

### 2.2 `GET /api/v1/checklist_items/:id/notes` — fully dead

Only caller was `note show`, which is being removed. The new `GET /checklist_items/:id` returns `notes` inline, and `GET /tasks/:id?full=true` returns them for a whole task.

### 2.3 `PUT /api/v1/checklist_items/:id/notes` — redundant, verify then delete

**`PATCH /api/v1/checklist_items/:id` already accepts `notes`** through the item changeset. Two routes write one field. The CLI is consolidating on the PATCH (`item update --notes`), and the TUI's note editor switches from `SetNotes` to `UpdateChecklistItem` in the same commit.

**Before deleting, confirm the PATCH path has parity:**

- **Size cap.** The docs say the notes endpoint enforces a cap with `422` on overflow. Does the item changeset enforce the same one? If not, **add it there first** — otherwise this deletion silently removes a validation.
- **Empty string clears.** `PUT /notes` treats `""` as a valid clear. The PATCH must behave identically — `--notes ""` is a documented, in-use way to clear a note.
- **Response.** The PATCH returns the full item; the PUT returns `{"notes": …}`. The CLI is fine with the item envelope. Confirm nothing else depends on the narrow one.
- **PubSub.** If the two routes broadcast different events, make sure the PATCH broadcasts whatever the note-editing UI listens for.

If parity doesn't hold and closing the gap is more work than it's worth, **keep the PUT** — say so, and the CLI will point `item update --notes` at it instead.

---

## Part 3 — Not changing

Listed so nothing gets swept up by accident:

- `GET /api/v1/tasks/:id` and `?full=true` — now the backbone of `sprawl task <id>`. **More load-bearing than before.**
- `GET /api/v1/checklist_items?state=` — backs `queue`. The CLI's `queue --full` sends `?full=true` on this route and each item gains `notes` (null when empty), symmetrically with `GET /tasks/:id?full=true`. Landed server-side with the task-grouped response (server PR #39); the grouped shape itself is documented in [items/TECHNICAL.md](../features/items/TECHNICAL.md).
- `PATCH /api/v1/checklist_items/:id/completed` and `/state` — unchanged.
- `POST /api/v1/tasks/:task_id/checklist` — backs `item add`. Unchanged. (Note it's the one surviving route under the `/tasks/:id/checklist` prefix that 2.1 otherwise retires — **don't delete the POST with the GET.**)
- `DELETE /api/v1/checklist_items/:id`, all `/tasks` routes, `/whoami`, `/settings/theme`, `/activity_log` — untouched.

---

## Summary

| Route | Action |
|---|---|
| `GET /api/v1/checklist_items/:id` | **ADD** |
| `GET /api/v1/tasks/:task_id/checklist` | remove (incl. `?full=true`) |
| `GET /api/v1/checklist_items/:id/notes` | remove |
| `PUT /api/v1/checklist_items/:id/notes` | remove **after** confirming PATCH parity (size cap, empty-clear, PubSub) |
| `POST /api/v1/tasks/:task_id/checklist` | **keep** — same prefix, different verb |

Net: **+1 endpoint, −3 endpoints.**

---

## Status — verified against the dev backend, 2026-08-04

| Item | State |
|---|---|
| Part 1 — `GET /checklist_items/:id` | ✅ 200 with the full shape; `404 not_found` for a missing id; `403 forbidden` for an item outside a project key. |
| 2.1 — `GET /tasks/:task_id/checklist` | ✅ retired. |
| 2.2 — `GET /checklist_items/:id/notes` | ✅ retired. |
| 2.3 — `PUT /checklist_items/:id/notes` | ✅ retired; PATCH parity confirmed (envelope required, `""` clears, response echoes `notes`). |
| Part 3 — surviving routes | ✅ all present. `GET /tasks/:id` additionally gained a bodyless `checklist_items` array — beyond this spec, and what `sprawl task <id>` is now built on. |
| `queue --full` | ✅ landed with the task-grouped queue (server PR #39, 2026-09-12); the CLI reads `{tasks: [{…, checklist_items}]}` and `?full=true` inlines `notes`. |
| `/api/v1` catch-all | ⚠️ **regressed.** Retired paths answered `{"error":"not_found"}` earlier today; they now fall through to `Phoenix.Router.NoRouteError` (HTML, or the dev error view under `Accept: application/json`). See [output-formats/TECHNICAL.md](../features/output-formats/TECHNICAL.md) for why the CLI needs the envelope. |

**This doc is disposable.** Once the catch-all row above is closed it can be deleted: every durable contract in it now lives in the feature docs — the 403-vs-404 rule in [auth-and-config](../features/auth-and-config/TECHNICAL.md), the required `task` stub and the `notes` contract in [items](../features/items/TECHNICAL.md), and the 404-envelope requirement in [output-formats](../features/output-formats/TECHNICAL.md). The retired-route list is only of historical interest, and git has it.
