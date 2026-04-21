package cli

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"
)

func newNoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "note",
		Short: "Read checklist-item notes",
	}
	cmd.AddCommand(newNoteShowCmd())
	return cmd
}

func newNoteShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <item_id>",
		Short: "Fetch the notes blob for a checklist item (GET /api/v1/checklist_items/:id/notes)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(),
					errors.New("note show requires exactly one argument: the checklist item id"))
			}
			return runNoteShow(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0])
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runNoteShow(ctx context.Context, stdout, stderr io.Writer, itemID string) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	notes, err := c.GetNotes(ctx, itemID)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	payload := map[string]any{"notes": notes}
	// Text fallback is the raw notes blob — this is the whole point of the
	// endpoint, and wrapping it would only get in the way of `| less` etc.
	return renderPayload(stdout, payload, notes)
}
