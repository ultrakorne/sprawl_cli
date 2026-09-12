# Workspaces — Technical

## Architecture

The selector is resolved once per invocation (`--workspace` flag → `SPRAWL_WORKSPACE`) by the resolver in `internal/cli/auth.go` and validated in the root command's `PersistentPreRunE` before any command runs. `newAuthedClient` passes it to the client as an option; it is checked *after* the narrowing-factor rule, so a workspace alone never lets a request out. On the wire the client turns a selection into a path prefix — `/api/v1/workspaces/<id>/tasks` instead of `/api/v1/tasks` — on every workspace-bound route (tasks, checklist items, activity log) and leaves user-level routes (whoami, settings, device auth) flat. The server models a workspace as a resource, not a credential, which is why nothing about headers or permission changes.

`workspace list` and `whoami` both read `GET /api/v1/whoami`, which reports the default workspace and the reachable list but ignores the selector. The CLI resolves a selected id against that list itself, so an unreachable selection fails in those two commands with a plain message instead of as a 404 on the first task call. The TUI receives the selector through its `Deps`, fetches `whoami` silently after the credentials validate (to name the workspace in the header), and re-pins the session on `w` by building a new client and discarding every loaded task and item.

## Where things live

| File | Role |
|------|------|
| `internal/cli/auth.go` | `resolveWorkspace` (flag → env, positive-integer check) and its wiring into `newAuthedClient`. |
| `internal/cli/root.go` | Registers `-w` / `--workspace`; validates the selector in `PersistentPreRunE`. |
| `internal/cli/workspace.go` | The `workspace` noun, `workspace list`, `effectiveWorkspace` (selection resolved against the reachable list), the table and json shapes. |
| `internal/cli/whoami.go` | Workspace lines in the text view, `workspace` / `workspaces` keys in json, the project-key mismatch warning. |
| `internal/cli/output.go` | Guidance for `workspace_mismatch`, `workspace_required`, and `not_found` under a selector. |
| `internal/cli/interactive.go` | Hands the resolved selector to the TUI. |
| `internal/client/client.go` | `WithWorkspace`, the `scoped` path builder, the `Workspace` type and the workspace fields on `Whoami`. |
| `internal/tui/model.go` | Workspace state, `applyWhoami` / `resolveWorkspace`, the picker items. |
| `internal/tui/update.go` | `pickWorkspace`, `openWorkspacePicker`, `switchWorkspace`. |
| `internal/tui/view.go` | The breadcrumb header and the `w` hint / help line. |
| `internal/tui/msgs.go` | `whoamiCmd` and `whoamiLoadedMsg`. |
| `internal/cli/workspace_test.go`, `internal/client/workspace_test.go`, `internal/tui/workspace_test.go` | Coverage for the three layers; the client tests pin which routes are prefixed. |

## Noteworthy

### The selector never satisfies the narrowing rule

`newAuthedClient` checks for a project key or agent secret first and only then resolves the workspace. With neither factor set, a request still fails locally exactly as before — the selector is applied on top of the factors, never instead of one.

### Only workspace-bound routes are prefixed

The server mounts task, checklist-item and activity routes under both `/api/v1` and `/api/v1/workspaces/<id>`; user-level routes exist only flat. Prefixing `whoami` or `settings/theme` would be a 404, so `scoped` is applied per route, not to the base URL.

### `whoami` ignores the selector

Its `workspace` block is always the default for the factors. Both `whoami` and `workspace list` therefore match a selection against the `workspaces` list locally and fail plainly when it is absent — the server would only ever answer a task call with a 404, and it never reveals which workspaces exist. A server that sends no `workspaces` list at all makes both commands fail the same way when a selector is set; without one, `whoami` still prints whatever default the server reported.

### The TUI treats workspace 403s as errors, not credential failures

`workspace_mismatch` and `workspace_required` are 403s the credentials prompt cannot fix (it has no workspace field), so `validationErrMsg` / `opErrMsg` turn them into a footer error naming the selector instead of bouncing to the prompt. Responses also remember the client that issued them: after a switch (or a re-auth) replaces the client, a list, search or whoami answer from the old one is dropped rather than rendered under the new workspace's header, and a picker requested on the list is not opened once the user has drilled into a task.

### Ids are per workspace

A task or item id obtained under one selector is meaningless under another. The 404 guidance under a selector names both possibilities (wrong id, or a workspace the key can't reach) for that reason; without a selector a 404 is left unexplained. `task delete` / `item delete` keep treating a 404 as idempotent success (exit 0, `existed: false`), but under a selector the text line reads `No task #17 in workspace 3 (nothing deleted — …)` instead of "already gone", mirroring the project-key wording, because the record may well exist in another workspace.

### A project key pins the workspace

Under a key the server refuses any other selector with `403 workspace_mismatch`. The CLI does not pre-check this (the key's workspace is only known after `whoami`), but `whoami` warns when the two disagree and the TUI's `w` refuses to open the picker under a key rather than offer a switch that can only fail.

### An older server is tolerated, never faked

A backend without workspaces answers `whoami` with neither `workspace` nor `workspaces`; those decode to nil. The CLI then omits the keys and lines rather than emitting empty ones, `workspace list` errors plainly, and the TUI header stays unnamed. Likewise `project_permissions` is emitted only when the server sends it.

### Switching in the TUI is a full reset

`switchWorkspace` builds a new client with the same factors and the new selector, then clears the task list, the cached detail, the pending fetch and the search state before refetching — nothing loaded under the old workspace is valid under the new one. The choice is memory-only and is re-applied when the credentials prompt rebuilds the client, so a mid-session re-auth does not silently drop back to the default workspace. A target where the key resolves to `none` still switches (the server allows it); the footer says why the list will be empty.

## Testing

`internal/cli/workspace_test.go` covers resolution and the pre-HTTP rejection, path prefixing from the command layer, the not-a-factor rule, `workspace list` and `whoami` under a selection, and the error guidance; `internal/client/workspace_test.go` pins exactly which routes are prefixed, that an empty selector is a no-op, that the narrowing headers survive, and `Whoami` decoding with and without the workspace keys; `internal/tui/workspace_test.go` drives the header, picker, switch, no-op, level-`none` footer, project-key pin, older-server case and survival across the credentials prompt. All run against `httptest`; no backend is required.
