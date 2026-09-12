# Workspaces — Design

## Overview

A workspace is an independent canvas of tasks and projects. A user owns one and may be a member of others, and every task, item, queue and activity call runs in exactly one of them. Without a selector a call lands in the **default workspace** — the confined project's workspace under a project key, else the agent key's own. `--workspace <id>` / `-w <id>` (or `SPRAWL_WORKSPACE`) runs the call in another one. The idea that shapes the feature: the workspace is a **selector, not a narrowing factor** — it changes *where* a call runs and nothing about *what the caller may do* there.

## Surface

- **`-w`, `--workspace <id>`** — persistent flag on every command; `SPRAWL_WORKSPACE` is the session-wide fallback. The value must be a positive integer id; anything else is refused before a request goes out, with a pointer at `workspace list`.
- **`workspace list`** — every workspace the user can reach, `›` marking the one this session works in. Columns `ID NAME ROLE LEVEL`: **role** is the user's own standing there (`owner`, or the membership level), **level** is what the presented agent key actually resolves to. `LEVEL` is dropped under a project key. JSON: `{"status":"ok","current":{…},"workspaces":[…]}`.
- **`whoami`** — prints `workspace:` (or `workspace (selected):` when a selector is set) with the user's role and the key's access there, and warns when a project key's workspace disagrees with the selection.
- **TUI** — the task-list header reads `sprawl · <workspace> · tasks[ · <project key>]`; `w` opens a picker of reachable workspaces and switches the session.
- **Errors** — `workspace_mismatch` (a project key lives in another workspace than the selector), `workspace_required` (the agent key's own workspace was deleted), and a 404 under a selector, which can mean either the id or the workspace is out of reach. Each carries a plain-language hint (see [output-formats](../output-formats/INDEX.md)).

## Flows

- **Discover and pin** — `sprawl workspace list`, read the id, `export SPRAWL_WORKSPACE=<id>` in the shell (or `.envrc`) that should work there. Nothing is written by sprawl.
- **Work across workspaces** — a task or item id is only meaningful in the workspace it was read from: `task 17` read under `-w 3` must be addressed under `-w 3` again, or the server answers 404.
- **Switch in the TUI** — `w` refetches the workspace picture, opens the picker on the current one, and `enter` re-pins the session: a new client, an empty list refetched from the new workspace, nothing from the old one kept. Choosing a workspace where the key resolves to `none` still switches, with a footer line saying why the list will be empty. The choice lives in memory only.
- **Under a project key** — the key already pins its project's workspace. A different selector is refused server-side with `workspace_mismatch`; `w` in the TUI says the key pins the workspace instead of offering a choice, and `whoami` warns when the two disagree.
- **Older server** — a backend that reports no workspaces yields `workspace list` failing plainly, `whoami` without workspace lines, an unnamed TUI header, and `w` saying the server doesn't report workspaces.

## On the wire

A selection is a per-request **path prefix**, not a header: `/api/v1/workspaces/<id>/tasks` instead of `/api/v1/tasks`, on every task, checklist-item and activity-log route. User-level routes — `whoami`, `workspaces`, `settings/theme` — are flat only. The narrowing headers (`X-Agent-Secret`, `X-Project-Key`) go out unchanged. `workspace list` answers `{"status":"ok","current":{id,name,role,level},"workspaces":[…]}`, read from the flat `whoami` route.

## Decisions

- **A selector, not a factor.** The bearer-plus-one-narrowing-factor rule is untouched: a workspace on its own still fails before the HTTP call. Permission is decided by the factors; the workspace only decides which canvas they apply to.
- **Never persisted, and no `workspace use`.** Like the project key, which workspace a shell works in is the shell's job. A stored choice would make "which workspace am I in?" depend on state the user forgot about, and would be a second place scope comes from.
- **Validated locally as a positive integer.** The server answers a malformed id with a bare 404 that reads as "task not found"; the CLI names the real problem before any request.
- **Ids are per workspace.** The server scopes ids to the canvas, so the CLI never tries to look an id up across workspaces — a miss is reported as "not found in workspace N", naming both the id and the workspace as possible causes.
- **Role and level are shown side by side.** A shared workspace where the user is a member but the presented key is bound elsewhere reads `role: write`, `level: none`; showing only one of the two would either hide why lists are empty or overstate what the key can do.
- **`LEVEL` is hidden under a project key.** Confinement makes every workspace's level `none` by construction; printing that would read as "no access" when the project's own level is what matters.
- **`workspace list` reads through `whoami`.** One call carries both the reachable list and the default, so the current workspace can be marked without a second request.
