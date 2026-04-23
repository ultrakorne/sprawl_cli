---
name: sprawl
description: >
  Collaborate on shared tasks, checklists, and notes with the human and other
  agents via the sprawl CLI. Use this skill whenever the user asks you to look
  at "my tasks", "the backlog", "what's assigned to me", to check off a
  checklist item, leave a note for another agent, create a task, or coordinate
  work with another agent — anything that reads or writes the sprawl task
  space. The skill wraps the `sprawl` CLI; a user-provided `SPRAWL_AGENT_SECRET`
  (env var or `-s` flag) scopes what the caller can see and change.
allowed-tools: Bash(sprawl:*), Bash(which:*), Bash(command:*), Bash(printenv SPRAWL_AGENT_SECRET), Bash(test:*)
---

# sprawl

Shared task space for the human owner and their agents. Every agent has its own
secret; the server resolves per-agent permissions so you only see and touch
what you're allowed to. The CLI is a thin HTTP client — the server is the
source of truth for validation and permissions, so trust its error codes.

## When to use this skill

- The user refers to tasks, checklists, notes, or the backlog in sprawl.
- The user asks you to coordinate with another agent or leave them context.
- You need durable state that survives beyond this conversation (a todo, a
  hand-off note, a status update another agent can pick up).
- The user mentions `sprawl` directly, or points at a task id.

Do **not** use this skill for one-off in-conversation todos — use TaskCreate
for those. sprawl is for collaboration across sessions and agents.

## Preflight

**Skip it.** Just run the command the user asked for. 99 % of the time `sprawl`
is on PATH, the secret is set, and the token is valid — probing first only
burns tokens and a round-trip.

Only diagnose when a real call fails. Map the failure, then act:

- **`sprawl: command not found`** — not installed. Read `SETUP.md` in this
  skill's directory and walk the user through it.
- **`SPRAWL_AGENT_SECRET not set`** (or similar pre-flight error from the CLI)
  — ask the user to export it in this shell or pass `-s <value>` per command.
  If they don't have one, point them at `SETUP.md`.
- **HTTP 401** — token bad or missing. Ask the user to re-run `sprawl login`
  (interactive — don't try it yourself).
- **HTTP 403** — secret is scoped out of this action. Don't retry; tell the
  user which action you lack permission for. See
  [Permission model](#permission-model-how-to-read-errors).
- **Anything ambiguous** — *then* run `sprawl health --format=json` to
  separate "CLI/network broken" from "auth broken".

**Never** write the secret to a file, commit it, echo it back to the user, or
print it in a command you run. If you show a command using `-s`, redact the
value.

## Credential model

Two credentials, resolved per request:

1. **Token** — user's device-flow token, managed by `sprawl login`. You don't
   touch this. Lives in `~/.config/sprawl/config.toml` (mode 0600) or
   `SPRAWL_TOKEN`.
2. **Agent secret** — `SPRAWL_AGENT_SECRET` env var (preferred) or
   `-s <value>` / `--agent-secret <value>` flag. **Never persisted by sprawl.**
   Prefer the env var for long-lived shells; the flag leaks via `ps auxe` and
   shell history.

If either is missing the CLI fails before making the HTTP call.

## Permission model (how to read errors)

Your agent secret resolves one of four scopes per task / project:
`none` · `read` · `write` · `write_create`. Resolution order on read/write is
task override → project override → `agent_keys.default_permission`.

| HTTP | Meaning | What to do |
|---|---|---|
| `200` | Success | Carry on. |
| `401` | Token bad / missing | Ask user to re-run `sprawl login`. |
| `403` | Your key is scoped out of this action | Don't retry. Tell the user you lack permission and which action. |
| `404` | Not visible to you, or genuinely gone | Treat as "not available to me". Don't assume it exists and retry. |
| `422` | Validation (e.g. empty search query) | Fix the input. |

Non-owner agents only see tasks their key resolves at least `:read` on, so
`task list` is already filtered. **Do not** loop retrying on `403` — the
server won't change its mind.

## Output formats

`--format` is a persistent flag: `text | json | toon` (default `toon`).
`SPRAWL_OUTPUT` sets a session default.

**Default to omitting `--format` for your own reads.** Toon is 30–60 % fewer
tokens than json and lossless, so when the output is just coming back into
your context for you to eyeball (list, show, checklist, search), let it
default. Passing `--format=json` to read a task list is pure token waste.

Only override when you have a specific reason:

- `--format=json` — only when you're actually piping to `jq` or another
  parser. Not for "I want to read it myself".
- `--format=text` — when you're showing the output to the user (tabwriter
  tables, multi-line detail views).

Errors in `json` / `toon` come as a structured envelope:

```json
{"status": "error", "error": "<message>", "http_status": 403}
```

`http_status` is omitted for pre-flight errors (e.g. missing secret).

## Task shape (house style)

The owner's convention for this task space. Follow it unless the user says
otherwise — it's what makes the board readable across agents and sessions.

- **Everything is a checklist item.** The task is a lightweight container;
  the actual work is the items underneath it.
- **Title: short.** Server cap is **30 characters** (enforced — longer fails
  with a 422). Aim well under that: a handful of words, no punctuation
  filler. If you can't say it in a title, the thing is probably two tasks.
- **Skip the description.** Default to no `--description`. Don't summarise
  the task there — summarise it in the checklist.
- **If you must use a description**, keep it brief. Server cap is **255
  characters** (enforced at the DB layer — longer currently surfaces as a
  500, not a clean 422).
- **Itemize work as checklist items**, titles kept short like task titles.
  One discrete thing per item. If an item reveals subwork, add a new item,
  don't cram it into the title.
- **Long context → item notes.** When something needs paragraphs — a
  rationale, a block of status, a link dump, a hand-off — attach it as a
  note on the relevant checklist item via `sprawl note set`. Notes are
  free-form and unbounded; that's the channel for anything that won't fit
  in a title.

So the usual create flow is: `task create --title "..."` (no description),
then one or more `checklist add <task_id> --title "..."`, then
`note set <item_id>` only on items that need the extra context.

## Command reference

All `/api/v1/*` commands honour `--format` and the credential model above.
`login` is interactive and always plain text.

### Discovery (reads)

Omit `--format` — default toon is what you want here (see [Output formats](#output-formats)).

```bash
sprawl task list
sprawl task show <id>
sprawl task search "<query>"                   # case-insensitive substring on title
sprawl checklist <task_id>                     # list checklist items for a task
sprawl note show <item_id>                     # raw notes blob; empty is valid
```

### Writes — tasks

Wire body: `{"task": {...}}`. Accept explicit flags or `--from-json <path|->`;
explicit flags override fields parsed from JSON. Title max **30 chars**,
description max **255 chars** — see [Task shape](#task-shape-house-style)
for why you usually skip description entirely.

```bash
# Create (projectless — needs default_permission=write_create):
sprawl task create --title "draft spec"

# Create attached to a project (needs write_create at project scope):
sprawl task create --title "wire up CI" --project-id 42

# Create from a JSON template, tweak one field:
echo '{"title":"draft","project_id":42}' \
  | sprawl task create --from-json - --title "final"

# Update title (server ignores project_id on update):
sprawl task update 17 --title "renamed"
sprawl task update 17 --description ""        # explicit clear (not "flag unset")
```

### Writes — checklists

Wire body: `{"checklist_item": {...}}`. Server assigns `position` on add.
Permission is checked on the **parent task**, not per item.

```bash
sprawl checklist add <task_id> --title "write migration"
sprawl checklist add <task_id> --title "deploy" --notes "run after backfill"

sprawl checklist check <item_id>              # idempotent
sprawl checklist uncheck <item_id>            # idempotent
sprawl checklist update <item_id> --title "renamed"
```

Use `check` / `uncheck` for completion — `update` doesn't mutate it. The split
avoids a GET-then-PATCH race when you don't know current state.

### Writes — notes (hand-off channel)

Notes are per-checklist-item free-form blobs, addressed separately so list
responses stay small. This is the primary place to leave context for another
agent or the human.

```bash
sprawl note show <item_id>                    # read
sprawl note set <item_id> "blocked on PR #418"
sprawl note set <item_id> ""                  # clears notes
cat status.md | sprawl note set <item_id> --stdin
```

`--stdin` and the positional arg are mutually exclusive — passing both is a
local error before any HTTP call.

### Misc

```bash
sprawl version                                # prints version + baked-in API URL
sprawl health --format=json                   # liveness + auth pipeline
sprawl theme get                              # read active UI theme
sprawl theme set tokyo-night                  # owner-only; unknown id → 404
```

## Collaboration patterns

These are the common shapes of work the skill exists for.

### 1. Pick up assigned work

```bash
sprawl task list
# inspect output, pick tasks you can act on
sprawl task show <id>
sprawl checklist <id>
```

Filter by what's visible — your key already scopes the list server-side.

**Before you start an item, read its note.** The checklist response shows
whether an item has a note but not its content. If `has_notes` is true (or
the text output shows a note marker), pull it — that's where the previous
agent or the human left the hand-off context:

```bash
sprawl note show <item_id>
```

Skipping this is how you redo work someone already did, or miss a blocker
they flagged.

**When you finish an item, check it off immediately** — see
[§2](#2-make-progress-on-a-checklist). Don't wait until the end of the task:
other agents and the human are watching the board live, and an unchecked
item reads as "still to do".

### 2. Make progress on a checklist

Mark items as you finish them; don't batch. Other agents and the human see
the state live.

```bash
sprawl checklist check 203
```

If an item reveals subwork, add a child item rather than stuffing it into the
note:

```bash
sprawl checklist add <task_id> --title "backfill legacy rows"
```

### 3. Leave context for another agent or the human

Use the item's notes blob. Append by reading first if you need to preserve
prior content (`note set` is a full replace):

```bash
prev=$(sprawl note show 203)
printf '%s\n\n---\n\n%s\n' "$prev" "new status from <your-agent-name>: blocked on PR #418" \
  | sprawl note set 203 --stdin
```

Sign your additions so the human can tell agents apart.

### 4. Create a new task on behalf of the user

Only if the user asked, and only if your key resolves `write_create`. If you
get a `403`, surface that — don't retry.

```bash
sprawl task create --title "investigate flaky deploy" \
  --description "seen twice this week on main; logs in #ops" \
  --project-id 42
```

### 5. Hand off

Finishing your slice and passing to another agent or the human:

1. `checklist check` the items you finished.
2. `note set` on the next item with a short status + what you couldn't do and
   why (permission, missing info, blocker).
3. Do **not** `task update` the title / description just to log status — that
   rewrites the task. Status belongs in notes or new checklist items.

## Guardrails

- **Never** echo, log, commit, or persist `SPRAWL_AGENT_SECRET`. If you must
  show a command to the user, redact the value.
- **Never** attempt `sprawl login` — it's interactive; ask the user instead.
- **Never** retry `403` responses. Permission won't flip mid-session.
- **Don't** use `task update` as a status channel. Use notes / checklist
  items.
- **Don't** `--from-json` with untrusted input without reading it first — the
  file / stdin is parsed as the full task/item attrs map.
- **Empty strings matter**: `--description ""` and `note set X ""` are
  *explicit clears*, distinct from "flag unset". Use them deliberately.
- **Write only what the user asked for.** The task space is shared with the
  human and other agents; spurious tasks or notes are noise for everyone.
