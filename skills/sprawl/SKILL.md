---
name: sprawl
description: >
  Collaborate on shared tasks, items, and notes with the human and other
  agents via the sprawl CLI. Use this skill whenever the user asks you to look
  at "my tasks", "the backlog", "what's assigned to me", to pick up work that's
  ready, check off an item, mark something in progress or in review,
  attach a PR number, leave a note for another agent, create a task, or
  coordinate work with another agent — anything that reads or writes the sprawl
  task space.
license: 'MIT'
metadata:
  version: 0.6.0
allowed-tools: Bash(sprawl:*), Bash(which:*), Bash(command:*), Bash(printenv SPRAWL_PROJECT_KEY), Bash(printenv SPRAWL_WORKSPACE), Bash(test:*)
---

# sprawl

Shared task space for the human owner and their agents. You work through a
**project key**: every call is confined to one project, so you only see and
touch that project's tasks. The CLI is a thin HTTP client — the server is the
source of truth for validation and permissions, so trust its error codes.

## When to use this skill

- The user refers to tasks, items, notes, or the backlog in sprawl.
- The user asks you to coordinate with another agent or leave them context.
- You need durable state that survives beyond this conversation (a todo, a
  hand-off note, a status update another agent can pick up).
- The user mentions `sprawl` directly, or points at a task id.

Do **not** use this skill for one-off in-conversation todos — use TaskCreate
for those. sprawl is for collaboration across sessions and agents.

## Preflight

**For reads, skip it.** `task list` / `task <id>` / `item <id>` / `queue`
are already filtered server-side to what your key can see, so just run them.
Probing first only burns tokens and a round-trip.

**For writes, run `sprawl whoami` once.** Before the first `task create`,
`task update`, `item add`, `item update`, or any other mutation in this
session, check your scope — see [Before you write](#before-you-write--check-your-scope).
A wasted `403` on a write is fine; a wasted *chained* write is the trap,
because `&&` swallows the error and the follow-up commands run with empty
inputs.

Only diagnose other failures when a real call hits one. Map the failure,
then act:

- **`sprawl: command not found`** — not installed. Read `SETUP.md` in this
  skill's directory and walk the user through it.
- **`no project key or agent secret set`** (pre-flight error from the CLI)
  — no project key is configured (the message also offers an agent secret;
  ignore that half, you work by project key). Ask the user to export `SPRAWL_PROJECT_KEY`
  in this shell (or pass `-p <key>` per command). If they don't know their
  key, point them at `SETUP.md`.
- **HTTP 401** — token bad or missing. Ask the user to re-run `sprawl login`
  (interactive — don't try it yourself).
- **HTTP 403 `invalid_project_key`** — the key is a typo; no project of theirs
  has it. Ask them to check the Project key field on the project's side panel
  in `/tasks`.
- **HTTP 403 `forbidden`** — a permissions boundary, not a typo: the target
  lives outside your project. Don't retry; tell the user which action you lack
  permission for. See
  [Permission model](#permission-model-how-to-read-errors).
- **HTTP 403 `workspace_mismatch`** — a `SPRAWL_WORKSPACE` (or `-w`) in this
  shell names a different workspace than the project key's. The key pins the
  workspace; ask the user to unset the variable. See
  [Workspace](#workspace--the-key-already-picks-it).
- **Anything ambiguous** — *then* run `sprawl whoami --format=json` to
  separate "CLI/network broken" from "auth broken". A 200 also tells you
  which agent and scope the server thinks you are.

The project key is **not** a secret — it's the key shown on the project's side
panel, so printing it or including it in a command you show the user is fine.
Never go looking for or handling other credentials: the bearer token is the
user's, managed by `sprawl login`, and is none of your business.

## Before you write — check your scope

Run `sprawl whoami` once at the start of a write session. Reads don't need
this (the server already filters them), but writes need to know your scope
upfront — otherwise you'll learn it the hard way through a `403`, and any
chained follow-up commands will run with empty inputs and produce a confusing
partial state.

```bash
sprawl whoami
# agent:
#   ...
# workspace: Hobby #1                      # ← the canvas the project lives on (the key picks it)
# project:                                 # ← the project your key confines you to
#   key: hobby
#   name: Hobby
#   level: write_create                    # ← your scope *there* — the only one that matters
```

What each scope unlocks: `read` → list/show only · `write` → edit existing
tasks/items · `write_create` → also create new ones.

`project.level` is your answer for every read and write this session, and
creates land in that project automatically.

- `write_create` — go ahead.
- `write` — you can edit existing tasks and items, but **don't try to
  create.** Tell the user you lack `write_create` on this project.
- `read` — reads only. Say so instead of attempting a write.
- `none` — the key is valid but you have no access to that project. Stop and
  tell the user; don't hunt for tasks that will never appear.

Surfacing this from `whoami` saves the wasted round-trip and the confusing
partial state from chained follow-ups.

For edits (`task update`, `item check` / `uncheck` / `update`),
`write` is enough on the target's project. You can usually skip the
whoami check for edits if you've already established scope earlier in the
session — but if this is the first write of the session, just check.

**Don't chain writes with `&&` to capture the new id.** Run
`task create` on its own, read the id from its output, *then* add items. If the create returns 403, an `&&` chain blows past it with an empty
id and the next command silently no-ops on stderr — exactly the failure
mode that surfaces as "(no output)".

```bash
# Right — split so each step's outcome is visible:
sprawl task create --title "quick reminders"
# → note the id from the output
sprawl item add <id> --title "print something"
sprawl item add <id> --title "talk to Antti about AI tools"

# Wrong — masks 403 on create, follow-ups run on empty id:
task_json=$(sprawl task create --title "quick reminders" --format=json) \
  && id=$(printf '%s' "$task_json" | jq -r '.task.id') \
  && sprawl item add "$id" --title "..."
```

## Project key — the scope you work in

Two things travel with every request:

1. **Token** — the user's device-flow token, managed by `sprawl login`. You
   don't touch this. Lives in `~/.config/sprawl/config.toml` (mode 0600) or
   `SPRAWL_TOKEN`.
2. **Project key** — `SPRAWL_PROJECT_KEY` env var (usual case: the user
   exports it once per repo / shell) or `-p <key>` / `--project-key <key>` per
   command. It's the 2–32 char key on the project's side panel in `/tasks`,
   matched case-insensitively.

Without a project key the CLI stops before the HTTP call. If that happens,
ask the user for the key — don't hunt for other ways to authenticate.

What the key does, server-side, on every call:

- `task list` / `task search` / `activity` come back **already filtered** to
  that project. Don't filter again, and don't tell the user "that's all the
  tasks" — it's all the tasks *in this project*.
- `task create` lands in that project. **Never pass `--project-id`** — you
  don't need the id, and naming a different project is rejected.
- Anything outside the project is out of reach: a task or item id from another
  project answers **`403`**, not its content — reads and writes alike. `404`
  means the id doesn't exist at all. Tasks with no project are invisible too.

Run `sprawl whoami` to see which project you're in — it prints the name, the
key, and the level you resolve to there.

## Workspace — the key already picks it

A workspace is the canvas a project lives on; every call runs in exactly one.
Your project key pins its project's workspace, so the workspace selector
(`-w <id>` / `--workspace <id>` / `SPRAWL_WORKSPACE`) stays unset — naming a
different one is refused with `403 workspace_mismatch`, and that error means
a stray `SPRAWL_WORKSPACE` is exported in this shell: ask the user to unset it.

`sprawl workspace list` answers "which workspace am I in, and which exist?" —
every workspace the user can reach, `›` on the current one. `whoami`'s
`workspace:` line says the same.

Only when the user explicitly asks to work in **another** workspace: the
simplest path is that workspace's project key (it pins it, nothing to select).
If instead they've set this shell up with an agent secret and no project key,
pass `-w <id>` on every call — ids are per workspace, so an id read under
`-w 3` answers `404` anywhere else, and a `404` under `-w` can also mean the
workspace itself is out of reach (check `workspace list`).

```bash
# Right — the key pins the workspace; nothing to select:
sprawl task list

# Right — user asked for workspace 3, shell has an agent secret and no key:
sprawl -w 3 task list
sprawl -w 3 item check 203                    # same -w on every follow-up

# Wrong — key and selector disagree → 403 workspace_mismatch:
SPRAWL_PROJECT_KEY=hobby sprawl -w 3 task list
```

## Permission model (how to read errors)

Your key resolves one of four scopes on the project:
`none` · `read` · `write` · `write_create`. `whoami`'s `project.level` is that
answer — one level, for everything you can reach this session.

| HTTP | Meaning | What to do |
|---|---|---|
| `200` | Success | Carry on. |
| `401` | Token bad / missing | Ask user to re-run `sprawl login`. |
| `403` | Your key is scoped out of this action — including any id outside your project | Don't retry. Tell the user you lack permission and which action. |
| `403 workspace_mismatch` | A `-w` / `SPRAWL_WORKSPACE` names a workspace other than the project key's | Not a permission problem. Ask the user to unset the selector — the key pins the workspace. |
| `404` | Genuinely gone or never existed | Treat as "not there". Don't assume it exists and retry. |
| `422` | Validation (e.g. empty search query) | Fix the input. |

Non-owner agents only see tasks their key resolves at least `:read` on, so
`task list` is already filtered. **Do not** loop retrying on `403` — the
server won't change its mind.

## Output formats

`--format` is a persistent flag: `text | json` (default `json`).
`SPRAWL_OUTPUT` sets a session default.

**Omit `--format` for reads you only eyeball.** The default is already `json`,
so a list / task / item / queue / search you're just pulling into your context
needs no flag.

Pass it explicitly when it matters:

- `--format=json` — in scripts and pipelines, so the command doesn't inherit
  a `SPRAWL_OUTPUT=text` from the caller's environment.
- `--format=text` — when you're showing the output to the user (tabwriter
  tables, multi-line detail views).

Errors in `json` come as a structured envelope:

```json
{"status": "error", "error": "<message>", "http_status": 403}
```

`http_status` is omitted for pre-flight errors (e.g. no project key set).
Auth / scope failures (`unauthenticated`, `second_factor_required`,
`invalid_project_key`, `forbidden`) add a `hint` string spelling out what went
wrong and how to fix it — relay it to the user rather than paraphrasing the
raw code.

## Task shape (house style)

The owner's convention for this task space. Follow it unless the user says
otherwise — it's what makes the board readable across agents and sessions.

- **Everything is an item.** The task is a lightweight container; the actual
  work is the items underneath it.
- **Title: short.** Server cap is **30 characters** (enforced — longer fails
  with a 422). Aim well under that: a handful of words, no punctuation
  filler. If you can't say it in a title, the thing is probably two tasks.
- **Skip the description.** Default to no `--description`. Don't summarise
  the task there — summarise it in the items.
- **If you must use a description**, keep it brief. Server cap is **255
  characters** (enforced at the DB layer — longer currently surfaces as a
  500, not a clean 422).
- **Itemize work as items**, titles kept short like task titles.
  One discrete thing per item. If an item reveals subwork, add a new item,
  don't cram it into the title.
- **Long context → the item's note.** When something needs paragraphs — a
  rationale, a block of status, a link dump, a hand-off — attach it as the
  note on the relevant item via `sprawl item update <id> --notes "..."`. A note
  is a field of its item, not a separate object; each item has at most one, and
  it's free-form and unbounded. That's the channel for anything that won't fit
  in a title.

So the usual create flow is: `task create --title "..."` (no description),
then one or more `item add <task_id> --title "..."`, then
`item update <id> --notes "..."` only on items that need the extra context.

## Command reference

All `/api/v1/*` commands honour `--format` and the credential model above.
`login` is interactive and always plain text.

### Discovery (reads)

Omit `--format` — the default `json` is what you want here (see [Output formats](#output-formats)).

```bash
sprawl task list
sprawl task <id>                               # task + its items (no `show` subcommand)
sprawl task <id> --full                        # …and every item's note body, one call
sprawl task search "<query>"                   # task + item titles; returns ids to fetch
sprawl item <id>                               # one item + its note (no --full; always included)
sprawl queue                                   # items ready to pick up, grouped by task
sprawl queue --state progress|review           # what's being worked / awaiting review
sprawl queue --full                            # …with each note expanded
sprawl activity                                # completed tasks + items for today
sprawl activity --days-ago 1                   # yesterday
sprawl activity --date 2026-04-29              # specific day
sprawl workspace list                          # workspaces you can reach, › on the current one
```

**Two nouns: `task` and `item`.** A task is the container; an item is the unit
of work and the thing a note hangs off. Every single-item verb takes a bare
**item** id — the one exception is `item add <task_id>`, because the item
doesn't exist yet. There is no `checklist` command and no `note` command.

**Read cost: `task <id>` is cheap, `--full` is not.** A plain `sprawl task <id>`
returns every item with a `has_notes` flag but no note **bodies** — enough to
see the shape of the work and spot which items carry context. `--full` pulls
every body over the wire. So:

- `sprawl task <id>` — orienting, checking progress, finding an item id.
- `sprawl task <id> --full` — you're about to work the task and need the notes.
- `sprawl item <id>` — you want one item's note and nothing else.

**Daily activity:** `sprawl activity` returns the calling agent's completed tasks + completed items for a single day, scoped by the same key cascade as `task list`. Default is today in the user's timezone. `--date` (YYYY-MM-DD) and `--days-ago` (`0..365`, `0`=today) are mutually exclusive — passing both is a local error before any HTTP call. Empty days return an empty result, not an error. Useful for daily standup write-ups, weekly summaries (loop over `--days-ago 0..6`), and answering "what did I get done yesterday?".

**Reading a note body into a shell variable or another command — use `--format=json` and `jq`.**

There is no longer a command that prints a bare note. `item <id> --format=text`
renders a table row with the note under it, which is right for a human and wrong
for a pipe. When you need the raw body — capturing it, piping it into `less`,
writing it to a file — go through json and extract the field.

```bash
# Right — the raw body, nothing else:
body=$(sprawl item 203 --format=json | jq -r '.checklist_item.notes')
sprawl item 203 --format=json | jq -r '.checklist_item.notes' | less
sprawl item 203 --format=json | jq -r '.checklist_item.notes' > note.md

# Wrong — the table rendering leaks into the variable (columns, "note:" label):
body=$(sprawl item 203 --format=text)

# Wrong — the whole json envelope leaks in instead of just the note:
body=$(sprawl item 203)
```

`--format=json` is explicit here even though it's the default: `SPRAWL_OUTPUT`
may be set to `text` in the environment you're running in, and a pipeline that
silently depends on the caller's env is a pipeline that breaks on someone
else's machine.

An item with no note gives `null` from `jq -r` — check for it rather than
writing the four characters into a file.

For a whole task at once, `--full` plus one `jq` beats N calls:

```bash
sprawl task 119 --full --format=json \
  | jq -r '.task.checklist_items[] | select(.notes) | "#\(.id) \(.title)\n\(.notes)\n"'
```

### Writes — tasks

Wire body: `{"task": {...}}`. Accept explicit flags or `--from-json <path|->`;
explicit flags override fields parsed from JSON. Title max **30 chars**,
description max **255 chars** — see [Task shape](#task-shape-house-style)
for why you usually skip description entirely.

```bash
# Create — lands in your project, no id needed:
sprawl task create --title "wire up CI"

# Create from a JSON template, tweak one field:
echo '{"title":"draft"}' \
  | sprawl task create --from-json - --title "final"

# Update title (server ignores project_id on update):
sprawl task update 17 --title "renamed"
sprawl task update 17 --description ""        # explicit clear (not "flag unset")

# Set / clear due date (separate route — `task update` ignores due_date).
# Only do this when the user asked for it — see Guardrails below.
sprawl task due 17 today                      # set due date (resolved in user TZ)
sprawl task due 17 yesterday                  # backdate by one day
sprawl task due 17 week                       # owner's configured week-end
sprawl task due 17 none                       # clear due date

sprawl task delete 17                         # soft-delete; 404 from server treated as success (idempotent)
```

`task due` takes a preset (`yesterday|today|week|none`) and the server
resolves it against the user's timezone and `week_end_day` setting. Reads
return the *resolved* ISO date in `due_date` — there's no echo of which
preset is currently set, so if you need that, compute it locally by
comparing the date against today / yesterday / the user's week-end.

### Writes — items

Wire body: `{"checklist_item": {...}}`. Server assigns `position` on add.
Permission is checked on the **parent task**, not per item.

```bash
sprawl item add <task_id> --title "write migration"
sprawl item add <task_id> --title "deploy" --notes "run after backfill"

sprawl item check <id>                        # idempotent
sprawl item uncheck <id>                      # idempotent
sprawl item update <id> --title "renamed"
sprawl item delete <id>                       # hard-delete; idempotent on 404
```

Use `check` / `uncheck` for completion — `update` doesn't mutate it. The split
avoids a GET-then-PATCH race when you don't know current state.

Every write prints a one-line `✓ …` confirmation in `text` mode and the
`{"checklist_item": {…}}` envelope in `json`.

### Writes — item state and PR number

Two independent fields on an item. State says where the work is; the PR number
links it to GitHub.

```bash
sprawl item state <id> ready|progress|review|none
sprawl item pr    <id> <number|none>
```

`ready` / `progress` / `review` are the vocabulary in **both** directions — what
you type and what every read gives back. The server's own spellings
(`ready_to_pickup` / `in_progress` / `in_review`) are still accepted as input,
but no output uses them, so a value you read can be written straight back.
Don't confuse an item's `state` with a task's `status`: the task's is derived
from checked counts and really is `in_progress`.

Rules that bite:

- **`ready` is the human's signal, not yours** — it's how they flag work for an
  agent to pick up. You set `progress` and `review`.
- **States are exclusive** — setting one replaces the previous.
- **Setting a state un-completes the item.** State and "done" are mutually
  exclusive server-side, so never set a state on something already checked
  off unless you mean to reopen it.
- **Checking an item clears its state**; the PR number survives.
- `state` and `pr` are separate routes from `item update`, which ignores both
  fields.

### Writes — the note (hand-off channel)

A note is a free-form field **of** an item — each item has at most one, and
`item update` is the only thing that writes it. This is the primary place to
leave context for another agent or the human.

```bash
sprawl item 203                               # read it (note always included)
sprawl item update 203 --notes "blocked on PR #418"
sprawl item update 203 --notes ""             # clears the note
cat status.md | sprawl item update 203 --notes -
```

`--notes -` reads the whole body from stdin — that's how you write a multi-line
or piped note. You can't combine it with `--from-json -`; both would claim
stdin, and the CLI rejects that locally before any HTTP call.

### Misc

```bash
sprawl version                                # prints version + baked-in API URL
sprawl whoami                                 # who am I + which project I'm in (also a liveness probe)
sprawl theme get                              # read the active UI theme
```

The theme is a **user-level** setting, not a project one: `sprawl theme set`
is not available to you under a project key, and changing the user's theme is
not your job. If they ask for it, tell them to run it themselves.

## Collaboration patterns

These are the common shapes of work the skill exists for.

### 1. Pick up assigned work

```bash
sprawl queue                   # items flagged ready to pick up, grouped by task
sprawl task <id> --full        # the task plus every item and its notes, one call
```

`sprawl queue` is the entry point when the user says "pick something up" or
"what's ready?". Its payload is `{"tasks": [{id, title, description, due_date,
project, checklist_items: [...]}]}` — each task's title and description come
with its items, so you can choose what to pick up without a `task <id>` per
group. Fall back to `sprawl task list` when nothing is queued or you
need the wider picture — your key already scopes both server-side.

**Claim it, then work it.** The states are how the human and other agents see
who's on what, live:

```bash
sprawl item state 7 progress   # claiming it — do this before you start
# … do the work, open the PR …
sprawl item pr    7 412
sprawl item state 7 review     # hands it back
```

Stop at `review`. **Don't check off an item you put in review** — the human
reviews the PR and checks it off. Checking it would clear the state and hide
the work from their queue.

**Read the notes before you start.** Notes are where the previous agent or
the human left hand-off context — skipping them is how you redo work someone
already did or miss a blocker they flagged. `sprawl task <id> --full` pulls
every item's note in one call, so prefer it when picking up a task; drop to
`sprawl item <id>` when you only care about one:

```bash
sprawl task <id> --full   # preferred: task + items + every note, one call
sprawl item <id>          # one item and its note, when that's all you need
```

A plain `sprawl task <id>` (no `--full`) still shows a `NOTES` column (`o` / `-`) per item
without the bodies — fine for a quick glance or for finding an item id, but
`--full` is the one-call way to actually read them.

**When you finish an item, check it off immediately** — see
[§2](#2-make-progress-on-a-task). Don't wait until the end of the task:
other agents and the human are watching the board live, and an unchecked
item reads as "still to do".

### 2. Make progress on a task

Mark items as you finish them; don't batch. Other agents and the human see
the state live.

```bash
sprawl item check 203
```

If an item reveals subwork, add a sibling item rather than stuffing it into
the note:

```bash
sprawl item add <task_id> --title "backfill legacy rows"
```

### 3. Leave context for another agent or the human

Use the item's note. `--notes` is a full replace, so read it first if you need
to preserve what's there:

```bash
prev=$(sprawl item 203 --format=json | jq -r '.checklist_item.notes // ""')
printf '%s\n\n---\n\n%s\n' "$prev" "blocked on PR #418" \
  | sprawl item update 203 --notes -
```

**Don't sign your edits.** No "done by <agent-name>", no "— claude". The
server already records the last actor on each item. Manual signatures just add
noise the human has to skim past.

### 4. Create a new task on behalf of the user

Only if the user asked, and only if `whoami` shows `write_create` on your
project. Run
[`sprawl whoami`](#before-you-write--check-your-scope) first if you haven't
this session — that's the cheap way to find out before round-tripping.
If you get a `403` anyway, surface it — don't retry.

Create the task on its own, then add items in separate calls so
each step's outcome is visible — see the chaining warning in
[Before you write](#before-you-write--check-your-scope).

```bash
sprawl task create --title "flaky deploy"     # lands in your project automatically
# → note the task id from the output
sprawl item add <task_id> --title "repro on staging"
sprawl item add <task_id> --title "check #ops logs"
```

### 5. Hand off

Finishing your slice and passing to another agent or the human:

1. `item check` the items you finished.
2. `item update --notes` on the next item with a short status + what you
   couldn't do and why (permission, missing info, blocker).
3. Do **not** `task update` the title / description just to log status — that
   rewrites the task. Status belongs in notes or new items.

## Guardrails

- **Never** go looking for the user's bearer token, and never read or copy
  `~/.config/sprawl/config.toml`. The project key is all the scope you need
  (and it isn't a secret).
- **Never** pass `--project-id`. Your project key already decides where a
  task lands; naming a project id is at best redundant and at worst rejected.
- **Leave `-w` / `SPRAWL_WORKSPACE` unset** unless the user explicitly named
  another workspace. The project key pins the workspace; a selector that
  disagrees is a `403 workspace_mismatch`, never a workaround.
- **Never** attempt `sprawl login` — it's interactive; ask the user instead.
- **Never** retry `403` responses. Permission won't flip mid-session.
- **Don't** use `task update` as a status channel. Use notes / items.
- **Don't delete tasks or items the user didn't ask you to remove.**
  `task delete` is a soft-delete and can only be undone via the LiveView
  trash bin (no API to restore). `item delete` is a hard delete and
  has no undo. When in doubt, leave a note on the item instead.
- **Don't set due dates the user didn't ask for.** `task due` is for when
  the human explicitly tells you to schedule something (or you're acting
  out a clearly scheduled instruction — "remind me about this Friday").
  Don't infer due dates from urgency cues, don't backfill them on existing
  tasks, and don't add them when creating a task on the user's behalf
  unless they specified one. The due date is the owner's planning channel,
  not an agent housekeeping field — leaving it blank is the correct
  default.
- **Don't** `--from-json` with untrusted input without reading it first — the
  file / stdin is parsed as the full task/item attrs map.
- **Empty strings matter**: `--description ""` and `item update X --notes ""` are
  *explicit clears*, distinct from "flag unset". Use them deliberately.
- **Write only what the user asked for.** The task space is shared with the
  human and other agents; spurious tasks or notes are noise for everyone.
