package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// newWorkspaceCmd is the `workspace` noun. A workspace is a resource on the
// wire, selected per request with --workspace / $SPRAWL_WORKSPACE; the noun
// only exists to discover ids (and, later, anything else workspace-level).
// There is deliberately no `workspace use` that writes the choice to disk:
// like the project key, which workspace a shell works in is the shell's job
// (an exported variable, a direnv file, a wrapper), so there is exactly one
// place scope comes from.
func newWorkspaceCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Discover the workspaces you can reach and which one this session works in",
		Long: "A workspace is an independent canvas of tasks and projects. Task, item, queue and " +
			"activity calls run in the workspace your factors resolve to by default — the " +
			"confined project's under a project key, else the agent key's own. Pass " +
			"--workspace <id> (or export SPRAWL_WORKSPACE) to work in another one; ids come " +
			"from `workspace list`.\n\n" +
			"Ids are per workspace: a task or item id read under --workspace 3 must be addressed " +
			"under --workspace 3 again, or the server answers 404.",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.SilenceErrors = true
	cmd.AddCommand(newWorkspaceListCmd(opts))
	return cmd
}

func newWorkspaceListCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every workspace you can reach, marking the one this session works in (GET /api/v1/whoami)",
		Long: "Lists the workspaces the logged-in user owns or is a member of, with `role` (the user's " +
			"standing there) and `level` (what the presented agent key can actually do there — " +
			"a workspace-bound key reads `none` everywhere but its own). The current workspace " +
			"is the --workspace / $SPRAWL_WORKSPACE selection when set, else the server's default " +
			"for the factors in use. Read through `whoami`, which carries the list and the default " +
			"in one call.",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceList(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runWorkspaceList(ctx context.Context, stdout, stderr io.Writer, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	w, err := c.Whoami(ctx)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	// The list is what gets rendered and what a selection resolves against;
	// without it there is nothing to show, whatever else the server sent.
	if w.Workspaces == nil {
		return reportErr(stdout, stderr, errServerNoWorkspaces, opts)
	}
	cur, err := effectiveWorkspace(w, c.Workspace())
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	payload := map[string]any{
		"status":     "ok",
		"current":    workspaceMap(cur),
		"workspaces": workspaceMaps(w.Workspaces),
	}
	return renderPayload(stdout, payload, workspaceListText(w.Workspaces, cur, w.Project != nil), opts)
}

// errServerNoWorkspaces is the pre-workspaces-server case: whoami came back
// without the reachable list, so there is nothing to list and no way to
// confirm a --workspace selection.
var errServerNoWorkspaces = errors.New("this server doesn't report workspaces yet — update the backend, or drop --workspace")

// effectiveWorkspace picks the workspace this session's calls actually run
// in: the --workspace selection resolved against the reachable list, else the
// server's default. whoami is a user-level route and doesn't take the
// selector, so a selected id has to be matched here. An id outside the list
// is the same failure the server would give every task call (404) — said
// plainly instead, and before any of those calls.
func effectiveWorkspace(w *client.Whoami, selected string) (*client.Workspace, error) {
	if selected == "" {
		return w.Workspace, nil
	}
	id, err := strconv.ParseInt(selected, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace %q: want a numeric id", selected)
	}
	for i := range w.Workspaces {
		if w.Workspaces[i].ID == id {
			return &w.Workspaces[i], nil
		}
	}
	return nil, fmt.Errorf("workspace %s is not one you can reach — run `%s workspace list` to see your workspaces", selected, build.AppName)
}

func workspaceMap(ws *client.Workspace) any {
	if ws == nil {
		return nil
	}
	return map[string]any{
		"id":    ws.ID,
		"name":  ws.Name,
		"role":  ws.Role,
		"level": ws.Level,
	}
}

func workspaceMaps(list []client.Workspace) []any {
	out := make([]any, 0, len(list))
	for i := range list {
		out = append(out, workspaceMap(&list[i]))
	}
	return out
}

var workspaceListHeader = []string{"", "ID", "NAME", "ROLE", "LEVEL"}

// workspaceListText is the human table: a `›` on the current workspace, then
// id / name / role / level. Under a project key the LEVEL column is dropped —
// confinement rules out workspace-wide work, so the server reports `none` on
// every workspace, and printing that would read as "no access".
func workspaceListText(list []client.Workspace, cur *client.Workspace, confined bool) string {
	if len(list) == 0 {
		return sty.render(sty.faint, "(no workspaces)")
	}
	header := workspaceListHeader
	if confined {
		header = header[:len(header)-1]
	}
	rows := make([][]col, 0, len(list))
	for i := range list {
		ws := &list[i]
		mark := ""
		if cur != nil && ws.ID == cur.ID {
			mark = "›"
		}
		row := []col{
			styledCol(mark, sty.accent),
			plainCol(strconv.FormatInt(ws.ID, 10)),
			plainCol(fallback(ws.Name, "(unnamed)")),
			plainCol(fallback(ws.Role, "-")),
		}
		if !confined {
			row = append(row, levelCol(ws.Level))
		}
		rows = append(rows, row)
	}
	return renderTable(header, rows)
}

// levelCol colors a workspace level: `none` is the one value worth a warning,
// because it means every list in that workspace comes back empty.
func levelCol(level string) col {
	if level == "" || level == "none" {
		return styledCol("none", sty.warn)
	}
	return plainCol(level)
}
