package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"
)

func newWorkspaceActorsCmd(opts *runtimeOpts) *cobra.Command {
	return &cobra.Command{
		Use: "actors [id]", Short: "Discover eligible item assignees in a workspace",
		Long: "List assignee type, id, label and marker. Requires write access. " +
			"Uses the positional workspace id, --workspace / $SPRAWL_WORKSPACE, or whoami's " +
			"default workspace, in that order. A positional id conflicting with the selector " +
			"is a local error. Use the same --workspace for subsequent item writes; " +
			"discovery does not change this shell's selection or reserve eligibility.",
		Args: textArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) != 0 {
				id = args[0]
			}
			return runWorkspaceActors(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), id, opts)
		},
		SilenceErrors: true,
	}
}

func runWorkspaceActors(ctx context.Context, stdout, stderr io.Writer, id string, opts *runtimeOpts) error {
	selected, err := resolveWorkspace(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	if id != "" {
		if !isPositiveInt(id) {
			return reportErr(stdout, stderr, fmt.Errorf("invalid workspace %q: want a positive decimal id", id), opts)
		}
		if selected != "" && id != selected {
			return reportErr(stdout, stderr, fmt.Errorf("workspace actors %s conflicts with workspace selector %s", id, selected), opts)
		}
	} else {
		id = selected
	}
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	if id == "" {
		w, err := c.Whoami(ctx)
		if err != nil {
			return reportErr(stdout, stderr, err, opts)
		}
		if w.Workspace == nil || w.Workspace.ID <= 0 {
			return reportErr(stdout, stderr, errServerNoWorkspaces, opts)
		}
		id = strconv.FormatInt(w.Workspace.ID, 10)
	}
	actors, err := c.ListWorkspaceActors(ctx, id)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	list := make([]any, 0, len(actors))
	rows := make([][]col, 0, len(actors))
	for _, actor := range actors {
		list = append(list, map[string]any{
			"type": actor.Type, "id": actor.ID, "label": actor.Label, "marker": actor.Marker,
		})
		rows = append(rows, []col{plainCol(assigneeText(&actor.Assignee)), plainCol(actor.Label), plainCol(actor.Marker)})
	}
	text := "(no eligible assignees)"
	if len(rows) > 0 {
		text = renderTable([]string{"ASSIGNEE", "LABEL", "MARKER"}, rows)
	}
	return renderPayload(stdout, map[string]any{"actors": list}, text, opts)
}
