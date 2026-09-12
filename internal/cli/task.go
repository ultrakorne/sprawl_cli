package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func newTaskCmd(opts *runtimeOpts) *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "task <id>",
		Short: "Show a task by id, or manage tasks (list, search, create, update, due, delete)",
		Long: "With just a task id, fetches that task and its items (GET /api/v1/tasks/:id) — " +
			"mirrors `item <id>`, so a bare positional id falls through to this show " +
			"behaviour while the list / search / create / update / due / delete subcommands " +
			"dispatch first.\n\n" +
			"The human view is a title/description header and then the item table; project, " +
			"due date and progress live in `task list` and in --format=json. Items always " +
			"come back with a NOTES column saying whether each has a note; --full is what " +
			"pulls the note bodies and expands them under their rows.",
		// The show behaviour is the parent RunE so `sprawl task <id>` works
		// without a `show` subcommand, matching `item <id>`. cobra
		// routes exact subcommand matches (list / search / …) before falling
		// through here.
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskShow(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], full, opts)
		},
	}
	cmd.Flags().BoolVar(&full, "full", false,
		"pull each item's note body and expand it under its row (GET …/tasks/:id?full=true)")
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
		cmd.Flags().StringVar(&f.projectID, "project-id", "",
			"assign the new task to a project id (unnecessary under a project key — the task lands there by default)")
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
			"`--from-json -`. Flags override fields parsed from --from-json.\n\n" +
			"Under a project key (--project-key / $SPRAWL_PROJECT_KEY) the task lands in " +
			"that project automatically — --project-id is only honoured when it names the " +
			"same project, and any other project is rejected with 403 forbidden.",
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
	payload := map[string]any{"task": taskMap(task, false)}
	return renderPayload(stdout, payload, taskWriteSummary("created", task), opts)
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
	payload := map[string]any{"task": taskMap(task, false)}
	return renderPayload(stdout, payload, taskWriteSummary("updated", task), opts)
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
	return renderPayload(stdout, payload, deletedText("task", id, existed, resolveProjectKey(opts), c.Workspace()), opts)
}

// deletedText is the shared text-fallback line for `task delete` and
// `checklist delete`. existed=true ⇒ "Deleted <kind> #<id>"; existed=false
// ⇒ "<Kind> #<id> already gone (no change)" so a typo or retry is visibly
// distinct from a real delete in --format=text output.
//
// Under a project key the 404 is ambiguous in a way it isn't otherwise: the
// server answers 404 for anything outside the confined project, so "already
// gone" would claim a delete that never happened to a task living in another
// project. projectKey (empty when unconfined) switches the wording to say so.
// A workspace selector is ambiguous the same way — ids are per workspace and
// the server never says whether the id or the workspace was the miss — so
// workspace (empty for the default) gets the same treatment.
func deletedText(kind, id string, existed bool, projectKey, workspace string) string {
	if existed {
		return sty.render(sty.ok, fmt.Sprintf("Deleted %s #%s", kind, id))
	}
	if projectKey != "" {
		return sty.render(sty.faint, fmt.Sprintf(
			"No %s #%s in project %q (nothing deleted — it may exist outside this project key)",
			kind, id, projectKey))
	}
	if workspace != "" {
		return sty.render(sty.faint, fmt.Sprintf(
			"No %s #%s in workspace %s (nothing deleted — it may exist in another workspace, or the workspace isn't one you can reach)",
			kind, id, workspace))
	}
	// Capitalize the first byte of `kind` for sentence start. All current
	// callers pass ASCII ("task", "checklist item") so byte-level upper is
	// safe and avoids pulling strings.Title (deprecated) or a unicode pkg.
	upper := kind
	if len(upper) > 0 && upper[0] >= 'a' && upper[0] <= 'z' {
		upper = string(upper[0]-32) + upper[1:]
	}
	return sty.render(sty.faint, fmt.Sprintf("%s #%s already gone (no change)", upper, id))
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
	payload := map[string]any{"task": taskMap(task, false)}
	return renderPayload(stdout, payload, taskDueSummary(task), opts)
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
		items = append(items, taskMap(t, false))
	}
	payload := map[string]any{"tasks": items}
	return renderPayload(stdout, payload, taskSearchText(tasks), opts)
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
	payload := map[string]any{"task": taskMap(task, full)}
	return renderPayload(stdout, payload, taskShowText(task, full), opts)
}

func renderTaskList(out io.Writer, tasks []*client.Task, opts *runtimeOpts) error {
	items := make([]any, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, taskMap(t, false))
	}
	payload := map[string]any{"tasks": items}
	return renderPayload(out, payload, taskListText(tasks), opts)
}

// taskMap mirrors the server's task_json shape but as a `map[string]any` so
// renderPayload can hand it to the json encoder. Pointer
// fields (project, created_by, last_actor) become literal `nil` map values,
// which the encoder emits as JSON null.
//
// The envelope keeps EVERY task field — title, description, status, due_date,
// project, checklist_progress, created_by, last_actor. `task <id> -h` shows only
// title + description, so this is the only place the rest is reachable.
//
// withNotes says whether the embedded items' note bodies were fetched, and is
// simply the caller's --full. It is a parameter rather than something inferred
// from the payload because a null note is indistinguishable from an unfetched
// one, and guessing wrong would report "no note" for a note that exists.
func taskMap(t *client.Task, withNotes bool) map[string]any {
	m := map[string]any{
		"id":          t.ID,
		"title":       t.Title,
		"description": t.Description,
		"status":      t.Status,
		"due_date":    nilIfEmpty(t.DueDate),
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
			// Exactly the two fields the server sent. Running these through
			// itemMap would emit completed / state / pr_url for values search
			// never returned, which reads as data rather than as absence.
			matched = append(matched, map[string]any{
				"id":    it.ID,
				"title": it.Title,
			})
		}
		m["matched_checklist_items"] = matched
	}
	// checklist_items rides only on the task READ. nil ⇒ field absent on the wire
	// ⇒ suppress it so list / search / create / update payloads keep their shape.
	// Non-nil (including empty) ⇒ embed each item via the shared itemMap, which
	// resolves pr_url from the task's own project — the one read path where the
	// whole chain is in hand.
	if t.ChecklistItems != nil {
		items := make([]any, 0, len(t.ChecklistItems))
		for _, it := range t.ChecklistItems {
			items = append(items, itemMap(it, t.Project, withNotes))
		}
		m["checklist_items"] = items
	}
	return m
}

// projectMap mirrors the nested project shape. `key` and `github_url` are
// additive: both are always emitted, with github_url as a literal null when the
// project has no repo (which is also what a pre-rollout server's absent field
// looks like). github_url is what turns an item's pr_number into a link.
func projectMap(p *client.Project) any {
	if p == nil {
		return nil
	}
	return map[string]any{
		"id":         p.ID,
		"name":       p.Name,
		"key":        p.Key,
		"color":      p.Color,
		"github_url": nilIfEmpty(p.GithubURL),
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
// json; this view is for humans, so tabwriter alignment is worth the
// stdlib cost.
func taskListText(tasks []*client.Task) string {
	if len(tasks) == 0 {
		return sty.render(sty.faint, "(no tasks)")
	}
	return renderTable(taskListHeader, taskRows(tasks))
}

// STATUS is intentionally absent from the human list view: the PROGRESS column
// (done/total, green once complete) already conveys it. Status still rides in
// the json payload (taskMap) and the single-task detail view for agents
// and full reads.
var taskListHeader = []string{"ID", "DUE", "PROGRESS", "PROJECT", "TITLE"}

func taskRows(tasks []*client.Task) [][]col {
	rows := make([][]col, len(tasks))
	for i, t := range tasks {
		rows[i] = taskRowFields(t)
	}
	return rows
}

// taskRowFields returns one row of columns matching taskListHeader. Shared with
// the search and activity renderers so every task table is built from the same
// source.
func taskRowFields(t *client.Task) []col {
	return []col{
		plainCol(fmt.Sprintf("%d", t.ID)),
		plainCol(fallback(t.DueDate, "-")),
		progressCol(t.ChecklistProgress),
		plainCol(projectLabel(t.Project)),
		plainCol(t.Title),
	}
}

// progressCol renders done/total, traffic-light colored (see progressStyle) so
// completion state reads at a glance — this is the list's status signal.
func progressCol(p client.ChecklistProgress) col {
	text := fmt.Sprintf("%d/%d", p.Done, p.Total)
	return styledCol(text, sty.progressStyle(p.Done, p.Total))
}

// taskSearchText renders one block per matching task: the task's title and
// description, then the ids and titles of the items that matched under it.
// Several tasks stack as several blocks, so it stays readable when a query hits
// across the board.
//
// It shows id and title and nothing else because that is all /tasks/search
// returns — search is a LOOKUP surface. It tells you which tasks and items
// mention the query; `item <id>` and `task <id>` are how you then read them.
// Rendering the full item table here would mean printing an unchecked box and
// an empty state for every hit regardless of its real state, since the search
// payload carries neither.
//
// A task that matched on its own title has no matching items (the server sends
// `[]`) and renders as the header alone — the title already said why it's here.
func taskSearchText(tasks []*client.Task) string {
	if len(tasks) == 0 {
		return sty.render(sty.faint, "(no tasks)")
	}
	blocks := make([]string, 0, len(tasks))
	for _, t := range tasks {
		b := taskHeader(t)
		if len(t.MatchedChecklistItems) > 0 {
			rows := make([][]col, len(t.MatchedChecklistItems))
			for i, it := range t.MatchedChecklistItems {
				rows[i] = []col{
					plainCol(fmt.Sprintf("%d", it.ID)),
					plainCol(it.Title),
				}
			}
			b += "\n" + renderTable([]string{"ID", "TITLE"}, rows)
		}
		blocks = append(blocks, b)
	}
	return strings.Join(blocks, "\n\n")
}

// taskShowText is the human view for `task <id>`: a title + description header,
// then the shared item table. The bordered card is gone — no box, no meta grid,
// no project / due / progress. `task list` already carries all three per task
// and --format=json carries them here, so repeating them above a table of the
// task's actual work was noise.
//
// full expands each item's note under its row; without it the NOTES column is
// still there, answering "is there a note to go and read?".
func taskShowText(t *client.Task, full bool) string {
	head := taskHeader(t)
	if len(t.ChecklistItems) == 0 {
		return head + "\n" + sty.render(sty.faint, "(no items)")
	}
	views := make([]itemView, len(t.ChecklistItems))
	for i, it := range t.ChecklistItems {
		// The task carries its project, so a PR number resolves to a full link.
		views[i] = itemView{item: it, project: t.Project}
	}
	// renderTable opens with one newline; the extra one is the blank line that
	// separates the header block from the table.
	return head + "\n" + itemTable(views, itemCols{checkbox: true, state: true}, full)
}

// taskWriteSummary is the one-line confirmation for create / update: `✓ created
// task 224  New thing`. Matches the TUI's transient vocabulary.
func taskWriteSummary(verb string, t *client.Task) string {
	return fmt.Sprintf("%s %s task %d  %s",
		sty.render(sty.ok, "✓"), verb, t.ID, t.Title)
}

// taskDueSummary is `✓ task 119 due 2026-08-09`, or `due cleared` when the
// preset was `none`. Built from the task the server returned, so it reports the
// date the server actually resolved rather than the preset that was asked for.
func taskDueSummary(t *client.Task) string {
	if strings.TrimSpace(t.DueDate) == "" {
		return fmt.Sprintf("%s task %d %s",
			sty.render(sty.ok, "✓"), t.ID, sty.render(sty.faint, "due cleared"))
	}
	return fmt.Sprintf("%s task %d due %s", sty.render(sty.ok, "✓"), t.ID, t.DueDate)
}

func projectLabel(p *client.Project) string {
	if p == nil {
		return "-"
	}
	return p.Name
}

func fallback(s, zero string) string {
	if strings.TrimSpace(s) == "" {
		return zero
	}
	return s
}
