package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func newWhoamiCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the calling agent, the workspace and project this session works in, and your access there (GET /api/v1/whoami)",
		Long: "Show who the server thinks you are and where this session's calls land.\n\n" +
			"The workspace line names the workspace task, item, queue and activity calls run in: " +
			"the server's default for your factors, or the --workspace / $SPRAWL_WORKSPACE " +
			"selection resolved against the workspaces you can reach (an unreachable id fails " +
			"here, before any task call). It carries your role there and the level this agent " +
			"key resolves to — `none` means every list there comes back empty.\n\n" +
			"Under a project key (--project-key / $SPRAWL_PROJECT_KEY) the output also names the " +
			"project this session is confined to and the level you resolve to there — including " +
			"`none`, which means the key is valid but this agent has no access to that project — " +
			"and warns when a --workspace selection names a workspace other than the project's " +
			"(the server refuses that with workspace_mismatch). Older servers that report " +
			"per-project permission overrides instead get those listed.",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWhoami(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runWhoami(ctx context.Context, stdout, stderr io.Writer, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	w, err := c.Whoami(ctx)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	// whoami is user-level and ignores the workspace selector, so the
	// workspace it names is the default. Resolve the selected one (matched
	// against the reachable list) so the answer describes what THIS session's
	// task calls will hit — an unreachable selection fails here, plainly.
	// A selection with no list to confirm it against is the same failure
	// `workspace list` reports: labelling the server's default "(selected)"
	// would describe a workspace this session's calls won't hit.
	eff, selected := w.Workspace, false
	if c.Workspace() != "" {
		if w.Workspaces == nil {
			return reportErr(stdout, stderr, errServerNoWorkspaces, opts)
		}
		eff, err = effectiveWorkspace(w, c.Workspace())
		if err != nil {
			return reportErr(stdout, stderr, err, opts)
		}
		selected = true
	}
	payload := whoamiPayload(w, eff)
	return renderPayload(stdout, payload, whoamiText(w, eff, selected), opts)
}

// whoamiPayload preserves the wire shape (`status: ok`, `agent`, `workspace`,
// `workspaces`, `project`) for json. We re-encode `agent` as a map so json
// sees the same structure other commands hand them. The workspace keys are
// emitted only when the server sent them, and `project_permissions` (what
// pre-workspaces servers report instead) likewise — so the envelope mirrors
// whichever server answered rather than inventing empty keys. `eff` is the
// workspace this session's calls run in: the server's default, or the
// --workspace selection resolved by the caller.
func whoamiPayload(w *client.Whoami, eff *client.Workspace) map[string]any {
	payload := map[string]any{
		"status": "ok",
		"agent": map[string]any{
			"id":                 w.Agent.ID,
			"name":               w.Agent.Name,
			"emoji":              w.Agent.Emoji,
			"is_owner":           w.Agent.IsOwner,
			"default_permission": w.Agent.DefaultPermission,
		},
		"project": whoamiProjectMap(w.Project),
	}
	if w.Workspace != nil || w.Workspaces != nil {
		payload["workspace"] = workspaceMap(eff)
		payload["workspaces"] = workspaceMaps(w.Workspaces)
	}
	if w.ProjectPermissions != nil {
		perms := make([]any, 0, len(w.ProjectPermissions))
		for _, p := range w.ProjectPermissions {
			perms = append(perms, map[string]any{
				"project_id": p.ProjectID,
				"name":       p.Name,
				"level":      p.Level,
			})
		}
		payload["project_permissions"] = perms
	}
	return payload
}

// whoamiProjectMap mirrors the server's `project` key: the confinement this
// call ran under, or a literal null when no project key was sent (which is
// also what a pre-project-keys server reports).
func whoamiProjectMap(p *client.WhoamiProject) any {
	if p == nil {
		return nil
	}
	return map[string]any{
		"id":         p.ID,
		"name":       p.Name,
		"key":        p.Key,
		"level":      p.Level,
		"github_url": nilIfEmpty(p.GithubURL),
	}
}

// whoamiText is the human-friendly view: who you are, the workspace your calls
// land in, the project you're confined to (if any), and which projects (if
// any) elevate your default scope. Mirrors the shape of the task detail view
// so the command line stays familiar. `eff` is the workspace this session's
// calls run in (the server's default, or the --workspace selection resolved
// by the caller); `selected` says which of the two it is, which decides the
// label and whether a project-key conflict is worth warning about.
func whoamiText(w *client.Whoami, eff *client.Workspace, selected bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n",
		sty.render(sty.faint, "agent:"),
		sty.render(sty.bold, fmt.Sprintf("%s #%d", agentLabel(w.Agent), w.Agent.ID)))
	if w.Agent.IsOwner {
		fmt.Fprintf(&b, "  %s    %s\n", sty.render(sty.faint, "role:"), sty.render(sty.ok, "owner"))
	} else {
		fmt.Fprintf(&b, "  %s %s\n", sty.render(sty.faint, "default:"), fallback(w.Agent.DefaultPermission, "-"))
	}
	// Only printed when the server reports workspaces — an older server reads
	// exactly as it did before workspaces existed.
	if ws := eff; ws != nil {
		label := "workspace:"
		if selected {
			label = "workspace (selected):"
		}
		fmt.Fprintf(&b, "%s %s\n",
			sty.render(sty.faint, label),
			sty.render(sty.bold, fmt.Sprintf("%s #%d", fallback(ws.Name, "(unnamed)"), ws.ID)))
		fmt.Fprintf(&b, "  %s    %s\n", sty.render(sty.faint, "role:"), fallback(ws.Role, "-"))
		switch {
		case w.Project != nil:
			// Under a project key every workspace level is `none` by
			// construction (confinement rules out workspace-wide work), and
			// the project's own level below is the one that matters.
		case ws.Level == "" || ws.Level == "none":
			fmt.Fprintf(&b, "  %s  %s\n",
				sty.render(sty.faint, "access:"),
				sty.render(sty.warn, "none — this agent key can't reach that workspace, so every list there is empty"))
		default:
			fmt.Fprintf(&b, "  %s  %s\n", sty.render(sty.faint, "access:"), ws.Level)
		}
		if selected && w.Project != nil && w.Workspace != nil && w.Workspace.ID != ws.ID {
			// A project key pins its project's workspace (which is what the
			// server's default is under a key); naming another one is rejected
			// on every task call. Say so here, where it's cheap.
			fmt.Fprintf(&b, "  %s %s\n",
				sty.render(sty.faint, "warning:"),
				sty.render(sty.warn, fmt.Sprintf("project key %q lives in workspace #%d — task calls in workspace #%d will be refused (workspace_mismatch)",
					w.Project.Key, w.Workspace.ID, ws.ID)))
		}
	}
	// Only printed when the call ran under a project key — an unconfined
	// whoami reads exactly as it did before project keys existed.
	if p := w.Project; p != nil {
		fmt.Fprintf(&b, "%s %s\n",
			sty.render(sty.faint, "working in:"),
			sty.render(sty.bold, fmt.Sprintf("%s (%s)", fallback(p.Name, "(unnamed)"), p.Key)))
		if p.Level == "" || p.Level == "none" {
			// A valid key naming a project this agent can't reach: not an auth
			// error server-side, so say it plainly rather than letting the user
			// puzzle over empty task lists.
			fmt.Fprintf(&b, "  %s   %s\n",
				sty.render(sty.faint, "access:"),
				sty.render(sty.warn, "none — the key is valid but this agent has no access to that project"))
		} else {
			fmt.Fprintf(&b, "  %s   %s\n", sty.render(sty.faint, "access:"), p.Level)
		}
		// The repo PR links are built against. Printed only when set — a project
		// without one renders PR numbers bare, which is not an error.
		if p.GithubURL != "" {
			fmt.Fprintf(&b, "  %s   %s\n", sty.render(sty.faint, "github:"), p.GithubURL)
		}
	}
	// Pre-workspaces servers list per-project elevations; a server that
	// doesn't send the key gets no section at all.
	if w.ProjectPermissions != nil {
		header := sty.render(sty.bold, "elevated project permissions:")
		if len(w.ProjectPermissions) == 0 {
			fmt.Fprintf(&b, "%s %s\n", header, sty.render(sty.faint, "(none)"))
		} else {
			fmt.Fprintln(&b, header)
			for _, line := range groupedPermissionLines(w.ProjectPermissions) {
				fmt.Fprintf(&b, "  - %s\n", line)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// agentLabel renders the human bit of an agent identity ("🤖 my-agent" or
// "my-agent" when the server didn't set an emoji).
func agentLabel(a client.Agent) string {
	name := fallback(a.Name, "(unnamed)")
	if strings.TrimSpace(a.Emoji) == "" {
		return name
	}
	return a.Emoji + " " + name
}

// groupedPermissionLines collapses ProjectPermissions into one line per level,
// preserving the server's project_id ordering inside each group. Output looks
// like "write_create in: foo, bar".
func groupedPermissionLines(perms []client.ProjectPermission) []string {
	type bucket struct {
		level string
		names []string
	}
	order := []string{}
	buckets := map[string]*bucket{}
	for _, p := range perms {
		b, ok := buckets[p.Level]
		if !ok {
			b = &bucket{level: p.Level}
			buckets[p.Level] = b
			order = append(order, p.Level)
		}
		b.names = append(b.names, p.Name)
	}
	// Stable, intuitive ordering: highest scope first, then anything custom.
	rank := map[string]int{"write_create": 0, "write": 1, "read": 2}
	sort.SliceStable(order, func(i, j int) bool {
		ri, oi := rank[order[i]]
		rj, oj := rank[order[j]]
		switch {
		case oi && oj:
			return ri < rj
		case oi:
			return true
		case oj:
			return false
		default:
			return order[i] < order[j]
		}
	})
	out := make([]string, 0, len(order))
	for _, level := range order {
		out = append(out, fmt.Sprintf("%s in: %s", level, strings.Join(buckets[level].names, ", ")))
	}
	return out
}
