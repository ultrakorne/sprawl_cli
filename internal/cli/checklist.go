package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func newChecklistCmd(opts *runtimeOpts) *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "checklist <task_id>",
		Short: "List or mutate checklist items for a task",
		Long: "With just a task id, lists checklist items (GET /api/v1/tasks/:task_id/checklist). " +
			"Pass --full to include each item's notes inline. " +
			"Use the `add`, `check`, `uncheck`, `update`, `state`, `pr`, and `delete` subcommands " +
			"to mutate items. " +
			"Permission is enforced on the parent task — 403 if the caller can't read/write it, " +
			"404 if it isn't visible to them at all.",
		// The listing behaviour is preserved as the parent RunE so that
		// `sprawl checklist <task_id>` keeps working without an explicit
		// `list` subcommand. cobra routes exact subcommand matches (add /
		// check / uncheck / update) to their own handlers before falling
		// through here.
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChecklist(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], full, opts)
		},
	}
	cmd.Flags().BoolVar(&full, "full", false,
		"include each item's notes inline (GET …/tasks/:task_id/checklist?full=true)")
	cmd.SilenceErrors = true
	cmd.AddCommand(newChecklistAddCmd(opts))
	cmd.AddCommand(newChecklistCheckCmd(opts))
	cmd.AddCommand(newChecklistUncheckCmd(opts))
	cmd.AddCommand(newChecklistUpdateCmd(opts))
	cmd.AddCommand(newChecklistDeleteCmd(opts))
	cmd.AddCommand(newChecklistStateCmd(opts))
	cmd.AddCommand(newChecklistPRCmd(opts))
	return cmd
}

// stateAliases maps the short words the CLI accepts onto the server's three
// state strings. The wire values are accepted verbatim too, so an agent can
// echo back whatever a read gave it.
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

func newChecklistStateCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state <item_id> <ready|progress|review|none>",
		Short: "Set or clear a checklist item's state (PATCH /api/v1/checklist_items/:id/state)",
		Long: "Set the hand-set state on a checklist item: `ready` (ready_to_pickup), " +
			"`progress` (in_progress), `review` (in_review), or `none` to clear it. The wire " +
			"values are accepted verbatim too.\n\n" +
			"States are mutually exclusive — setting one replaces any previous one. Setting a " +
			"state on a COMPLETED item un-completes it, so any item carrying a state is " +
			"incomplete by construction. The PR number is untouched by this command.\n\n" +
			"This is a separate route from `checklist update`, which ignores state entirely.",
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
			return runChecklistState(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(),
				args[0], map[string]any{"state": val}, opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newChecklistPRCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr <item_id> <number|none>",
		Short: "Set or clear a checklist item's PR number (PATCH /api/v1/checklist_items/:id/state)",
		Long: "Attach a GitHub pull-request number to a checklist item, or `none` to clear it.\n\n" +
			"The PR number is fully independent of state and completion: it can be set before any " +
			"review starts (a draft PR), it survives the item being checked off, and only an " +
			"explicit `none` clears it. The link itself is built by the client from the project's " +
			"github_url — items whose project has none render the bare number.",
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
			return runChecklistState(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(),
				args[0], map[string]any{"pr_number": val}, opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runChecklistState(ctx context.Context, stdout, stderr io.Writer, itemID string, attrs map[string]any, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	item, err := c.SetChecklistItemState(ctx, itemID, attrs)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return renderChecklistItem(stdout, item, opts)
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
	return renderPayload(stdout, payload, deletedText("checklist item", itemID, existed, resolveProjectKey(opts)), opts)
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
	// Single-item write responses (add / update / check / uncheck) never carry
	// notes, so this is the non-full path: suppress the notes key.
	payload := map[string]any{"checklist_item": checklistItemMap(item, false)}
	return renderPayload(out, payload, checklistItemLine(item), opts)
}

func checklistItemMap(it *client.ChecklistItem, full bool) map[string]any {
	m := map[string]any{
		"id":         it.ID,
		"title":      it.Title,
		"completed":  it.Completed,
		"position":   it.Position,
		"state":      nilIfEmpty(it.State),
		"pr_number":  nilIfZero(it.PRNumber),
		"has_notes":  it.HasNotes,
		"last_actor": actorMap(it.LastActor),
	}
	// notes rides only on the ?full=true read paths. On non-full lists and
	// single-item write responses the server omits it, so we suppress the key
	// to keep those payloads' shape. On the full path the server always
	// includes notes — null when empty — so we always emit the key, collapsing
	// empty ("" pre-rollout or null post-rollout) to a literal null for a
	// uniform "empty ⇒ null" contract that matches `note show`.
	if full {
		if it.Notes != nil && *it.Notes != "" {
			m["notes"] = *it.Notes
		} else {
			m["notes"] = nil
		}
	}
	return m
}

func checklistItemLine(it *client.ChecklistItem) string {
	box := sty.render(sty.checkboxStyle(it.Completed), checkbox(it.Completed))
	return fmt.Sprintf("%s #%d %s%s", box, it.ID, it.Title, itemTrailer(it, nil))
}

// itemTrailer is the ` · review · #412` suffix appended to a one-line item
// rendering. Empty when the item carries neither state nor PR number, so items
// that predate the feature (and the vast majority that never enter review) look
// exactly as they did. project, when non-nil and carrying a github_url, upgrades
// the bare number to the full PR link.
func itemTrailer(it *client.ChecklistItem, project *client.Project) string {
	var parts []string
	if it.State != "" {
		parts = append(parts, sty.render(stateStyle(it.State), stateLabel(it.State)))
	}
	if it.PRNumber > 0 {
		parts = append(parts, sty.render(sty.faint, prLabel(it.PRNumber, project)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + sty.render(sty.faint, "·") + " " + strings.Join(parts, " "+sty.render(sty.faint, "·")+" ")
}

// stateLabel is the short human word for a state — the same vocabulary the
// `checklist state` command accepts, so what you read is what you type. An
// unknown value (a state added server-side after this build) passes through
// verbatim rather than being hidden.
func stateLabel(state string) string {
	switch state {
	case client.StateReadyToPickup:
		return "ready"
	case client.StateInProgress:
		return "progress"
	case client.StateInReview:
		return "review"
	case "":
		return ""
	default:
		return state
	}
}

// stateStyle colors a state like the rest of the CLI's status vocabulary:
// ready is cyan (available), in-progress yellow (running), in review green
// (work is done, awaiting a human).
func stateStyle(state string) lipgloss.Style {
	switch state {
	case client.StateReadyToPickup:
		return sty.accent
	case client.StateInProgress:
		return sty.warn
	case client.StateInReview:
		return sty.ok
	default:
		return sty.plain
	}
}

// prLabel renders a PR number as the full GitHub URL when the item's project
// resolves one, and as a bare `#412` otherwise. Neither break in the chain (no
// project, or no github_url on it) is an error — the number is still shown.
func prLabel(prNumber int64, project *client.Project) string {
	if u := client.PRURL(project, prNumber); u != "" {
		return u
	}
	return fmt.Sprintf("#%d", prNumber)
}

// nilIfEmpty / nilIfZero map a zero value back to a literal JSON null, which is
// what the server sends for a cleared state / PR number. Keeping the key present
// (rather than omitting it) means a payload always answers "is there a state?"
// without the consumer having to distinguish absent from null.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nilIfZero(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func runChecklist(ctx context.Context, stdout, stderr io.Writer, taskID string, full bool, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	items, err := c.ListChecklistItems(ctx, taskID, full)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	payload := map[string]any{"checklist_items": checklistMaps(items, full)}
	// Full mode swaps the aligned table for a per-item block so multi-line
	// notes stay readable; the json/toon payload is identical either way
	// (checklistItemMap emits the notes key only when present).
	text := checklistItemsText(items)
	if full {
		if len(items) == 0 {
			text = "(no checklist items)"
		} else {
			text = fullChecklistText(items, "")
		}
	}
	return renderPayload(stdout, payload, text, opts)
}

func checklistMaps(items []*client.ChecklistItem, full bool) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, checklistItemMap(it, full))
	}
	return out
}

func checklistItemsText(items []*client.ChecklistItem) string {
	if len(items) == 0 {
		return sty.render(sty.faint, "(no checklist items)")
	}
	rows := make([][]col, len(items))
	for i, it := range items {
		rows[i] = []col{
			plainCol(fmt.Sprintf("%d", it.ID)),
			plainCol(fmt.Sprintf("%d", it.Position)),
			styledCol(checkbox(it.Completed), sty.checkboxStyle(it.Completed)),
			styledCol(fallback(stateLabel(it.State), "-"), stateStyle(it.State)),
			plainCol(prCell(it.PRNumber)),
			plainCol(notesFlag(it.HasNotes)),
			plainCol(it.Title),
		}
	}
	// The listing endpoint carries no project, so PR numbers stay bare here —
	// `queue` and `task <id> --full` are the views that can resolve the link.
	return renderTable([]string{"ID", "POS", " ", "STATE", "PR", "NOTES", "TITLE"}, rows)
}

// prCell is the table-cell form of a PR number: `#412`, or "-" when unset.
func prCell(prNumber int64) string {
	if prNumber <= 0 {
		return "-"
	}
	return fmt.Sprintf("#%d", prNumber)
}

// fullChecklistText renders the ?full=true item view: one line per item
// (`<checkbox> #<id> <title>`) followed by its notes. Notes are indented six
// columns past the item line and printed verbatim — multi-line blobs keep
// their line breaks. Empty notes render `(no notes)`. indent prefixes every
// item line so the same helper serves both `checklist <id> --full` (indent
// "") and the embedded block under `task <id> --full` (indent "  ").
// Callers handle the empty-slice case before calling.
func fullChecklistText(items []*client.ChecklistItem, indent string) string {
	notesIndent := indent + "      "
	var b strings.Builder
	for _, it := range items {
		box := sty.render(sty.checkboxStyle(it.Completed), checkbox(it.Completed))
		fmt.Fprintf(&b, "%s%s #%d %s%s\n", indent, box, it.ID, it.Title, itemTrailer(it, nil))
		notes := ""
		if it.Notes != nil {
			notes = *it.Notes
		}
		if strings.TrimSpace(notes) == "" {
			fmt.Fprintf(&b, "%s%s\n", notesIndent, sty.render(sty.faint, "(no notes)"))
			continue
		}
		lines := strings.Split(notes, "\n")
		fmt.Fprintf(&b, "%s%s %s\n", notesIndent, sty.render(sty.faint, "notes:"), lines[0])
		for _, ln := range lines[1:] {
			fmt.Fprintf(&b, "%s%s\n", notesIndent, ln)
		}
	}
	return strings.TrimRight(b.String(), "\n")
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
