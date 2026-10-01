# Implement checklist assignment in the CLI

Add a non-interactive CLI surface to discover assignees, assign or reassign an
item, clear its assignment, and assign it during creation. Follow the existing
`item` command tree and shared HTTP client and renderers.

Assignment belongs to a **checklist item**, the actionable child of a task.
Task cards have no assignee field or assignment endpoint. A command receiving
an existing item takes an item ID; `item add` takes its parent task ID.

Backend reference: [Sprawl PR #51](https://github.com/ultrakorne/sprawl/pull/51),
commit `09317c5`. Existing-item assignment is already implemented; assignment
at creation ships in that PR. Verify the target backend includes it before
claiming create-time assignment works: an older create endpoint can ignore
the field. The backend contract is documented in
`/home/ultra/Developer/sprawl/docs/features/api/checklist-assignment.md`.

## Implementation steps

1. Read `AGENTS.md`, `docs/INDEX.md`, the items and output-formats feature docs,
   and the credential rules. Inspect `internal/client/client.go`,
   `internal/cli/item.go`, `internal/cli/item_render.go`,
   `internal/cli/root.go`, and their tests. Done when the changes reuse the
   existing request, error, flag-merging, and output paths.
2. Add typed assignment and actor-discovery models to the client. Reuse the
   decoded Workspace from `whoami` and existing Workspace listing; add actor
   discovery through the existing HTTP client. Done when mocked responses
   retain `assignee`, actor labels/markers, and Workspace IDs.
3. Add the proposed commands below. Share one assignment parser and one
   attribute-merging helper across standalone commands and add/update flags.
   Done when every assignment write uses the existing item create/update
   method and the exact envelope in the API contract.
4. Carry assignment through the shared renderer. Done when item detail,
   task detail, queue, and item writes expose the same pair or null in JSON
   and readable assignment in text, without extra roster requests.
5. Add the acceptance tests below, update help, README, and feature docs,
   and update `skills/sprawl/SKILL.md` using writing-for-agents. Done when
   `make check` passes and command examples match actual help output.

## Proposed command surface

These commands and flags are implementation targets, not existing CLI features.

```sh
sprawl workspace list
sprawl workspace actors                 # resolve workspace.id via whoami
sprawl workspace actors 42              # explicitly discover Workspace 42

sprawl item assign 203 user:3
sprawl item assign 203 agent_key:7       # same operation also reassigns
sprawl item unassign 203

sprawl item add 119 --title "Write docs" --assignee agent_key:7
sprawl item update 203 --assignee user:3
sprawl item update 203 --unassign
sprawl item update 203 --title "Review docs" --assignee user:3

sprawl --workspace 42 workspace actors
sprawl --workspace 42 item assign 203 agent_key:7
```

`--assignee` accepts `user:<id>` or `agent_key:<id>`. Require a positive
base-10 ID at most `2147483647`; reject unknown types and malformed values
locally. IDs may overlap across actor types, so always retain the type.
`agent_key` is the assignment type; the audit field `last_actor.type` can
use `agent` and is a separate concept.

Use Cobra's flag-presence check to distinguish omission from an explicit
clear. Reject `--assignee` with `--unassign` locally. Standalone assign and
unassign commands build the same attrs as `item update` and reuse its write
and output path. Add/update also accept `assignee` through the existing
`--from-json` object; explicit assignment flags override JSON. A missing
assignment flag preserves the JSON value, including explicit null.

Reuse the root `--workspace <id>` / `SPRAWL_WORKSPACE` selector and the
client's existing Workspace-bound path selection. Task, item, queue, and
activity routes are scoped; user-level `whoami`, Workspace listing, auth,
and settings stay flat. Keep `workspace list` reading `whoami` so its list
and current marker use one call. Actor discovery names a Workspace in its
own path. If its positional Workspace conflicts with the selector, return
a local argument error. Otherwise use the explicit ID, selector, or
`whoami.workspace.id`, in that order. Keep the TUI unchanged.

The supported output formats are JSON and text. For a backend on port 4001,
use `make build-dev PORT=4001` or `SPRAWL_API_URL=http://localhost:4001`;
the default build port and config model stay unchanged.

Actor labels are for display and selection by humans; assignment uses the
stable `{type, id}` pair. Start with explicit typed IDs rather than implicit
name matching or interactive prompts.

## REST contract

Reuse the configured API URL and existing authentication: bearer token plus
`X-Agent-Secret` and/or `X-Project-Key`. Workspace selection changes the path,
not credentials. Keep all configured narrowing factors on discovery and writes.

| Operation | Method and path | Success |
|---|---|---|
| Resolve current Workspace | `GET /api/v1/whoami` | 200; `workspace.id` |
| List reachable Workspaces | `GET /api/v1/workspaces` | 200; `workspaces` array |
| Discover eligible assignees | `GET /api/v1/workspaces/:id/actors` | 200; `actors` array |
| Assign, reassign, or clear | `PATCH /api/v1/checklist_items/:id` | 200; `checklist_item` |
| Create with assignment | `POST /api/v1/tasks/:task_id/checklist` | 201; `checklist_item` |
| Read an item | `GET /api/v1/checklist_items/:id` | 200; `checklist_item`, notes and task stub |

With a Workspace selected, use
`PATCH /api/v1/workspaces/:workspace_id/checklist_items/:id` and
`POST /api/v1/workspaces/:workspace_id/tasks/:task_id/checklist`.
Use that same Workspace on item reads and other follow-up calls.

Discovery response example:

```json
{"actors":[
  {"type":"user","id":3,"label":"Alex","marker":"🦊"},
  {"type":"agent_key","id":7,"label":"builder","marker":"🤖"}
]}
```

The server lists the Workspace owner, members with write access, and bound
agent keys whose effective access is write or above, capped by their owner's
Workspace role. Discovery itself requires write access. The server enforces
eligibility again on the write; a successful discovery is not a reservation.

Assignment/reassignment request body:

```json
{"checklist_item":{"assignee":{"type":"agent_key","id":7}}}
```

Unassignment request body:

```json
{"checklist_item":{"assignee":null}}
```

Create request body:

```json
{"checklist_item":{"title":"Write docs","notes":"Check the examples",
                   "assignee":{"type":"user","id":3}}}
```

Always send the singular `checklist_item` envelope. The input `assignee` has
three meanings: omitted preserves assignment (creates unassigned), null
clears it, and an object assigns exactly one actor. Both types accept integer
or numeric-string IDs on the wire; the CLI should emit integers. PATCH
requires at least one of title, notes, or assignee. Creation requires a title.

Responses include the stored `checklist_item.assignee` pair or null. PATCH
also echoes notes; creation keeps its existing compact response. Successful
creation inserts the assigned item atomically; an invalid assignment inserts
nothing. Assignment itself preserves completion, Item State, and PR number.
Continue using the dedicated commands for those fields.

## Client models and output

Add an optional assignment pair to `client.ChecklistItem`, separate from
`client.Actor` (audit identity). Add a discovery actor with type, ID, label,
and marker, and decode the `actors` envelope. Model omission versus explicit
null in write attrs with map-key presence; a nil value alone cannot represent
both when using `omitempty` on a request struct.

Extend `itemMap` once so every machine-output item includes `assignee` as a
pair or null. Update the shared item table/write renderer for text output;
show a typed ID when no label is available. Keep the ordinary read path usable
with a read-only key: fetching actor labels implicitly would introduce a
write-permission requirement into item/task/queue reads. Actor labels and
markers can be obtained explicitly through `workspace actors`.

The API can retain a stale assignment during concurrent actor revocation.
When a current actor roster is available, treat an absent pair as unassigned
for display. Preserve the stored pair in machine output; without a roster,
display its typed ID rather than claiming the actor still has write access.

## Errors and Workspace behavior

| Result | CLI behavior |
|---|---|
| 401 | Surface the existing authentication error and re-pair guidance. |
| 403 `forbidden` | Surface insufficient effective access; retain all factors. |
| 403 `workspace_mismatch` | Explain that the selector conflicts with the Project Key. |
| 404 `not_found` | Resource is unavailable in this scope; verify Workspace and ID. |
| 422 `invalid_assignee` | Malformed/ineligible target; refresh actor discovery. |
| 422 `invalid_body` or validation errors | Report the server error in the selected output format. |

The request's key permissions and the user's Workspace role still bound each
write. Selecting a Workspace grants no permissions. Keep the Project Key;
the server allows actor discovery in its confined Workspace and rejects
conflicting selectors. Reach failures are 404; permission failures are 403.
Use the existing `APIError` and formatted-error pipeline rather than a second
error path. Surface assignment refusals as failures, preserving their code.

## MCP correspondence

The CLI remains a REST client. These tools describe the equivalent server
surface for agent help and cross-surface terminology; no MCP client is required.

| Tool | Arguments relevant to assignment |
|---|---|
| `workspace_actors` | Optional `workspace_id`; returns `actors`. |
| `checklist_update` | `id`, `assignee`; optional title, notes, `workspace_id`. |
| `checklist_add` | `task_id`, title, optional notes, assignee, `workspace_id`. |

MCP uses flat arguments, with the same pair/null/omission semantics. The
dispatcher selects the Workspace before invoking the tool. Execution refusals
appear as `result.isError: true` and `result.structuredContent.error`.

## Acceptance tests

Use `httptest` and existing fixtures; verify the actual method, path, headers,
body, and decoded/output fields. Cover:

- Discovery default/explicit Workspace and shared Workspace roles; all formats.
- Assign/reassign to different users and keys; unassign; assigned creation.
- Omitted assignment versus null, JSON-only assignment, explicit flag precedence,
  and mutually exclusive assignment/clear flags before any HTTP request.
- Invalid type/ID, zero, oversized ID, and assignment-only PATCH envelopes.
- Flat versus selected-Workspace paths; user-level calls stay flat; credentials
  and Project Key survive discovery and writes unchanged.
- 401/403/404/422 propagation, stale/unknown actors, and server refusal after
  a target was discovered. Never claim successful assignment after a refusal.
- Pair/null preservation across item/task/queue/write JSON outputs,
  shared text cells, and no implicit actor request on read-only item views.
- Existing notes/stdin handling, state/completion/PR behavior, and both binary
  variants retain their current contracts. Run `make check` before completion.

In agent-facing help, lead with assign/reassign/clear actions and point to
`workspace actors` for discovery in the same Workspace. Keep pair/null/omission
semantics in one shared reference, and use tested command examples.
