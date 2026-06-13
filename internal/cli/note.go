package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newNoteCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "note",
		Short: "Read or write checklist-item notes",
	}
	cmd.AddCommand(newNoteShowCmd(opts))
	cmd.AddCommand(newNoteSetCmd(opts))
	return cmd
}

func newNoteShowCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <item_id>",
		Short: "Fetch the notes blob for a checklist item (GET /api/v1/checklist_items/:id/notes)",
		Args:  textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteShow(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newNoteSetCmd(opts *runtimeOpts) *cobra.Command {
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "set <item_id> [<notes>]",
		Short: "Replace the notes blob on a checklist item (PUT /api/v1/checklist_items/:id/notes)",
		Long: "Set the notes on a checklist item. Pass the notes text as the second positional " +
			"argument, or use --stdin to read the entire body from stdin (useful for multi-line / " +
			"piped input). An empty string clears existing notes and is a valid value. Size cap is " +
			"enforced server-side (422 on overflow).",
		// Body source is checked in RunE so the --stdin / positional combo
		// rule can emit a format-aware error; cobra's arg validators only
		// know about counts.
		Args: textArgs(cobra.RangeArgs(1, 2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
			itemID := args[0]
			notes, err := resolveNoteBody(cmd.InOrStdin(), args[1:], fromStdin)
			if err != nil {
				return reportErr(stdout, stderr, err, opts)
			}
			return runNoteSet(cmd.Context(), stdout, stderr, itemID, notes, opts)
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

func runNoteSet(ctx context.Context, stdout, stderr io.Writer, itemID, notes string, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	saved, err := c.SetNotes(ctx, itemID, notes)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	// saved == nil ⇒ the notes were cleared ⇒ emit JSON/TOON null, matching the
	// server and `task/checklist --full`. Text mirrors `note show`: the saved
	// blob verbatim, or a terse "(notes cleared)" line when empty so piping
	// into less / rg stays natural.
	var savedVal any
	text := sty.render(sty.faint, "(notes cleared)")
	if saved != nil {
		savedVal = *saved
		text = *saved
	}
	payload := map[string]any{"notes": savedVal}
	return renderPayload(stdout, payload, text, opts)
}

func runNoteShow(ctx context.Context, stdout, stderr io.Writer, itemID string, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	notes, err := c.GetNotes(ctx, itemID)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	// notes == nil ⇒ the item has no notes ⇒ emit JSON/TOON null, matching the
	// server and `task/checklist --full`. Text fallback is the raw blob (empty
	// when nil) — the whole point of the endpoint, and wrapping it would only
	// get in the way of `| less` etc.
	var notesVal any
	text := ""
	if notes != nil {
		notesVal = *notes
		text = *notes
	}
	payload := map[string]any{"notes": notesVal}
	return renderPayload(stdout, payload, text, opts)
}
