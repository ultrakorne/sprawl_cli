# Whoami — Technical

## Architecture

`internal/cli/whoami.go` builds the authed client, calls `Whoami`, resolves the effective workspace through `effectiveWorkspace` (from `internal/cli/workspace.go`) when a selector is set, and renders the payload and text view. `internal/client/client.go` decodes the wire shape into `Whoami` with nilable `Workspace` / `Workspaces` / `Project` / `ProjectPermissions` so an older server's missing fields are distinguishable from empty ones.

## Where things live

| File | Role |
|------|------|
| `internal/client/client.go` | `Whoami(ctx)`; the `Whoami`, `Agent`, `Workspace`, `WhoamiProject`, `ProjectPermission` types. |
| `internal/cli/whoami.go` | The command; payload assembly, `whoamiProjectMap`, the text view and its grouping. |
| `internal/cli/workspace.go` | `effectiveWorkspace`, `workspaceMap` / `workspaceMaps`, shared with `workspace list`. |
| `internal/cli/whoami_test.go`, `internal/cli/workspace_test.go` | Text and json coverage, including the mismatch warning and the older-server case. |

## Noteworthy

### Pre-flight failure without an HTTP call

`newAuthedClient` refuses to build a client with neither factor; the error goes through `reportErr` so the format matches wire errors.

### Optional keys mirror the answering server

`workspace` / `workspaces` are emitted only when the server sent at least one of them; `project_permissions` only when present; `project` always (null when unconfined). `github_url` inside the project block is always emitted, null when unset. The rule is one condition per feature, never two.

### The selected workspace replaces the default in output

With a selector, `effectiveWorkspace` swaps the server's default for the matched entry in `workspaces`, so the `workspace` key describes what this session's task calls will hit. An unmatched selection is an error here, plainly worded, instead of a later 404.

### Text grouping

`project_permissions` collapse to one line per level (`write_create`, then `write`, then `read`, then anything custom), names in the server's order. Under a project key the workspace `access:` line is omitted — confinement makes every workspace level `none` by construction and the project's own level is the one that matters.

### The mismatch warning needs three facts at once

The `workspace_mismatch` warning is printed only when a selector is set, the call ran under a project key, *and* the server's default workspace (the project's) differs from the selected one. Without the project block the key's workspace is unknown, and without a selector there is nothing to disagree with — so the line never fires spuriously on an unconfined or unselected session.

### `status: "ok"` is re-injected

The client drops the redundant status flag on decode; the CLI puts it back so json consumers see the full server contract.
