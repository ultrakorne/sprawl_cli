package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newNoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "note",
		Short: "Read or write checklist-item notes",
	}
	cmd.AddCommand(newNoteShowCmd())
	cmd.AddCommand(newNoteSetCmd())
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

func newNoteSetCmd() *cobra.Command {
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "set <item_id> [<notes>]",
		Short: "Replace the notes blob on a checklist item (PUT /api/v1/checklist_items/:id/notes)",
		Long: "Set the notes on a checklist item. Pass the notes text as the second positional " +
			"argument, or use --stdin to read the entire body from stdin (useful for multi-line / " +
			"piped input). An empty string clears existing notes and is a valid value. Size cap is " +
			"enforced server-side (422 on overflow).",
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
			if len(args) < 1 {
				return reportErr(stdout, stderr,
					errors.New("note set requires the checklist item id as the first argument"))
			}
			itemID := args[0]
			notes, err := resolveNoteBody(cmd.InOrStdin(), args[1:], fromStdin)
			if err != nil {
				return reportErr(stdout, stderr, err)
			}
			return runNoteSet(cmd.Context(), stdout, stderr, itemID, notes)
		},
	}
	cmd.Flags().BoolVar(&fromStdin, "stdin", false,
		"read the notes body from stdin instead of a positional argument")
	cmd.SilenceErrors = true
	return cmd
}

// resolveNoteBody picks the notes payload from either stdin or a positional
// arg. `--stdin` and a second positional arg are mutually exclusive — keeping
// the input path explicit avoids surprise when shell pipes and arg quoting
// disagree.
func resolveNoteBody(stdin io.Reader, extraArgs []string, fromStdin bool) (string, error) {
	switch {
	case fromStdin && len(extraArgs) > 0:
		return "", errors.New("pass notes via --stdin OR a positional argument, not both")
	case fromStdin:
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(b), nil
	case len(extraArgs) == 1:
		return extraArgs[0], nil
	case len(extraArgs) > 1:
		return "", errors.New("note set accepts at most one positional notes argument; quote multi-word notes")
	default:
		return "", errors.New("note set requires notes text: pass a second argument or use --stdin")
	}
}

func runNoteSet(ctx context.Context, stdout, stderr io.Writer, itemID, notes string) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	saved, err := c.SetNotes(ctx, itemID, notes)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	payload := map[string]any{"notes": saved}
	// Text fallback mirrors `note show`: emit the saved notes blob verbatim
	// so piping into less / rg stays natural. Fall back to a terse summary
	// when the server echoed an empty body.
	text := saved
	if text == "" {
		text = "(notes cleared)"
	}
	return renderPayload(stdout, payload, text)
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
