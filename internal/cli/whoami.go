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
		Short: "Show the calling agent and its elevated project permissions (GET /api/v1/whoami)",
		Args:  textArgs(cobra.NoArgs),
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
	payload := whoamiPayload(w)
	return renderPayload(stdout, payload, whoamiText(w), opts)
}

// whoamiPayload preserves the wire shape (`status: ok`, `agent`,
// `project_permissions`) for json / toon. We re-encode `agent` as a map so
// gotoon and json see the same structure other commands hand them.
func whoamiPayload(w *client.Whoami) map[string]any {
	perms := make([]any, 0, len(w.ProjectPermissions))
	for _, p := range w.ProjectPermissions {
		perms = append(perms, map[string]any{
			"project_id": p.ProjectID,
			"name":       p.Name,
			"level":      p.Level,
		})
	}
	return map[string]any{
		"status": "ok",
		"agent": map[string]any{
			"id":                 w.Agent.ID,
			"name":               w.Agent.Name,
			"emoji":              w.Agent.Emoji,
			"is_owner":           w.Agent.IsOwner,
			"default_permission": w.Agent.DefaultPermission,
		},
		"project_permissions": perms,
	}
}

// whoamiText is the human-friendly view: who you are + which projects (if any)
// elevate your default scope. Mirrors the shape of taskDetailText so the
// command line stays familiar.
func whoamiText(w *client.Whoami) string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent: %s #%d\n", agentLabel(w.Agent), w.Agent.ID)
	if w.Agent.IsOwner {
		fmt.Fprintln(&b, "  role:    owner")
	} else {
		fmt.Fprintf(&b, "  default: %s\n", fallback(w.Agent.DefaultPermission, "-"))
	}
	if len(w.ProjectPermissions) == 0 {
		fmt.Fprint(&b, "elevated project permissions: (none)")
		return b.String()
	}
	fmt.Fprintln(&b, "elevated project permissions:")
	for _, line := range groupedPermissionLines(w.ProjectPermissions) {
		fmt.Fprintf(&b, "  - %s\n", line)
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
