# Whoami — Technical

## Source Files

| File | Role |
|------|------|
| `internal/client/client.go` | `Whoami(ctx)` + `Whoami` / `Agent` / `WhoamiProject` / `ProjectPermission` types. |
| `internal/cli/whoami.go` | Cobra subcommand; resolves auth, builds payload + text view (incl. `whoamiProjectMap`). |

## Noteworthy Behavior

- **Pre-flight check fails without an HTTP call.** `newAuthedClient` returns an error when neither a project key nor an agent secret is configured; the command surfaces that error through the same `reportErr` path as wire errors, so the format is consistent.
- **`project` is always present in json / toon output**, `null` when the call was unconfined. A server that predates project keys omits the field entirely, which decodes to the same `nil` — clients branch on one condition, not two.
- **`github_url` follows the same always-emit rule** inside the project block, `null` when the project has no repo *or* the server predates the field. Text output prints a `github:` line only when it's set. The value is the confined project's repo, used to resolve checklist-item PR links — see [item-state-and-pr](../item-state-and-pr/TECHNICAL.md).
- **Text rendering groups by level.** `whoamiText` collapses `project_permissions` into one line per scope (`write_create` first, then `write`, then `read`) so a long override list stays scannable. Within a group, names follow the server's `project_id` ordering — no extra sort.
- **`status: "ok"` is preserved on output.** The client decodes the wire shape into `*Whoami` (dropping the redundant status flag), but the CLI re-injects `"status":"ok"` into the rendered payload so JSON / TOON consumers see the full server contract.
