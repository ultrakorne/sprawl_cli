package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func newChecklistCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checklist <task_id>",
		Short: "List or mutate checklist items for a task",
		Long: "With just a task id, lists checklist items (GET /api/v1/tasks/:task_id/checklist). " +
			"Use the `add`, `check`, `uncheck`, and `update` subcommands to mutate items. " +
			"Permission is enforced on the parent task — 403 if the caller can't read/write it, " +
			"404 if it isn't visible to them at all.",
		// The listing behaviour is preserved as the parent RunE so that
		// `sprawl checklist <task_id>` keeps working without an explicit
		// `list` subcommand. cobra routes exact subcommand matches (add /
		// check / uncheck / update) to their own handlers before falling
		// through here.
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChecklist(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
		},
	}
	cmd.SilenceErrors = true
	cmd.AddCommand(newChecklistAddCmd(opts))
	cmd.AddCommand(newChecklistCheckCmd(opts))
	cmd.AddCommand(newChecklistUncheckCmd(opts))
	cmd.AddCommand(newChecklistUpdateCmd(opts))
	cmd.AddCommand(newChecklistDeleteCmd(opts))
	return cmd
}

// checklistItemWriteFlags carries the flag state shared by `checklist add`
// and `checklist update`. Title/notes flags and a --from-json escape hatch.
type checklistItemWriteFlags struct {
	title    string
	notes    string
	fromJSON string
	hasNotes bool
}

func bindChecklistWriteFlags(cmd *cobra.Command, f *checklistItemWriteFlags) {
	cmd.Flags().StringVar(&f.title, "title", "", "checklist item title")
	cmd.Flags().StringVar(&f.notes, "notes", "",
		"checklist item notes (free-form; use `note set` for bulk editing)")
	cmd.Flags().StringVar(&f.fromJSON, "from-json", "",
		"read checklist_item attrs as a JSON object from a file path, or `-` for stdin")
}

func (f *checklistItemWriteFlags) buildAttrs(stdin io.Reader) (map[string]any, error) {
	attrs := map[string]any{}
	if f.fromJSON != "" {
		loaded, err := loadJSONFromSource(f.fromJSON, stdin)
		if err != nil {
			return nil, err
		}
		maps.Copy(attrs, loaded)
	}
	mergeStringFlag(attrs, "title", f.title)
	if f.hasNotes {
		attrs["notes"] = f.notes
	}
	return attrs, nil
}

func newChecklistAddCmd(opts *runtimeOpts) *cobra.Command {
	var f checklistItemWriteFlags
	cmd := &cobra.Command{
		Use:   "add <task_id>",
		Short: "Add a checklist item to a task (POST /api/v1/tasks/:task_id/checklist)",
		Long: "Add a checklist item under a task. Provide --title (required server-side) and " +
			"optional --notes, or pipe a JSON object via `--from-json -`. Position is assigned " +
			"by the server (appended at the end).",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.hasNotes = cmd.Flags().Changed("notes")
			attrs, err := f.buildAttrs(cmd.InOrStdin())
			if err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			if err := requireAttrs(attrs, "checklist add"); err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			return runChecklistAdd(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], attrs, opts)
		},
	}
	bindChecklistWriteFlags(cmd, &f)
	cmd.SilenceErrors = true
	return cmd
}

func newChecklistCheckCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <item_id>",
		Short: "Mark a checklist item completed (PATCH /api/v1/checklist_items/:id/completed)",
		Long: "Mark a checklist item completed. Sends `{\"completed\": true}`. The server is " +
			"idempotent — calling this on an already-completed item is a no-op but still returns " +
			"the current item.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChecklistSetCompleted(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], true, opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newChecklistUncheckCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uncheck <item_id>",
		Short: "Mark a checklist item not completed (PATCH /api/v1/checklist_items/:id/completed)",
		Long: "Mark a checklist item not completed. Sends `{\"completed\": false}`. The server is " +
			"idempotent — calling this on an already-uncompleted item is a no-op but still returns " +
			"the current item.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChecklistSetCompleted(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], false, opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newChecklistUpdateCmd(opts *runtimeOpts) *cobra.Command {
	var f checklistItemWriteFlags
	cmd := &cobra.Command{
		Use:   "update <item_id>",
		Short: "Update a checklist item's title/notes (PATCH /api/v1/checklist_items/:id)",
		Long: "Update a checklist item. Accepts --title, --notes, or `--from-json -`. Completion " +
			"state isn't mutable through this endpoint — use `checklist check` / `checklist " +
			"uncheck` instead.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.hasNotes = cmd.Flags().Changed("notes")
			attrs, err := f.buildAttrs(cmd.InOrStdin())
			if err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			if err := requireAttrs(attrs, "checklist update"); err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			return runChecklistUpdate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], attrs, opts)
		},
	}
	bindChecklistWriteFlags(cmd, &f)
	cmd.SilenceErrors = true
	return cmd
}

// newChecklistDeleteCmd wraps DELETE /api/v1/checklist_items/:id. Hard
// delete — the row goes away and the parent task's completed_at may flip
// (server recomputes). 404 not_found is treated as success so retries are
// no-ops; see runChecklistDelete.
func newChecklistDeleteCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <item_id>",
		Short: "Hard-delete a checklist item (DELETE /api/v1/checklist_items/:id)",
		Long: "Hard-delete a checklist item from its parent task. The server " +
			"recomputes the parent task's completed_at (it may flip to done if " +
			"this was the last unchecked item, or clear if no items remain) and " +
			"broadcasts checklist_item_deleted on PubSub. A 404 (item already " +
			"gone or not visible) is treated as success but the payload sets " +
			"`existed: false` so callers can distinguish a real delete from a " +
			"no-op retry / typo'd id.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChecklistDelete(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runChecklistDelete(ctx context.Context, stdout, stderr io.Writer, itemID string, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	existed := true
	if err := c.DeleteChecklistItem(ctx, itemID); err != nil {
		if !isNotFoundAPIError(err) {
			return reportErr(stdout, stderr, err, opts)
		}
		existed = false
	}
	payload := map[string]any{"id": itemID, "deleted": true, "existed": existed}
	return renderPayload(stdout, payload, deletedText("checklist item", itemID, existed), opts)
}

func runChecklistAdd(ctx context.Context, stdout, stderr io.Writer, taskID string, attrs map[string]any, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	item, err := c.CreateChecklistItem(ctx, taskID, attrs)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return renderChecklistItem(stdout, item, opts)
}

func runChecklistSetCompleted(ctx context.Context, stdout, stderr io.Writer, itemID string, completed bool, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	item, err := c.SetChecklistItemCompleted(ctx, itemID, completed)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return renderChecklistItem(stdout, item, opts)
}

func runChecklistUpdate(ctx context.Context, stdout, stderr io.Writer, itemID string, attrs map[string]any, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	item, err := c.UpdateChecklistItem(ctx, itemID, attrs)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return renderChecklistItem(stdout, item, opts)
}

// renderChecklistItem wraps a single item in the `checklist_item` envelope
// for json/toon and emits a one-line `[x|] #id title` text fallback that
// matches the shape of the list view's rows.
func renderChecklistItem(out io.Writer, item *client.ChecklistItem, opts *runtimeOpts) error {
	payload := map[string]any{"checklist_item": checklistItemMap(item)}
	return renderPayload(out, payload, checklistItemLine(item), opts)
}

func checklistItemMap(it *client.ChecklistItem) map[string]any {
	return map[string]any{
		"id":         it.ID,
		"title":      it.Title,
		"completed":  it.Completed,
		"position":   it.Position,
		"has_notes":  it.HasNotes,
		"last_actor": actorMap(it.LastActor),
	}
}

func checklistItemLine(it *client.ChecklistItem) string {
	return fmt.Sprintf("%s #%d %s", checkbox(it.Completed), it.ID, it.Title)
}

func runChecklist(ctx context.Context, stdout, stderr io.Writer, taskID string, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	items, err := c.ListChecklistItems(ctx, taskID)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	payload := map[string]any{"checklist_items": checklistMaps(items)}
	return renderPayload(stdout, payload, checklistItemsText(items), opts)
}

func checklistMaps(items []*client.ChecklistItem) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, checklistItemMap(it))
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
