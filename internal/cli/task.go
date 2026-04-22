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
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Read or mutate tasks (list, show, search, create, update)",
	}
	cmd.AddCommand(newTaskListCmd(opts))
	cmd.AddCommand(newTaskShowCmd(opts))
	cmd.AddCommand(newTaskSearchCmd(opts))
	cmd.AddCommand(newTaskCreateCmd(opts))
	cmd.AddCommand(newTaskUpdateCmd(opts))
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

func newTaskShowCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Fetch a single task by id (GET /api/v1/tasks/:id)",
		Args:  textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskShow(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
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

func newTaskSearchCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search tasks by title substring (GET /api/v1/tasks/search?q=…)",
		Long: "Case-insensitive substring match on task title. Empty or whitespace-only queries are rejected " +
			"by the server with 422 query_required.",
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
	return renderTaskList(stdout, tasks, opts)
}

func runTaskShow(ctx context.Context, stdout, stderr io.Writer, id string, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	task, err := c.GetTask(ctx, id)
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
	return map[string]any{
		"type": a.Type,
		"id":   a.ID,
	}
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
	fmt.Fprintln(tw, "ID\tSTATUS\tDUE\tPROGRESS\tPROJECT\tTITLE")
	for _, t := range tasks {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%d/%d\t%s\t%s\n",
			t.ID,
			fallback(t.Status, "-"),
			fallback(t.DueDate, "-"),
			t.ChecklistProgress.Done, t.ChecklistProgress.Total,
			projectLabel(t.Project),
			t.Title,
		)
	}
	_ = tw.Flush()
	return strings.TrimRight(buf.String(), "\n")
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
	return strings.TrimRight(b.String(), "\n")
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
