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

func newTaskCmd(opts *runtimeOpts) *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "task <id>",
		Short: "Show a task by id, or manage tasks (list, search, create, update, due, delete)",
		Long: "With just a task id, fetches that task (GET /api/v1/tasks/:id) — mirrors " +
			"`checklist <task_id>`, so a bare positional id falls through to this show " +
			"behaviour while the list / search / create / update / due / delete subcommands " +
			"dispatch first. Pass --full to embed the task's checklist items and their notes " +
			"in one call.",
		// The show behaviour is the parent RunE so `sprawl task <id>` works
		// without a `show` subcommand, matching `checklist <task_id>`. cobra
		// routes exact subcommand matches (list / search / …) before falling
		// through here.
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskShow(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], full, opts)
		},
	}
	cmd.Flags().BoolVar(&full, "full", false,
		"embed the task's checklist items and their notes inline (GET …/tasks/:id?full=true)")
	cmd.SilenceErrors = true
	cmd.AddCommand(newTaskListCmd(opts))
	cmd.AddCommand(newTaskSearchCmd(opts))
	cmd.AddCommand(newTaskCreateCmd(opts))
	cmd.AddCommand(newTaskUpdateCmd(opts))
	cmd.AddCommand(newTaskDueCmd(opts))
	cmd.AddCommand(newTaskDeleteCmd(opts))
	return cmd
}

func newTaskListCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks visible to the current agent (GET /api/v1/tasks)",
		Args:  textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskList(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

// taskWriteFlags bundles the flag state shared by `task create` and
// `task update`. Each subcommand binds its own copy so flag parsing between
// invocations stays clean.
type taskWriteFlags struct {
	title       string
	description string
	projectID   string
	fromJSON    string
	hasDesc     bool
}

func bindTaskWriteFlags(cmd *cobra.Command, f *taskWriteFlags, forUpdate bool) {
	cmd.Flags().StringVar(&f.title, "title", "", "task title")
	cmd.Flags().StringVar(&f.description, "description", "", "task description")
	cmd.Flags().StringVar(&f.fromJSON, "from-json", "",
		"read task attrs as a JSON object from a file path, or `-` for stdin")
	if !forUpdate {
		// `project_id` is assignable at create time only — the server's update
		// path runs through the plain task changeset, which ignores the key.
		cmd.Flags().StringVar(&f.projectID, "project-id", "", "assign the new task to a project id")
	}
}

// buildTaskAttrs merges --from-json (if present) with the explicit flags.
// Flags win on conflict so `--title foo` always lands in the wire payload
// regardless of what stdin said.
func (f *taskWriteFlags) buildAttrs(stdin io.Reader) (map[string]any, error) {
	attrs := map[string]any{}
	if f.fromJSON != "" {
		loaded, err := loadJSONFromSource(f.fromJSON, stdin)
		if err != nil {
			return nil, err
		}
		maps.Copy(attrs, loaded)
	}
	mergeStringFlag(attrs, "title", f.title)
	// Description is allowed to be set to empty explicitly. cobra's Changed
	// check handles the "was the flag passed?" question that the mere
	// presence of a zero value can't answer.
	if f.hasDesc {
		attrs["description"] = f.description
	}
	if err := mergeProjectID(attrs, f.projectID); err != nil {
		return nil, err
	}
	return attrs, nil
}

func newTaskCreateCmd(opts *runtimeOpts) *cobra.Command {
	var f taskWriteFlags
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new task (POST /api/v1/tasks)",
		Long: "Create a task. Provide --title (required server-side) and optional " +
			"--description / --project-id, or pipe the full attrs object as JSON via " +
			"`--from-json -`. Flags override fields parsed from --from-json.",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.hasDesc = cmd.Flags().Changed("description")
			attrs, err := f.buildAttrs(cmd.InOrStdin())
			if err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			if err := requireAttrs(attrs, "task create"); err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			return runTaskCreate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), attrs, opts)
		},
	}
	bindTaskWriteFlags(cmd, &f, false)
	cmd.SilenceErrors = true
	return cmd
}

func newTaskUpdateCmd(opts *runtimeOpts) *cobra.Command {
	var f taskWriteFlags
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an existing task (PATCH /api/v1/tasks/:id)",
		Long: "Update a task's title / description. Accepts the same --title, --description, " +
			"and --from-json flags as `task create`. The server's update changeset ignores " +
			"project_id and other fields.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.hasDesc = cmd.Flags().Changed("description")
			attrs, err := f.buildAttrs(cmd.InOrStdin())
			if err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			if err := requireAttrs(attrs, "task update"); err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			return runTaskUpdate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], attrs, opts)
		},
	}
	bindTaskWriteFlags(cmd, &f, true)
	cmd.SilenceErrors = true
	return cmd
}

func runTaskCreate(ctx context.Context, stdout, stderr io.Writer, attrs map[string]any, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	task, err := c.CreateTask(ctx, attrs)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	payload := map[string]any{"task": taskMap(task)}
	return renderPayload(stdout, payload, taskDetailText(task), opts)
}

func runTaskUpdate(ctx context.Context, stdout, stderr io.Writer, id string, attrs map[string]any, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	task, err := c.UpdateTask(ctx, id, attrs)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	payload := map[string]any{"task": taskMap(task)}
	return renderPayload(stdout, payload, taskDetailText(task), opts)
}

// newTaskDueCmd wraps PATCH /api/v1/tasks/:id/due_date. The endpoint takes
// one of four preset names ("yesterday" / "today" / "week") or null to
// clear; the server resolves the preset against the user's timezone and
// week_end_day setting and returns the same task envelope as `task <id>`.
// The verb is separate from `task update` because the update changeset
// ignores the due_date key — bundling them would mislead.
func newTaskDueCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "due <id> <preset>",
		Short: "Set or clear a task's due date (PATCH /api/v1/tasks/:id/due_date)",
		Long: "Preset is one of: yesterday | today | week | none. " +
			"`none` clears the due date. The server resolves the preset against " +
			"the user's timezone and week_end_day setting; the response carries " +
			"the resolved ISO date (or null) in `due_date`.",
		Args: textArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskDue(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(),
				args[0], args[1], opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

// newTaskDeleteCmd wraps DELETE /api/v1/tasks/:id. The server soft-deletes
// the task (row stays with hidden=true and deleted_at stamped) and reflows
// neighboring cards atomically. We treat a 404 not_found as success so a
// repeated delete is a no-op — see runTaskDelete.
func newTaskDeleteCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Soft-delete a task (DELETE /api/v1/tasks/:id)",
		Long: "Soft-delete a task. The server reflows neighbor cards on the " +
			"canvas and broadcasts task_deleted on PubSub. There is currently no " +
			"API to restore a soft-deleted task — only the LiveView trash bin. " +
			"A 404 from the server (task already deleted or never visible) is " +
			"treated as success so repeated deletes are no-ops, but the payload " +
			"sets `existed: false` so callers can distinguish a real delete " +
			"from a no-op retry / typo'd id.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskDelete(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runTaskDelete(ctx context.Context, stdout, stderr io.Writer, id string, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	existed := true
	if err := c.DeleteTask(ctx, id); err != nil {
		if !isNotFoundAPIError(err) {
			return reportErr(stdout, stderr, err, opts)
		}
		existed = false
	}
	payload := map[string]any{"id": id, "deleted": true, "existed": existed}
	return renderPayload(stdout, payload, deletedText("task", id, existed), opts)
}

// deletedText is the shared text-fallback line for `task delete` and
// `checklist delete`. existed=true ⇒ "Deleted <kind> #<id>"; existed=false
// ⇒ "<Kind> #<id> already gone (no change)" so a typo or retry is visibly
// distinct from a real delete in --format=text output.
func deletedText(kind, id string, existed bool) string {
	if existed {
		return fmt.Sprintf("Deleted %s #%s", kind, id)
	}
	// Capitalize the first byte of `kind` for sentence start. All current
	// callers pass ASCII ("task", "checklist item") so byte-level upper is
	// safe and avoids pulling strings.Title (deprecated) or a unicode pkg.
	upper := kind
	if len(upper) > 0 && upper[0] >= 'a' && upper[0] <= 'z' {
		upper = string(upper[0]-32) + upper[1:]
	}
	return fmt.Sprintf("%s #%s already gone (no change)", upper, id)
}

func runTaskDue(ctx context.Context, stdout, stderr io.Writer, id, preset string, opts *runtimeOpts) error {
	// Local validation matches the --project-id pattern: bounded user input
	// gets a clean message instead of a server 422 invalid_due round-trip.
	var due *string
	switch preset {
	case "yesterday", "today", "week":
		v := preset
		due = &v
	case "none":
		due = nil
	default:
		return reportErr(stdout, stderr,
			fmt.Errorf("preset must be one of: yesterday|today|week|none (got %q)", preset),
			opts)
	}
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	task, err := c.SetTaskDueDate(ctx, id, due)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	payload := map[string]any{"task": taskMap(task)}
	return renderPayload(stdout, payload, taskDetailText(task), opts)
}

func newTaskSearchCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search tasks by title or checklist item title (GET /api/v1/tasks/search?q=…)",
		Long: "Case-insensitive substring match on task title AND checklist item titles (notes are not searched). " +
			"Each task in the response carries a `matched_checklist_items` array — empty when only the title matched, " +
			"otherwise one {id,title} entry per matched checklist item. A task appears at most once even when both " +
			"the title and one or more items match. Empty or whitespace-only queries are rejected by the server " +
			"with 422 query_required.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskSearch(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runTaskList(ctx context.Context, stdout, stderr io.Writer, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	tasks, err := c.ListTasks(ctx)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return renderTaskList(stdout, tasks, opts)
}

func runTaskSearch(ctx context.Context, stdout, stderr io.Writer, query string, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	tasks, err := c.SearchTasks(ctx, query)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	items := make([]any, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, taskMap(t))
	}
	payload := map[string]any{"tasks": items}
	return renderPayload(stdout, payload, taskSearchListText(tasks), opts)
}

func runTaskShow(ctx context.Context, stdout, stderr io.Writer, id string, full bool, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	task, err := c.GetTask(ctx, id, full)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	payload := map[string]any{"task": taskMap(task)}
	return renderPayload(stdout, payload, taskDetailText(task), opts)
}

func renderTaskList(out io.Writer, tasks []*client.Task, opts *runtimeOpts) error {
	items := make([]any, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, taskMap(t))
	}
	payload := map[string]any{"tasks": items}
	return renderPayload(out, payload, taskListText(tasks), opts)
}

// taskMap mirrors the server's task_json shape but as a `map[string]any` so
// renderPayload can hand it to either the json encoder or gotoon. Pointer
// fields (project, created_by, last_actor) become literal `nil` map values,
// which both encoders emit as JSON null / TOON null.
func taskMap(t *client.Task) map[string]any {
	m := map[string]any{
		"id":          t.ID,
		"title":       t.Title,
		"description": t.Description,
		"status":      t.Status,
		"due_date":    t.DueDate,
		"checklist_progress": map[string]any{
			"done":  t.ChecklistProgress.Done,
			"total": t.ChecklistProgress.Total,
		},
		"project":    projectMap(t.Project),
		"created_by": actorMap(t.CreatedBy),
		"last_actor": actorMap(t.LastActor),
	}
	// matched_checklist_items rides only on /search responses. nil ⇒ field
	// absent on the wire ⇒ suppress emission so list/show/create/update
	// payloads keep their existing shape. Non-nil (including empty) ⇒ pass
	// through verbatim so callers can distinguish "matched on title"
	// (`[]`) from "matched on items".
	if t.MatchedChecklistItems != nil {
		matched := make([]any, 0, len(t.MatchedChecklistItems))
		for _, it := range t.MatchedChecklistItems {
			matched = append(matched, map[string]any{
				"id":    it.ID,
				"title": it.Title,
			})
		}
		m["matched_checklist_items"] = matched
	}
	// checklist_items rides only on ?full=true task responses. nil ⇒ field
	// absent ⇒ suppress so list/show/create/update payloads keep their shape.
	// Non-nil (including empty) ⇒ embed each item via checklistItemMap, which
	// carries the inline notes blob on the full path.
	if t.ChecklistItems != nil {
		items := make([]any, 0, len(t.ChecklistItems))
		for _, it := range t.ChecklistItems {
			items = append(items, checklistItemMap(it))
		}
		m["checklist_items"] = items
	}
	return m
}

func projectMap(p *client.Project) any {
	if p == nil {
		return nil
	}
	return map[string]any{
		"id":    p.ID,
		"name":  p.Name,
		"color": p.Color,
	}
}

func actorMap(a *client.Actor) any {
	if a == nil {
		return nil
	}
	m := map[string]any{
		"type": a.Type,
		"id":   a.ID,
	}
	if a.Emoji != "" {
		m["emoji"] = a.Emoji
	}
	return m
}

// taskListText is the `--format=text` fallback for list/search. Agents read
// json/toon; this view is for humans, so tabwriter alignment is worth the
// stdlib cost.
func taskListText(tasks []*client.Task) string {
	if len(tasks) == 0 {
		return "(no tasks)"
	}
	var buf strings.Builder
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, taskListHeader)
	for _, t := range tasks {
		fmt.Fprintln(tw, taskRowFields(t))
	}
	_ = tw.Flush()
	return strings.TrimRight(buf.String(), "\n")
}

const taskListHeader = "ID\tSTATUS\tDUE\tPROGRESS\tPROJECT\tTITLE"

// taskRowFields returns one tab-separated row matching taskListHeader.
// Shared with the search renderer so both views build their tables from
// the same source — no cross-function line-layout coupling.
func taskRowFields(t *client.Task) string {
	return fmt.Sprintf("%d\t%s\t%s\t%d/%d\t%s\t%s",
		t.ID,
		fallback(t.Status, "-"),
		fallback(t.DueDate, "-"),
		t.ChecklistProgress.Done, t.ChecklistProgress.Total,
		projectLabel(t.Project),
		t.Title,
	)
}

// taskSearchListText renders the list view and interleaves a
// `matched checklist:` block under any task whose checklist items hit
// the query — one item per line so commas, long titles, or odd
// whitespace in titles can't garble the output. Title-only matches
// (MatchedChecklistItems == []) get no extra line — the title speaks
// for itself.
func taskSearchListText(tasks []*client.Task) string {
	if len(tasks) == 0 {
		return "(no tasks)"
	}
	// Render the aligned table to a buffer, then split header + rows
	// (Fprintln writes exactly one newline per call, so the line count
	// is guaranteed to be 1 + len(tasks) — no layout assumption).
	var table strings.Builder
	tw := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, taskListHeader)
	for _, t := range tasks {
		fmt.Fprintln(tw, taskRowFields(t))
	}
	_ = tw.Flush()
	lines := strings.Split(strings.TrimRight(table.String(), "\n"), "\n")

	var b strings.Builder
	b.WriteString(lines[0])
	b.WriteByte('\n')
	for i, t := range tasks {
		b.WriteString(lines[1+i])
		b.WriteByte('\n')
		if len(t.MatchedChecklistItems) == 0 {
			continue
		}
		b.WriteString("      matched checklist:\n")
		for _, it := range t.MatchedChecklistItems {
			fmt.Fprintf(&b, "        - %s\n", it.Title)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func taskDetailText(t *client.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n", t.ID, t.Title)
	fmt.Fprintf(&b, "  status:   %s\n", fallback(t.Status, "-"))
	fmt.Fprintf(&b, "  due:      %s\n", fallback(t.DueDate, "-"))
	fmt.Fprintf(&b, "  project:  %s\n", projectLabel(t.Project))
	fmt.Fprintf(&b, "  progress: %d / %d\n", t.ChecklistProgress.Done, t.ChecklistProgress.Total)
	fmt.Fprintf(&b, "  last_actor: %s\n", actorLabel(t.LastActor))
	fmt.Fprintf(&b, "  created_by: %s\n", actorLabel(t.CreatedBy))
	if strings.TrimSpace(t.Description) != "" {
		fmt.Fprintf(&b, "\n%s", t.Description)
	}
	out := strings.TrimRight(b.String(), "\n")
	// On the ?full=true path the task carries its checklist inline. Render it
	// as an indented block under the detail so notes (possibly multi-line)
	// stay readable — see fullChecklistText.
	if t.ChecklistItems != nil {
		out += "\n\nchecklist:\n"
		if len(t.ChecklistItems) == 0 {
			out += "  (no checklist items)"
		} else {
			out += fullChecklistText(t.ChecklistItems, "  ")
		}
	}
	return out
}

func projectLabel(p *client.Project) string {
	if p == nil {
		return "-"
	}
	return p.Name
}

func actorLabel(a *client.Actor) string {
	if a == nil {
		return "-"
	}
	return fmt.Sprintf("%s#%d", a.Type, a.ID)
}

func fallback(s, zero string) string {
	if strings.TrimSpace(s) == "" {
		return zero
	}
	return s
}
