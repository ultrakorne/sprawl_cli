package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func newChecklistCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checklist <task_id>",
		Short: "List checklist items for a task (GET /api/v1/tasks/:task_id/checklist)",
		Long: "List checklist items for a task. Permission cascade is enforced on the parent task — 403 if the " +
			"caller can't read the task, 404 if it isn't visible to them at all.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(),
					errors.New("checklist requires exactly one argument: the task id"))
			}
			return runChecklist(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0])
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runChecklist(ctx context.Context, stdout, stderr io.Writer, taskID string) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	items, err := c.ListChecklistItems(ctx, taskID)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	payload := map[string]any{"checklist_items": checklistMaps(items)}
	return renderPayload(stdout, payload, checklistItemsText(items))
}

func checklistMaps(items []*client.ChecklistItem) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"id":         it.ID,
			"title":      it.Title,
			"completed":  it.Completed,
			"position":   it.Position,
			"has_notes":  it.HasNotes,
			"last_actor": actorMap(it.LastActor),
		})
	}
	return out
}

func checklistItemsText(items []*client.ChecklistItem) string {
	if len(items) == 0 {
		return "(no checklist items)"
	}
	var buf strings.Builder
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tPOS\t \tNOTES\tTITLE")
	for _, it := range items {
		fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\n",
			it.ID,
			it.Position,
			checkbox(it.Completed),
			notesFlag(it.HasNotes),
			it.Title,
		)
	}
	_ = tw.Flush()
	return strings.TrimRight(buf.String(), "\n")
}

func checkbox(done bool) string {
	if done {
		return "[x]"
	}
	return "[ ]"
}

func notesFlag(has bool) string {
	if has {
		return "notes"
	}
	return "-"
}
