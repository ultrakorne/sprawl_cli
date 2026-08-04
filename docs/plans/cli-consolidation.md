# Plan — CLI consolidation: `task` + `item`

**Status:** ready to execute. Written 2026-08-03 from a design interview; every decision below was made explicitly by the repo owner, not inferred.

**Goal.** The CLI grew five separate renderings of a checklist item (`checklist` table, `checklist --full` blocks, `task --full` card+section, `queue` table, `note show` blob) and two command namespaces (`checklist`, `note`) for one concept. Collapse to **two nouns — `task` and `item`** — plus `queue`, with **one** row renderer behind all of them.

**Prerequisite.** This plan depends on a new backend endpoint. See [`backend-api-changes.md`](backend-api-changes.md) — do that first, or `item <id>` cannot be built.

---

## 1. Target command surface

```
sprawl task <id> [--full]        show task's items          GET /tasks/:id[?full=true]
sprawl task list                                            GET /tasks
sprawl task search <query>                                  GET /tasks/search?q=
sprawl task create                                          POST /tasks
sprawl task update <id>                                     PATCH /tasks/:id
sprawl task due <id> <preset>                               PATCH /tasks/:id/due_date
sprawl task delete <id>                                     DELETE /tasks/:id

sprawl item <id>                 show one item + its note   GET /checklist_items/:id   ← NEW ENDPOINT
sprawl item add <task_id>                                   POST /tasks/:task_id/checklist
sprawl item update <id>          --title / --notes          PATCH /checklist_items/:id
sprawl item check <id>                                      PATCH /checklist_items/:id/completed
sprawl item uncheck <id>                                    PATCH /checklist_items/:id/completed
sprawl item state <id> <s>                                  PATCH /checklist_items/:id/state
sprawl item pr <id> <n>                                     PATCH /checklist_items/:id/state
sprawl item delete <id>                                     DELETE /checklist_items/:id

sprawl queue [--state] [--full]                             GET /checklist_items?state=
```

`checklist` and `note` **cease to exist** — hard removal, no aliases, no deprecation shim.

### Command mapping

| Today | Tomorrow |
|---|---|
| `task <id>` / `task <id> --full` | `task <id>` / `task <id> --full` (rendering changes completely) |
| `checklist <task_id>` | `task <id>` |
| `checklist <task_id> --full` | `task <id> --full` |
| `checklist add <task_id>` | `item add <task_id>` |
| `checklist check/uncheck/update/state/pr/delete <item_id>` | `item check/uncheck/update/state/pr/delete <item_id>` |
| `note show <item_id>` | `item <item_id>` |
| `note set <item_id> <text>` | `item update <item_id> --notes <text>` |
| `note set <item_id> --stdin` | `item update <item_id> --notes -` |
| `queue` | `queue` (shared renderer, gains `--full`) |

---

## 2. Human (`-h`) rendering

### `sprawl task 119 -h`

A two-line header, then the table. **No bordered card** — no box, no meta grid, no project/due/progress.

```
  Ship the CLI consolidation
  outline the v2 API and land it behind a flag

  [x]  ID   STATE  PR    NOTES  TITLE
  ─────────────────────────────────────────────────
  [x]  203  -      #412  yes    add the migration
  [ ]  204  ◐      -     -      write the docs
  [ ]  205  -      -     -      ship it
```

Header rules:

- **Title only — no id.** You typed the id to get here; repeating it is noise.
- Title is bold (`sty.bold`); description is plain, on the following line(s), authored line breaks preserved and long lines wrapped to `outputWidth` — same treatment notes get.
- **Empty description → the title line alone**, then the blank line and the table. The title always shows.
- Project, due date and progress stay out. `task list` already carries all three per task.

Column rules:

- `[x]` first, `[ ]` / `[x]` exactly as today (`checkbox()`), colored by `checkboxStyle`.
- `ID` second, **bare — no `#` prefix**. The `#` now belongs to PR numbers only, and carrying it on both was the confusion.
- `STATE` renders the **TUI's state icon**, not a word. `-` when the item has no state.
- `PR` renders `#412`, **OSC 8 hyperlinked** when the chain resolves (project + `github_url` + number). `-` when unset.
- `NOTES` is `yes` / `-`, driven by `has_notes`. Present with and without `--full`.
- `TITLE` last, unpadded.
- `POS` is **gone** from every view.

### `sprawl task 119 -h --full`

Identical header and table, with each item's note expanded on following lines, indented to start under `TITLE`:

```
  Ship the CLI consolidation
  outline the v2 API and land it behind a flag

  [x]  ID   STATE  PR    NOTES  TITLE
  ─────────────────────────────────────────────────
  [x]  203  -      #412  yes    add the migration
                                note: blocked on the backfill
                                      retry after 2026-08-05
  [ ]  204  ◐      -     -      write the docs
```

- Label is **`note:`** — singular.
- An item with no note gets **nothing** — no `(no notes)` line. That string disappears from the CLI entirely.
- Multi-line notes keep authored line breaks; over-long lines wrap to `outputWidth` as `taskChecklistSection` does today (reuse that wrapping logic, don't rewrite it).

### `sprawl item 203 -h`

One row of the same table, note always expanded. **No task header of any kind** — you asked about an item, you get the item. `item <id>` takes no `--full`; it's the detail view.

```
  [x]  ID   STATE  PR    NOTES  TITLE
  ─────────────────────────────────────────────
  [ ]  203  ◐      #412  yes    add the migration
                                note: blocked on the backfill
                                      retry after 2026-08-05
```

The `task` stub still rides in the **json/toon** payload (§3) — it's free, the endpoint returns it, and `pr_url` is resolved from the project inside it. It is simply not rendered in `-h`.

### `sprawl queue -h`

Shared renderer. Drops `[x]` and `STATE` because both are constant within one queue (every queued item is incomplete by construction, and the state is the query) — the state stays in the heading, as today. Gains `--full`.

```
  ready (3)

  ID   PR    NOTES  TITLE              TASK              PROJECT
  ──────────────────────────────────────────────────────────────
  203  #412  yes    add the migration  119 Ship the CLI  sprawl_cli
```

### Write commands

All write verbs render a **one-line summary**. No cards.

```
$ sprawl task create --title "New thing"      →  ✓ created task 224  New thing
$ sprawl task update 119 --title "Ship it"    →  ✓ updated task 119  Ship it
$ sprawl task due 119 week                    →  ✓ task 119 due 2026-08-09
$ sprawl task delete 119                      →  (unchanged: deletedText)
$ sprawl item add 119 --title "..."           →  ✓ added item 206  ...
$ sprawl item check 203                       →  ✓ checked item 203  add the migration
$ sprawl item state 203 review                →  ✓ item 203 → review
$ sprawl item pr 203 412                      →  ✓ item 203 → PR #412
```

Match `internal/tui/update.go`'s existing `✓ …` transient vocabulary so the CLI and TUI say the same things.

---

## 3. Machine (`json` / `toon`) shapes

### `task <id>`

Envelope stays `{"task": {…}}` and **keeps every task field** — `title`, `description`, `status`, `due_date`, `project`, `checklist_progress`, `created_by`, `last_actor`. Unchanged from today: `-h` shows only title + description, so this is the only place the rest is reachable.

```jsonc
{"task": {
  "id": 119, "title": "…", "description": "…", "status": "in_progress",
  "due_date": null, "project": {…}, "checklist_progress": {"done": 2, "total": 5},
  "checklist_items": [
    {"id": 203, "title": "…", "completed": false,
     "state": "progress",          // short form
     "pr_number": 412,
     "pr_url": "https://github.com/…/pull/412",   // resolved, null when it can't be
     "has_notes": true,
     "notes": "…",                 // only when --full
     "last_actor": {…}}
  ]}}
```

### `item <id>`

```jsonc
{"checklist_item": {
  "id": 203, "title": "…", "completed": false,
  "state": "progress", "pr_number": 412, "pr_url": "…",
  "has_notes": true, "notes": "…",           // always present — this is the detail view
  "last_actor": {…},
  "task": {"id": 119, "title": "…", "project": {…}}}}
```

### Rules that changed

1. **`state` is short-form on output** — `ready` / `progress` / `review` / `null`. Never `ready_to_pickup` etc. **Input still accepts both** (`item state 203 in_progress` works) — Postel, and it costs one map lookup that already exists.
2. **`pr_url` is emitted everywhere an item is** — `task <id>`, `item <id>`, `queue`. Today only `queue` has it. Null when unresolvable.
3. **`position` is dropped from item payloads.** It drove only the `POS` column, which is gone. *(If any consumer needs ordering, array order already carries it — the server returns items in position order.)*
4. **`notes` rides only on `--full`** for `task <id>` and `queue`; **always present** on `item <id>`.
5. **`has_notes` is always present**, `--full` or not — it's what drives the `NOTES` column.

This breaks the README's claim that json/toon "return the server envelope unchanged." That sentence must be rewritten: the CLI now normalizes state vocabulary and resolves `pr_url`. It was already partly untrue (`queue` added `pr_url`, empty strings became `null`).

---

## 4. Implementation

### 4.1 New shared renderer — do this first

Create `internal/cli/item_render.go` as the **single** source of item rendering. Every view below calls it; no view builds its own row.

```go
// itemCols selects which columns a view shows.
type itemCols struct{ checkbox, state, task, project bool }

func itemTableHeader(c itemCols) []string
func itemRow(it *client.ChecklistItem, project *client.Project, c itemCols) []col
func itemNoteLines(it *client.ChecklistItem, indent int) []string  // "note:" + wrapped body, nil when empty
func itemMap(it *client.ChecklistItem, project *client.Project, withNotes bool) map[string]any

// taskHeader renders the title + description block above the table.
// Used by `task <id>` only — `item <id>` and `queue` render no task header.
func taskHeader(t *client.Task) string
```

`taskHeader` reuses the same wrap-to-`outputWidth` helper as `itemNoteLines`; an empty description contributes no lines, so the no-description case falls out rather than being special-cased.

- `task <id>` → `itemCols{checkbox: true, state: true}`
- `item <id>` → same
- `queue` → `itemCols{task: true, project: true}`

`renderTable` (`style.go:211`) already does layout; feed it. Do **not** add a second table engine.

### 4.2 Files

| File | Action |
|---|---|
| `internal/cli/item_render.go` | **new** — shared row/note/map builders (4.1) |
| `internal/cli/item.go` | **new** — `item` command tree; port bodies from `checklist.go` |
| `internal/cli/checklist.go` | **delete** — after porting |
| `internal/cli/note.go` | **delete** |
| `internal/cli/note_test.go`, `note_handler_test.go` | **delete** |
| `internal/cli/checklist_test.go` | **rewrite** as `item_test.go` |
| `internal/cli/task.go` | strip card rendering; `runTaskShow` renders the shared table |
| `internal/cli/queue.go` | swap bespoke rows for the shared renderer; add `--full` |
| `internal/cli/root.go` | drop `newChecklistCmd` / `newNoteCmd`, add `newItemCmd` |
| `internal/cli/style.go` | move state icons in; delete dead helpers (4.4) |
| `internal/client/client.go` | add `GetChecklistItem`; delete `GetNotes` / `SetNotes` |
| `internal/tui/style.go` | export `iconSet` for CLI reuse, or lift to a shared package |
| `internal/tui/msgs.go:314` | `SetNotes` → `UpdateChecklistItem(…, {"notes": …})` |
| `internal/tui/client.go:25` | drop `SetNotes` from the interface |

### 4.3 Code to reuse, not rewrite

- **OSC 8 links** — `internal/tui/view.go:493` `linkPR` + `ansi.SetHyperlink`. Lift to shared code. Keep the ordering constraint documented there: **style first, hyperlink outside**, or lipgloss prints the URL as literal text. There is a regression test for this (`tui/state_test.go:525`) — write the CLI equivalent.
- **State icons** — `internal/tui/style.go` `nerdIcons` / `plainIcons` / `resolveIcons()`. The CLI adopts the same `SPRAWL_ICONS=plain` opt-out. `TestStateIconsAreOneCell` (`tui/state_test.go:59`) must gain a CLI counterpart — a two-cell glyph shears every row.
- **Note wrapping** — the `ansi.Wrap` block in `taskChecklistSection` (`task.go:690-727`). Move into `itemNoteLines`, delete the original.
- **`client.PRURL`** (`client.go:262`) — unchanged, now called from three places.
- **Style gating** — `sty.render` / `stylesEnabled`. Structural characters unconditional, color gated. `TestStyling_PreservesPlainLayout` guards this; keep it passing.

### 4.4 Dead code — delete, don't leave

Verify with `go build ./... && go vet ./...` plus a `grep -rn` per symbol; the linter won't catch unused exported funcs.

**`internal/cli/task.go`** — `taskDetailText`, `taskCard`, `taskMetaLines`, `metaGrid`, `metaCell`, `boxValCell`, `cellWidths`, `renderCell`, `taskChecklistSection`, `projectBoxLabel`, consts `metaKeyGap` / `metaColGap`.

> `taskCard` also holds the two-space-indent description rendering (`task.go:580-587`). That behaviour **survives** — lift it into `taskHeader` before deleting the function.

**`internal/cli/style.go`** — `renderTitledBox` (only caller was `taskCard`), `checkboxGlyph` (only caller was `taskChecklistSection`).

**`internal/cli/checklist.go`** (whole file) — `fullChecklistText`, `checklistItemsText`, `checklistItemLine`, `renderChecklistItem`, `checklistMaps`, `itemTrailer`, `notesFlag`, `prCell` → superseded by `item_render.go`. Keep and port: `parseState`, `parsePRNumber`, `stateAliases`, `stateLabel`, `stateStyle`, `nilIfEmpty`, `nilIfZero`, `checkbox`.

**`internal/client/client.go`** — `GetNotes`, `SetNotes`, `notesEnvelope`, `emptyNotesToNil` (verify no other caller first).

**Also check:** `internal/cli/output_test.go` and `root_test.go` for assertions on removed commands; `internal/tui/` for anything that consumed `ChecklistItem.Position`.

### 4.5 Tests

- Delete `note_test.go`, `note_handler_test.go`; rewrite `checklist_test.go` → `item_test.go`.
- New: `item_render_test.go` — one table of cases proving `task <id>`, `item <id>` and `queue` produce **byte-identical cells** for the same item. This is the test that keeps the consolidation from rotting.
- New: `taskHeader` cases — with and without a description (empty must yield the title line and nothing more), and a description long enough to wrap.
- New: CLI OSC 8 test — link present when project has `github_url`, **absent** when it doesn't, escapes balanced under truncation.
- New: state icon width test (mirror `tui/state_test.go:59`).
- Update every `--format=json` assertion for: `state` short form, `pr_url` added, `position` removed.
- Keep `TestStyling_PreservesPlainLayout` green — strip ANSI, layout must be unchanged.

---

## 5. Docs & skill — part of the same commit

Nothing may ship referring to `checklist` or `note` as commands.

| File | What to do |
|---|---|
| `README.md` | Rewrite the command table (~line 110-135). Fix the "server envelope unchanged" claim (§3). Rewrite the `--full` paragraph (~line 154) — it now gates fetch *and* render. Update all `### checklist …` / `### note set` examples. |
| `AGENTS.md` | Update any command references in the credential/usage examples. |
| `docs/INDEX.md` | Feature table: `checklists` row → items; drop `note show / set` from its description. |
| `docs/CONTEXT.md` | **Already updated** — item/note/state/queue entries and two new flagged ambiguities. Verify nothing else drifted. |
| `docs/features/checklists/` | Rename to `items/`; rewrite `DESIGN.md` + `TECHNICAL.md`. The endpoint inventory in `TECHNICAL.md` is now wrong in every row. |
| `docs/features/tasks/` | `task <id>` is now a title/description header plus the shared item table, not a card. Rewrite the rendering section. |
| `docs/features/item-state-and-pr/` | State is short-form on the wire out; `checklist state/pr` → `item state/pr`. |
| `docs/features/output-formats/` | The `{notes:"…"}` envelope is gone; `state` normalization and `pr_url` are new contract rules. |
| `docs/features/interactive-tui/` | Note the `SetNotes` → `UpdateChecklistItem` switch. |
| `skills/sprawl/SKILL.md` | See below — the biggest single doc job. |
| `agents/{claude,codex,opencode}/sprawl-bookkeeper.md` | Update every command reference. |

### `skills/sprawl/SKILL.md` specifically

529 lines, and it's the file agents actually read — get it right.

- Lines 37, 42, 110, 116, 125-131, 188, 230-236, 250-256, 263-265 all name `checklist` / `note` commands.
- **Lines 272-281 must be inverted, not renamed.** They currently tell agents that `note show --format=text` emits the raw body and that reaching for `jq` is "pure overhead." After this change `item <id> -h` renders a table, so extracting a note body *becomes* `sprawl item 203 --format=json | jq -r '.checklist_item.notes'`. The guidance flips.
- Line 215 ("Everything is a checklist item") is conceptually still true — keep it, adjust vocabulary to **item**.
- Add the read-cost guidance: `task <id>` is cheap, `task <id> --full` pulls every note body — reach for `--full` when you need the notes, `item <id>` when you need one.

---

## 6. Execution order

1. **Backend first** — ship `GET /api/v1/checklist_items/:id` per `backend-api-changes.md`. Nothing else can land without it.
2. `item_render.go` + its tests. Pure addition, nothing consumes it yet.
3. `client.GetChecklistItem`.
4. `item.go` — full command tree. Both trees briefly coexist; don't wire `item` into root yet if you want to bisect.
5. Repoint `task <id>` and `queue` at the shared renderer.
6. Wire `item` into root; delete `checklist.go`, `note.go`, and their tests.
7. TUI: `SetNotes` → `UpdateChecklistItem`; delete the client methods.
8. Dead-code sweep (§4.4). `go build ./... && go vet ./... && go test ./...`.
9. Docs + skill + agent files (§5).
10. Manual pass against `sprawl_dev`: every command in §1, in `-h`, `json`, and `toon`; piped and on a TTY; with and without `SPRAWL_ICONS=plain`; on a project with and without `github_url`.

---

## 7. Accepted trade-offs

Decided deliberately — don't relitigate mid-execution.

- **A task's project, due date and progress have no `-h` surface on `task <id>`.** The header carries title + description only. All three remain in `task list` and in `--format=json`. *(Description was originally cut too; the header was added back specifically to rescue it.)*
- **`item <id> -h` shows no parent-task context.** An item id is opaque, so nothing on screen says which task it belongs to — `--format=json` carries the `task` stub if you need it. Decided deliberately against the `queue` precedent, which does show a TASK column.
- **Piping a raw note body now needs `jq`.** `note show`'s verbatim output is gone.
- **`state` output diverges from the server's own values.** A consumer comparing CLI json against a PubSub payload or the web app sees `progress` vs `in_progress`. Deliberate — it kills the item-state/task-status collision.
- **Hard removal breaks any unversioned script.** Accepted: both known consumers (the skill, the bookkeeper agent) ship from this repo and are updated in the same commit.
- **`item <id>` costs a backend endpoint.** No client-side workaround exists — an item id cannot be resolved to its task without one.
