package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func newChecklistCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checklist <task_id>",
		Short: "List or mutate checklist items for a task",
		Long: "With just a task id, lists checklist items (GET /api/v1/tasks/:task_id/checklist). " +
			"Use the `add`, `toggle`, and `update` subcommands to mutate items. Permission is " +
			"enforced on the parent task — 403 if the caller can't read/write it, 404 if it isn't " +
			"visible to them at all.",
		// The listing behaviour is preserved as the parent RunE so that
		// `sprawl checklist <task_id>` keeps working without an explicit
		// `list` subcommand. cobra routes exact subcommand matches (add /
		// toggle / update) to their own handlers before falling through here.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(),
					errors.New("checklist requires exactly one argument: the task id"))
			}
			return runChecklist(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0])
		},
	}
	cmd.SilenceErrors = true
	cmd.AddCommand(newChecklistAddCmd())
	cmd.AddCommand(newChecklistToggleCmd())
	cmd.AddCommand(newChecklistUpdateCmd())
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

func newChecklistAddCmd() *cobra.Command {
	var f checklistItemWriteFlags
	cmd := &cobra.Command{
		Use:   "add <task_id>",
		Short: "Add a checklist item to a task (POST /api/v1/tasks/:task_id/checklist)",
		Long: "Add a checklist item under a task. Provide --title (required server-side) and " +
			"optional --notes, or pipe a JSON object via `--from-json -`. Position is assigned " +
			"by the server (appended at the end).",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(),
					errors.New("checklist add requires exactly one argument: the task id"))
			}
			f.hasNotes = cmd.Flags().Changed("notes")
			attrs, err := f.buildAttrs(cmd.InOrStdin())
			if err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err)
			}
			if err := requireAttrs(attrs, "checklist add"); err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err)
			}
			return runChecklistAdd(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], attrs)
		},
	}
	bindChecklistWriteFlags(cmd, &f)
	cmd.SilenceErrors = true
	return cmd
}

func newChecklistToggleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "toggle <item_id>",
		Short: "Flip a checklist item's completion state (PATCH /api/v1/checklist_items/:id/toggle)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(),
					errors.New("checklist toggle requires exactly one argument: the item id"))
			}
			return runChecklistToggle(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0])
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newChecklistUpdateCmd() *cobra.Command {
	var f checklistItemWriteFlags
	cmd := &cobra.Command{
		Use:   "update <item_id>",
		Short: "Update a checklist item's title/notes (PATCH /api/v1/checklist_items/:id)",
		Long: "Update a checklist item. Accepts --title, --notes, or `--from-json -`. Completion " +
			"state isn't mutable through this endpoint — use `checklist toggle` instead.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(),
					errors.New("checklist update requires exactly one argument: the item id"))
			}
			f.hasNotes = cmd.Flags().Changed("notes")
			attrs, err := f.buildAttrs(cmd.InOrStdin())
			if err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err)
			}
			if err := requireAttrs(attrs, "checklist update"); err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err)
			}
			return runChecklistUpdate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], attrs)
		},
	}
	bindChecklistWriteFlags(cmd, &f)
	cmd.SilenceErrors = true
	return cmd
}

func runChecklistAdd(ctx context.Context, stdout, stderr io.Writer, taskID string, attrs map[string]any) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	item, err := c.CreateChecklistItem(ctx, taskID, attrs)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	return renderChecklistItem(stdout, item)
}

func runChecklistToggle(ctx context.Context, stdout, stderr io.Writer, itemID string) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	item, err := c.ToggleChecklistItem(ctx, itemID)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	return renderChecklistItem(stdout, item)
}

func runChecklistUpdate(ctx context.Context, stdout, stderr io.Writer, itemID string, attrs map[string]any) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	item, err := c.UpdateChecklistItem(ctx, itemID, attrs)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	return renderChecklistItem(stdout, item)
}

// renderChecklistItem wraps a single item in the `checklist_item` envelope
// for json/toon and emits a one-line `[x|] #id title` text fallback that
// matches the shape of the list view's rows.
func renderChecklistItem(out io.Writer, item *client.ChecklistItem) error {
	payload := map[string]any{"checklist_item": checklistItemMap(item)}
	return renderPayload(out, payload, checklistItemLine(item))
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
