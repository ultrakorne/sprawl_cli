# sprawl CLI — Context

The ubiquitous language used across the sprawl CLI. Definitions are one sentence — what a term *is*, not what it does. The canonical name is bolded; rejected synonyms sit under `_Avoid_`. Implementation lives in the feature docs.

## Access and scope

**Token**:
The bearer credential obtained by `login` through the device flow and stored in `config.toml`; the only thing sprawl ever writes to disk.
_Avoid_: bearer token (as a distinct thing), API key

**Agent secret**:
A narrowing factor that makes a request act as a specific agent key, with that key's permissions and attribution; never persisted by sprawl.
_Avoid_: secret key, agent token

**Project key**:
A narrowing factor that confines a request to one project; not a secret, so it may be shown; never persisted by sprawl.
_Avoid_: project token, project id (a different thing — the numeric id)

**Narrowing factor**:
Either of the two values (agent secret, project key) the server requires alongside the token; both together are intersected, so a factor can only narrow permission, never widen it.
_Avoid_: second factor, credential (for the project key)

**Workspace**:
An independent canvas of tasks and projects — the user's own or one shared with them — in which every task, item, queue and activity call runs.
_Avoid_: tenant, account, board

**Workspace selector**:
The `--workspace` / `SPRAWL_WORKSPACE` id that picks which workspace a call runs in; it changes the canvas, never the permission, and is not a narrowing factor.
_Avoid_: workspace key, workspace factor

> **selector vs factor** — a narrowing factor decides what a request may do; the workspace selector only decides which canvas it does it in. A selector alone never satisfies the "token plus one factor" rule, and like the project key it is never persisted.

**Default workspace**:
The workspace a call runs in when no selector is set — the confined project's under a project key, else the agent key's own.
_Avoid_: home workspace, primary workspace

**Role**:
The logged-in user's own standing in a workspace (`owner`, or the membership level of a shared one).
_Avoid_: level (that is the key's)

**Level**:
The permission the presented credentials actually resolve to on a workspace or project — `none`, `read`, `write` or `write_create`.
_Avoid_: role, scope, access (as a noun for the value)

> **role vs level** — role is the user's membership; level is the presented key's reach. A user can be `owner` of a workspace whose level reads `none` for a workspace-bound key (lists empty, writes forbidden); under a project key every workspace's level is `none` by construction — confinement, not "no access".

## Work

**Task**:
A unit of work identified by a numeric id, with a title, description, due date, project and derived status, that owns an ordered set of checklist items.
_Avoid_: card, ticket

**Checklist item**:
A single entry under a task with its own numeric id, a title and a completion flag — the unit the checkbox toggles and the thing a note attaches to; **item** is the short form and the canonical user-facing name.
_Avoid_: subtask, todo, step, entry

**Note**:
Free-form text attached to one checklist item as a field of it; each item has at most one, and it is never a standalone object.
_Avoid_: comment, description, notes (plural, as a name for one note)

**Item state**:
The hand-set marker on a checklist item — **`ready`**, **`progress`**, **`review`**, or none — one vocabulary for reading and writing; an item carrying a state is incomplete by construction.
_Avoid_: item status, stage, phase, column, `ready_to_pickup` / `in_progress` / `in_review` (server-internal spellings)

**Task status**:
The task-level `status` string the server derives from how many checklist items are checked; nothing sets it by hand, and it is not the item state even where the wire spelling `in_progress` coincides.
_Avoid_: task state, progress (as a name for the string)

**PR number**:
The GitHub pull-request number attached to a checklist item — a bare positive integer, stored without a URL, independent of state and completion.
_Avoid_: PR link, PR url, pull request id

**Repo URL**:
A project's GitHub repository address (`github_url`), against which a PR number is resolved into a link; a project without one is normal.
_Avoid_: github link, repo link, remote

**Queue**:
The set of checklist items in one item state across every task the caller can read — a query, not a stored list, and the only item view that crosses tasks.
_Avoid_: backlog, inbox, board

## Presentation

**Terminal palette**:
The terminal emulator's sixteen ANSI colors (indices 0–15), the only colors the human output and the TUI use, so both adopt whatever theme the terminal runs.
_Avoid_: theme (alone), color scheme

**App theme**:
The server-side web-app theme id (e.g. `tokyo-night`) read and written by `theme get` / `theme set`; unrelated to the terminal palette.
_Avoid_: theme (alone), UI theme (in code)

## Relationships

- A request carries the **token** plus at least one **narrowing factor**; the **workspace selector** rides on top and picks the canvas.
- A **workspace** contains projects and **tasks**; a **project key** pins its project's workspace.
- A **task** owns ordered **checklist items**; a **checklist item** owns at most one **note**, at most one **item state**, and at most one **PR number**.
- A **task** belongs to at most one project; that project supplies the **repo URL** that turns an item's **PR number** into a link.
- A **queue** is every **checklist item** sharing one **item state**, across tasks in one **workspace**, grouped by task.
