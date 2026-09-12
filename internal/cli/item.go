package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// `item` is one of the CLI's two nouns (the other is `task`), plus `queue`. Every
// operation on a single checklist item lives here — reading it, editing its title
// or its note, checking it off, moving its state, attaching a PR, deleting it.
// The `checklist` and `note` command trees it replaced are gone; a note is not a
// separate object, it is a field of an item.

func newItemCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "item <id>",
		Short: "Show an item with its note, or manage items (add, update, check, uncheck, state, pr, delete)",
		Long: "With just an item id, fetches that item and its note " +
			"(GET /api/v1/checklist_items/:id) — the detail view, so the note is always " +
			"expanded and there is no --full. Use the `add`, `update`, `check`, `uncheck`, " +
			"`state`, `pr` and `delete` subcommands to mutate items.\n\n" +
			"The human view shows the item alone, with no parent-task context — an item id " +
			"is what you asked about, so an item is what you get. --format=json " +
			"additionally carries a `task` stub (id, title, project) if you need to get back " +
			"to the task.\n\n" +
			"Permission is inherited from the parent task: 403 when the caller can't read it " +
			"— which is what a project key gets for an item in another project — and 404 when " +
			"the item doesn't exist.",
		// The show behaviour is the parent RunE so `sprawl item <id>` works
		// without an explicit `show` subcommand, matching `task <id>`. cobra
		// routes exact subcommand matches (add / check / …) to their own
		// handlers before falling through here.
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runItemShow(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
		},
	}
	cmd.SilenceErrors = true
	cmd.AddCommand(newItemAddCmd(opts))
	cmd.AddCommand(newItemUpdateCmd(opts))
	cmd.AddCommand(newItemCheckCmd(opts))
	cmd.AddCommand(newItemUncheckCmd(opts))
	cmd.AddCommand(newItemStateCmd(opts))
	cmd.AddCommand(newItemPRCmd(opts))
	cmd.AddCommand(newItemDeleteCmd(opts))
	return cmd
}

// -- state / PR vocabulary --------------------------------------------------

// stateAliases maps the short words the CLI accepts onto the server's three
// state strings. The short words are THE vocabulary — they are what every read
// emits (see stateLabel) — but the wire values are accepted verbatim too, so an
// agent echoing back a raw server payload still works.
var stateAliases = map[string]string{
	"ready":    client.StateReadyToPickup,
	"progress": client.StateInProgress,
	"review":   client.StateInReview,
}

// parseState resolves a state argument to its wire value. `none` (and its
// synonyms) resolve to "" meaning "clear". Unknown values fail locally with the
// accepted set spelled out rather than round-tripping for a bare 422 — the
// aliases are a CLI concept, so the CLI has to own the vocabulary anyway.
func parseState(arg string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(arg))
	switch v {
	case "none", "clear", "null":
		return "", nil
	}
	if wire, ok := stateAliases[v]; ok {
		return wire, nil
	}
	if client.ValidState(v) {
		return v, nil
	}
	return "", fmt.Errorf(
		"invalid state %q (want: ready|progress|review|none, or the wire values %s)",
		arg, strings.Join(client.States, "|"))
}

// parsePRNumber resolves a PR argument to the value to send: a positive integer,
// or nil to clear. The server rejects <= 0 with a 422; we catch it here so the
// message names the rule.
func parsePRNumber(arg string) (*int64, error) {
	v := strings.ToLower(strings.TrimSpace(arg))
	switch v {
	case "none", "clear", "null":
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("invalid pr number %q (want: a positive integer, or `none` to clear)", arg)
	}
	return &n, nil
}

// -- read -------------------------------------------------------------------

func runItemShow(ctx context.Context, stdout, stderr io.Writer, itemID string, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	it, err := c.GetChecklistItem(ctx, itemID)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	// This is the detail read, so notes are always present and always rendered —
	// there is no --full to gate them on.
	payload := map[string]any{"checklist_item": itemDetailMap(it)}
	return renderPayload(stdout, payload, itemDetailText(it), opts)
}

// itemDetailMap is `item <id>`'s json shape: the shared item map with notes
// always on, plus the parent-task stub. The stub is not rendered in the human
// view but it rides here because it is the only thing tying an item id back to
// its task — and its project is what resolved pr_url.
func itemDetailMap(it *client.ItemDetail) map[string]any {
	m := itemMap(&it.ChecklistItem, it.Task.Project, true)
	m["task"] = itemTaskMap(&it.Task)
	return m
}

// itemDetailText is the human view: one row of the shared table with the note
// expanded under it. No task header of any kind — see newItemCmd's Long.
func itemDetailText(it *client.ItemDetail) string {
	cols := itemCols{checkbox: true, state: true}
	view := itemView{item: &it.ChecklistItem, project: it.Task.Project}
	return itemTable([]itemView{view}, cols, true)
}

// -- write ------------------------------------------------------------------

// itemWriteFlags carries the flag state shared by `item add` and `item update`:
// the two writable fields plus a --from-json escape hatch.
type itemWriteFlags struct {
	title    string
	notes    string
	fromJSON string
	hasNotes bool
}

func bindItemWriteFlags(cmd *cobra.Command, f *itemWriteFlags) {
	cmd.Flags().StringVar(&f.title, "title", "", "item title")
	cmd.Flags().StringVar(&f.notes, "notes", "",
		"the item's note; `-` reads the whole body from stdin, and \"\" clears it")
	cmd.Flags().StringVar(&f.fromJSON, "from-json", "",
		"read checklist_item attrs as a JSON object from a file path, or `-` for stdin")
}

// buildAttrs merges --from-json (if present) with the explicit flags; flags win
// on conflict. `--notes -` reads the body from stdin, which is how a multi-line
// or piped note is written now that `note set --stdin` is gone.
func (f *itemWriteFlags) buildAttrs(stdin io.Reader) (map[string]any, error) {
	attrs := map[string]any{}
	if f.fromJSON != "" {
		loaded, err := loadJSONFromSource(f.fromJSON, stdin)
		if err != nil {
			return nil, err
		}
		maps.Copy(attrs, loaded)
	}
	mergeStringFlag(attrs, "title", f.title)
	// cobra's Changed check answers "was the flag passed?", which the mere
	// presence of an empty value can't — and an empty value is meaningful here:
	// `--notes ""` is the documented way to clear a note.
	if f.hasNotes {
		notes := f.notes
		if notes == "-" {
			// --from-json - and --notes - can't both consume stdin; whichever
			// runs second gets nothing. --from-json is read above, so guard.
			if f.fromJSON == "-" {
				return nil, errNotesAndJSONBothStdin
			}
			b, err := io.ReadAll(stdin)
			if err != nil {
				return nil, fmt.Errorf("read notes from stdin: %w", err)
			}
			notes = string(b)
		}
		attrs["notes"] = notes
	}
	return attrs, nil
}

var errNotesAndJSONBothStdin = fmt.Errorf(
	"--notes - and --from-json - both read stdin; pass the note inline or the JSON from a file")

func newItemAddCmd(opts *runtimeOpts) *cobra.Command {
	var f itemWriteFlags
	cmd := &cobra.Command{
		Use:   "add <task_id>",
		Short: "Add an item to a task (POST /api/v1/tasks/:task_id/checklist)",
		Long: "Add a checklist item under a task. Provide --title (required server-side) and " +
			"an optional --notes, or pipe a JSON object via `--from-json -`. Position is " +
			"assigned by the server (appended at the end).\n\n" +
			"Note the argument is a TASK id — this is the one item verb that takes one, " +
			"because the item doesn't exist yet.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.hasNotes = cmd.Flags().Changed("notes")
			attrs, err := f.buildAttrs(cmd.InOrStdin())
			if err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			if err := requireAttrs(attrs, "item add"); err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			return runItemAdd(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], attrs, opts)
		},
	}
	bindItemWriteFlags(cmd, &f)
	cmd.SilenceErrors = true
	return cmd
}

func newItemUpdateCmd(opts *runtimeOpts) *cobra.Command {
	var f itemWriteFlags
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an item's title or note (PATCH /api/v1/checklist_items/:id)",
		Long: "Update a checklist item's title and/or its note. This is the ONLY way to write " +
			"a note — there is no separate note command.\n\n" +
			"  item update 203 --notes \"blocked on the backfill\"   set it\n" +
			"  item update 203 --notes -                           read the body from stdin\n" +
			"  item update 203 --notes \"\"                          clear it\n\n" +
			"Completion isn't mutable here — use `item check` / `item uncheck`. State and PR " +
			"number aren't either — use `item state` / `item pr`. The server requires at least " +
			"one of title / notes and rejects an empty write with 422.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.hasNotes = cmd.Flags().Changed("notes")
			attrs, err := f.buildAttrs(cmd.InOrStdin())
			if err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			if err := requireAttrs(attrs, "item update"); err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			return runItemUpdate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], attrs, opts)
		},
	}
	bindItemWriteFlags(cmd, &f)
	cmd.SilenceErrors = true
	return cmd
}

func newItemCheckCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <id>",
		Short: "Mark an item completed (PATCH /api/v1/checklist_items/:id/completed)",
		Long: "Mark an item completed. Sends `{\"completed\": true}`. The server is " +
			"idempotent — calling this on an already-completed item is a no-op but still " +
			"returns the current item.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runItemSetCompleted(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], true, opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newItemUncheckCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uncheck <id>",
		Short: "Mark an item not completed (PATCH /api/v1/checklist_items/:id/completed)",
		Long: "Mark an item not completed. Sends `{\"completed\": false}`. The server is " +
			"idempotent — calling this on an already-uncompleted item is a no-op but still " +
			"returns the current item.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runItemSetCompleted(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], false, opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newItemStateCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state <id> <ready|progress|review|none>",
		Short: "Set or clear an item's state (PATCH /api/v1/checklist_items/:id/state)",
		Long: "Set the hand-set state on an item: `ready`, `progress`, `review`, or `none` to " +
			"clear it. These short names are the vocabulary — they are also what every read " +
			"emits — though the server's own spellings (ready_to_pickup / in_progress / " +
			"in_review) are accepted verbatim.\n\n" +
			"States are mutually exclusive — setting one replaces any previous one. Setting a " +
			"state on a COMPLETED item un-completes it, so any item carrying a state is " +
			"incomplete by construction. The PR number is untouched by this command.\n\n" +
			"This is a separate route from `item update`, which ignores state entirely.",
		Args: textArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := parseState(args[1])
			if err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			// A cleared state is a literal JSON null; the key must still be
			// present, since an absent key means "leave unchanged".
			var val any
			if state != "" {
				val = state
			}
			return runItemState(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(),
				args[0], map[string]any{"state": val}, itemStateSummary, opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newItemPRCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr <id> <number|none>",
		Short: "Set or clear an item's PR number (PATCH /api/v1/checklist_items/:id/state)",
		Long: "Attach a GitHub pull-request number to an item, or `none` to clear it.\n\n" +
			"The PR number is fully independent of state and completion: it can be set before " +
			"any review starts (a draft PR), it survives the item being checked off, and only " +
			"an explicit `none` clears it. The link itself is built by the client from the " +
			"project's repo URL — items whose project has none render the bare number.",
		Args: textArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			pr, err := parsePRNumber(args[1])
			if err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			var val any
			if pr != nil {
				val = *pr
			}
			return runItemState(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(),
				args[0], map[string]any{"pr_number": val}, itemPRSummary, opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

// newItemDeleteCmd wraps DELETE /api/v1/checklist_items/:id. Hard delete — the
// row goes away and the parent task's completed_at may flip (server recomputes).
// 404 not_found is treated as success so retries are no-ops; see runItemDelete.
func newItemDeleteCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Hard-delete an item (DELETE /api/v1/checklist_items/:id)",
		Long: "Hard-delete an item from its parent task. The server recomputes the parent " +
			"task's completed_at (it may flip to done if this was the last unchecked item, " +
			"or clear if no items remain) and broadcasts checklist_item_deleted on PubSub. " +
			"A 404 (item already gone) is treated as success so retries are no-ops, but the " +
			"payload sets `existed: false` so callers can distinguish a real delete from a " +
			"no-op retry or a typo'd id. An item that exists but is outside a project key's " +
			"project is 403, not 404, and surfaces as an error.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runItemDelete(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runItemAdd(ctx context.Context, stdout, stderr io.Writer, taskID string, attrs map[string]any, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	item, err := c.CreateChecklistItem(ctx, taskID, attrs)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return renderItemWrite(stdout, item, false, itemWriteSummary("added", item), opts)
}

func runItemUpdate(ctx context.Context, stdout, stderr io.Writer, itemID string, attrs map[string]any, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	item, err := c.UpdateChecklistItem(ctx, itemID, attrs)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	// Alone among the write routes, PATCH /checklist_items/:id serialises the
	// item in full — so its response carries the saved note and we can emit it.
	// That is what makes a note write verifiable from its own response.
	return renderItemWrite(stdout, item, true, itemWriteSummary("updated", item), opts)
}

func runItemSetCompleted(ctx context.Context, stdout, stderr io.Writer, itemID string, completed bool, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	item, err := c.SetChecklistItemCompleted(ctx, itemID, completed)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	verb := "checked"
	if !completed {
		verb = "unchecked"
	}
	return renderItemWrite(stdout, item, false, itemWriteSummary(verb, item), opts)
}

// runItemState backs both `item state` and `item pr` — one route, two verbs.
// summary builds the one-line human confirmation from the item the server
// returned, so it reports what was actually saved rather than what was asked for.
func runItemState(
	ctx context.Context, stdout, stderr io.Writer, itemID string,
	attrs map[string]any, summary func(*client.ChecklistItem) string, opts *runtimeOpts,
) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	item, err := c.SetChecklistItemState(ctx, itemID, attrs)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return renderItemWrite(stdout, item, false, summary(item), opts)
}

func runItemDelete(ctx context.Context, stdout, stderr io.Writer, itemID string, opts *runtimeOpts) error {
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
	return renderPayload(stdout, payload, deletedText("item", itemID, existed, resolveProjectKey(opts)), opts)
}

// -- write rendering --------------------------------------------------------

// renderItemWrite is the shared response rendering for every write verb: the
// `checklist_item` envelope for json, and a one-line summary for humans.
// No table and no card — you already know what you changed; what you want back
// is confirmation that it landed.
//
// withNotes tracks whether the route's response actually carried the note, so a
// null never has to mean "cleared or just not sent".
func renderItemWrite(out io.Writer, item *client.ChecklistItem, withNotes bool, summary string, opts *runtimeOpts) error {
	// A write response carries no task, so no project, so no pr_url — the number
	// is there, the link isn't resolvable from this payload alone.
	payload := map[string]any{"checklist_item": itemMap(item, nil, withNotes)}
	return renderPayload(out, payload, summary, opts)
}

// itemWriteSummary is `✓ <verb> item <id>  <title>`, matching the TUI's transient
// vocabulary so the two surfaces say the same things about the same action.
func itemWriteSummary(verb string, it *client.ChecklistItem) string {
	return fmt.Sprintf("%s %s item %d  %s",
		sty.render(sty.ok, "✓"), verb, it.ID, it.Title)
}

// itemStateSummary is `✓ item <id> → review`, or `→ no state` once cleared.
func itemStateSummary(it *client.ChecklistItem) string {
	to := fallback(stateLabel(it.State), "no state")
	return fmt.Sprintf("%s item %d %s %s",
		sty.render(sty.ok, "✓"), it.ID, sty.render(sty.faint, "→"),
		sty.render(stateStyle(it.State), to))
}

// itemPRSummary is `✓ item <id> → PR #412`, or `→ no PR` once cleared.
func itemPRSummary(it *client.ChecklistItem) string {
	to := "no PR"
	if it.PRNumber > 0 {
		to = fmt.Sprintf("PR #%d", it.PRNumber)
	}
	return fmt.Sprintf("%s item %d %s %s",
		sty.render(sty.ok, "✓"), it.ID, sty.render(sty.faint, "→"), to)
}
