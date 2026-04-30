# Activity — Technical

## Source Files

| File | Role |
|------|------|
| `internal/client/client.go` | `GetActivityLog(ctx, date, daysAgo)` + `ActivityLog` / `ActivityChecklistItem` / `ActivityItemTask` types. |
| `internal/cli/activity.go` | Cobra subcommand; flag validation, payload assembly, two-table text view. |
| `internal/cli/activity_test.go` | Coverage for flag validation, payload shape, and text rendering. |

## Noteworthy Behavior

- **Pre-flight validation mirrors the server but stays narrow.** `validateActivityParams` enforces mutual exclusion, non-negative integer `--days-ago`, and `YYYY-MM-DD` parseability — nothing more. The 365-day upper bound is intentionally left to the server so the CLI doesn't drift if the cap moves.
- **Tasks reuse the existing task renderer.** `activityPayload` runs each completed task through `taskMap` and the text view through `taskRowFields` / `taskListHeader`, so the activity feed is shape-identical to `task list` output. This is deliberate: a single client-side mapper handles both feeds.
- **Items carry a trimmed parent-task summary.** `ActivityChecklistItem.Task` is `ActivityItemTask` (id + title + project), not the full `Task` — the server already knows the consumer doesn't need the rest, and serialising it would bloat a feed that can span hundreds of items.
- **`status: "ok"` is preserved on output.** The wire response has no envelope; the CLI re-injects the field in `activityPayload` so JSON / TOON consumers see the same envelope shape as `whoami`, `task list`, etc.
- **Empty days collapse to a single line.** `activityText` short-circuits when both arrays are empty and prints `activity for <date>\n(no activity)` instead of two empty headers — keeps the "nothing happened" case scannable.
