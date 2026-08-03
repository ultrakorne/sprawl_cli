# Item State & PR Number — Design

## Overview

A checklist item now carries two optional fields beyond its title, completion flag, and note:

- an **item state** — one of `ready_to_pickup`, `in_progress`, `in_review`, or none at all;
- a **PR number** — the GitHub pull-request number the work landed in.

Both are hand-set. Neither is derived, and neither is required: an item that never enters review carries neither and reads exactly as it did before the feature existed.

The pair exists to make a checklist legible as *live* work rather than as a static to-do list. Before, the only signals were "checked" and "not checked", so a human looking at a board could not tell whether an unchecked item was untouched, claimed by an agent right now, or already finished and waiting on their review. The state says where the work is; the PR number says where to look at it.

## Why

- **Agents need a claim mechanism.** Multiple agents share one task space. Without a visible "I'm on this", two agents pick up the same item, or none do because each assumes the other has.
- **Review is a human's job, and it needs a queue.** An agent that finishes a slice of work cannot check the item off — the human has to read the PR first. `in_review` is the parking state for exactly that, and `queue --state review` is the human's inbox.
- **"What can I pick up?" was an N-call question.** Answering it meant listing tasks, then fetching each one's checklist. `queue` answers it in one call across every task the caller can read.
- **A PR number without a link is friction.** The project already knows its repo, so the client can build the full URL rather than making every consumer concatenate one by hand.

## The two fields

### Item state

Three states, in lifecycle order:

| State | Short form | Means |
|---|---|---|
| `ready_to_pickup` | `ready` | Queued for whoever picks it up next. |
| `in_progress` | `progress` | Someone is on it right now. |
| `in_review` | `review` | Work is done; a human owes it a review. |
| *(cleared)* | `none` | Ordinary item — the default, and where every item starts. |

Rules that follow from the server's model:

- **States are mutually exclusive.** Setting one replaces any previous one; there is no "also".
- **State and completion are mutually exclusive.** Setting a state on a completed item **un-completes** it, and checking an item **clears** its state. So every item carrying a state is incomplete by construction — which is why `queue` needs no `completed` filter.
- **State is not task status.** A task's `status` is derived server-side from its checked counts; an item's state is set by hand. They happen to share the string `in_progress` and mean different things. See [CONTEXT.md](../../CONTEXT.md).

### PR number

A positive integer, or nothing. It is deliberately **independent of everything else**:

- it can be set before any review starts (a draft PR);
- it survives the item being checked off, un-checked, or moved between states;
- only an explicit clear removes it.

The **link** is not stored — it is built by the client from the item's parent task's project `github_url` plus the number. Any break in that chain (no project on the task, no repo URL on the project) is not an error: the number simply renders bare as `#412`. That keeps the feature usable on boards whose projects have no repo.

## Components

### `checklist state <item_id> <ready|progress|review|none>`

Sets or clears the state. Accepts the short words above *and* the wire values verbatim, so an agent can echo back whatever a read handed it. Anything else fails locally, naming the accepted set, rather than round-tripping for a bare 422. The PR number is untouched.

This is a separate route from `checklist update`, which ignores the field entirely.

### `checklist pr <item_id> <number|none>`

Attaches or clears the PR number. Non-positive or non-numeric input fails locally — the server's own rule, applied before the round-trip. The state is untouched.

### `queue`

`GET /api/v1/checklist_items?state=…` — the one read that crosses tasks. It is top-level rather than a `checklist` subcommand precisely because it is *not* scoped to a task: it answers "what is in this state anywhere I can see?", which is the question an agent asks before starting work.

Defaults to `ready_to_pickup`. Also takes `progress` (what's being worked) and `review` (what's waiting on a human). `none` is rejected — the endpoint requires one of the three, and "items with no state" is just the ordinary checklist. Results honour project confinement like every other read.

Each returned item carries its parent task and that task's project, so the human table can show *what work this is*, and the structured payload can carry a resolved `pr_url` without a second call.

### Existing reads

The fields are additive everywhere else, and render as nothing when unset:

- `checklist <task_id>` gains `STATE` and `PR` table columns.
- `checklist <task_id> --full` and `task <id> --full` append a ` · review · #412` trailer to the item line. Only `task <id> --full` can resolve the full link, since it is the one read that has the task's project in hand.
- json / toon payloads always carry `state` and `pr_number` keys, `null` when unset — so a consumer never has to distinguish "absent" from "cleared".

### Interactive TUI

`s` cycles the selected item's state (none → ready → progress → review → none); `p` prompts for a PR number, prefilled with the current one, and an empty submit clears it; `o` opens the PR in a browser, falling back to the clipboard when there's no browser to hand it to (the SSH case).

A row shows the **icon** only — the same hand / crossed-tools / eye icons as the web app's pills — in a fixed-width column beside a fixed-width PR column, so toggling a state never reflows the list. Opening the item spells the state out in full (`󱌣 In progress · PR #412`). PR numbers are hyperlinks in both places, so a click opens them where the terminal supports it. See [interactive-tui](../interactive-tui/INDEX.md).

## The agent hand-off protocol

The states only earn their keep if every agent follows the same sequence. This is the protocol the bundled sprawl skill teaches:

1. `queue` — see what's flagged ready.
2. `checklist state <id> progress` — **claim it before starting**, so nobody duplicates the work.
3. Do the work; open the PR.
4. `checklist pr <id> <number>` — link it.
5. `checklist state <id> review` — hand it back. **Stop here.**

An agent must **not** check off an item it put in review: checking clears the state, which would hide the work from the human's review queue. Items that never involved a PR follow the ordinary rule and are checked off as they finish.

On hand-off, anything left mid-flight goes back to `ready` rather than being left at `progress` — otherwise it looks claimed forever.

## Design decisions

- **A dedicated route, not `checklist update`.** The generic item PATCH silently ignores both keys, so folding them in would produce a no-op that looks like a success. The split also mirrors the existing `check` / `uncheck` precedent: fields with side effects get their own verb.
- **Short words in, short words out.** The CLI prints `review`, and `review` is what you type. The wire values are accepted too so a script can round-trip a read without translating. Aliases are a CLI concept, so the CLI owns the vocabulary and validates locally.
- **`none` rather than a `--clear` flag.** Keeps the surface positional-only and parallels `task due none`.
- **Clearing sends an explicit null, never an omitted key.** An absent key means "leave unchanged"; the two are different requests, and conflating them would make "clear" silently do nothing.
- **The PR link is built client-side.** Storing a URL per item would duplicate the repo on every row and go stale when a card is repainted into another project. Resolving through the task's *current* project means a moved card re-points its links for free.
- **A missing repo URL is not an error.** Rendering the bare number keeps the feature usable on projects with no GitHub repo instead of forcing every board to configure one.
- **`queue` is top-level.** Nesting it under `checklist` would imply a task scope it does not have.
- **Stable columns beat compact ones.** The TUI reserves the state and PR columns whether or not anything on screen uses them. Sizing them to the visible items saves a few cells and costs a sideways jump on every `s` press, right where the eye is.
- **Icons in rows, words in the header.** The row has room for one cell; the icon carries the meaning there, exactly as it does in the web app. The name only appears where it fits without pushing anything around.
- **Unknown states pass through verbatim.** A state added server-side after a given binary shipped renders as its raw string rather than vanishing — an old CLI shows something it doesn't understand instead of silently lying about an empty field.
