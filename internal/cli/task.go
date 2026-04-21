package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Read tasks (list, show, search)",
	}
	cmd.AddCommand(newTaskListCmd())
	cmd.AddCommand(newTaskShowCmd())
	cmd.AddCommand(newTaskSearchCmd())
	return cmd
}

func newTaskListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks visible to the current agent (GET /api/v1/tasks)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskList(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newTaskShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Fetch a single task by id (GET /api/v1/tasks/:id)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(),
					errors.New("task show requires exactly one argument: the task id"))
			}
			return runTaskShow(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0])
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func newTaskSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search tasks by title substring (GET /api/v1/tasks/search?q=…)",
		Long: "Case-insensitive substring match on task title. Empty or whitespace-only queries are rejected " +
			"by the server with 422 query_required.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(),
					errors.New("task search requires exactly one argument: the query string"))
			}
			return runTaskSearch(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0])
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runTaskList(ctx context.Context, stdout, stderr io.Writer) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	tasks, err := c.ListTasks(ctx)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	return renderTaskList(stdout, tasks)
}

func runTaskSearch(ctx context.Context, stdout, stderr io.Writer, query string) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	tasks, err := c.SearchTasks(ctx, query)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	return renderTaskList(stdout, tasks)
}

func runTaskShow(ctx context.Context, stdout, stderr io.Writer, id string) error {
	c, err := newAuthedClient()
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	task, err := c.GetTask(ctx, id)
	if err != nil {
		return reportErr(stdout, stderr, err)
	}
	payload := map[string]any{"task": taskMap(task)}
	return renderPayload(stdout, payload, taskDetailText(task))
}

func renderTaskList(out io.Writer, tasks []*client.Task) error {
	items := make([]any, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, taskMap(t))
	}
	payload := map[string]any{"tasks": items}
	return renderPayload(out, payload, taskListText(tasks))
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
