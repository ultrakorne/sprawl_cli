# Items — Technical

## Architecture

`internal/cli/item.go` owns the item command tree and shared add/update flag
merging. Standalone assignment commands build attributes through
`internal/cli/assignment.go` and call the same update request and response path.
The HTTP client owns the singular item envelope and workspace path selection;
the server enforces parent-task permission and assignment eligibility.

`internal/cli/item_render.go` supplies item maps and table rows for item detail,
task detail, queue, and writes. Assignment belongs to the item model, separately
from audit identity. Discovery supplies labels explicitly; ordinary views
render typed IDs and never request write-only actor data.

## Where things live

| File | Role |
|------|------|
| `internal/cli/item.go` | Item commands, notes/stdin merging, and write responses |
| `internal/cli/assignment.go` | Shared assignment parser, attribute merging, and assign/clear commands |
| `internal/cli/item_render.go` | Shared machine maps, table cells, and expanded notes |
| `internal/cli/workspace_actors.go` | Explicit actor discovery and workspace resolution |
| `internal/client/client.go` | Typed item/actor models, envelopes, and scoped HTTP methods |
| `internal/cli/output.go` | Shared errors and invalid-assignee guidance |
| `internal/cli/task.go` | Task detail through the shared item renderer |
| `internal/cli/queue.go` | Task-grouped queue through the shared item renderer |
| `internal/cli/style.go` | Palette styling, table widths, and hyperlinks |
| `internal/icons/icons.go` | State glyph vocabulary shared with the TUI |

## Noteworthy

- Attribute-map key presence distinguishes omitted assignment from explicit
  null. Explicit flags override JSON before assignment validation, so a flag
  can replace an invalid JSON target. Numeric-string JSON IDs normalize to integers.
- Assignment parsing checks shape and bounds locally; actor eligibility stays
  server-owned. Discovery is advisory and a refused write remains a failure.
- Stored assignments survive roster revocation in responses. Machine output
  retains the pair, including unknown types; text renders the typed ID without
  inferring eligibility. No roster is loaded or cached by item views.
- PATCH carries saved notes, including assignment-only writes. Create,
  completion, and state writes use compact responses, so their renderers omit
  notes rather than imply a clear. Item detail always emits notes.
- A write has no parent project; its PR number survives but the rendered PR URL
  is null. Reads resolve links from the parent-task stub or task group.
- Completion and state interact server-side. The returned item is authoritative;
  assignment does not make an item active or complete it.
- Every item map emits an assignee pair or null, including responses from a
  backend that omits the field. State normalization and PR-link construction
  follow [output-formats](../output-formats/TECHNICAL.md).
- Notes use table column widths to align beneath ID. Hyperlink escapes wrap
  styled cells after layout; neither escapes nor emoji distort column widths.
- Add/update reject shared stdin consumption before sending HTTP. An empty
  note is an explicit clear, detected by Cobra flag presence.
- Hard-delete 404s share the idempotent predicate with tasks. A selected
  workspace or confined project is named in the no-op response because a miss
  there does not prove the item is absent elsewhere.
