package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// newQueueCmd wraps GET /api/v1/checklist_items?state=<state> — the one read
// that crosses tasks. It's top-level rather than a `checklist` subcommand
// because it isn't scoped to a task: it answers "what is in this state
// anywhere I can see?", which is the question an agent asks before picking up
// work.
func newQueueCmd(opts *runtimeOpts) *cobra.Command {
	var state string
	var full bool
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "List checklist items in a given state across every visible task, grouped by task (GET /api/v1/checklist_items?state=)",
		Long: "List checklist items carrying a state, across every task the caller can read, in " +
			"one call. Defaults to `ready_to_pickup` — the agent's \"what can I pick up?\" query.\n\n" +
			"Accepts the short forms `ready` / `progress` / `review` as well as the wire values " +
			"`ready_to_pickup` / `in_progress` / `in_review`. Results are confined by a project " +
			"key like every other read.\n\n" +
			"Every returned item is incomplete: completing an item clears its state, so no " +
			"`completed` filter is needed. Items are grouped under their parent task, which " +
			"carries its title, description, due date and project — so the context for picking " +
			"an item up comes in the same call, and a PR number resolves to a full link without " +
			"a second one.",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			wire, err := parseState(state)
			if err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			// `none` is meaningful when *setting* a state but not when querying —
			// the endpoint requires one of the three, and "items with no state" is
			// just the ordinary checklist.
			if wire == "" {
				err := fmt.Errorf("--state must be one of ready|progress|review (there is no queue of stateless items)")
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			return runQueue(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), wire, full, opts)
		},
	}
	cmd.Flags().StringVar(&state, "state", "ready",
		"state to list: ready|progress|review (the server's own spellings are accepted too)")
	cmd.Flags().BoolVar(&full, "full", false,
		"expand each item's note under its row")
	cmd.SilenceErrors = true
	return cmd
}

func runQueue(ctx context.Context, stdout, stderr io.Writer, state string, full bool, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	groups, err := c.ListChecklistItemsByState(ctx, state, full)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	rows := make([]any, 0, len(groups))
	for _, g := range groups {
		rows = append(rows, queueTaskMap(g, full))
	}
	payload := map[string]any{"tasks": rows}
	return renderPayload(stdout, payload, queueText(groups, state, full), opts)
}

// queueTaskMap is the machine shape of one queue group: the trimmed task the
// server sends (id, title, description, due_date, project) with its items
// rendered through the shared itemMap, so each carries the same short-form
// state and resolved pr_url as every other item view. pr_url resolves through
// the group's project — the reason the project rides along at all.
func queueTaskMap(g *client.QueueTask, full bool) map[string]any {
	items := make([]any, 0, len(g.ChecklistItems))
	for _, it := range g.ChecklistItems {
		items = append(items, itemMap(it, g.Project, full))
	}
	return map[string]any{
		"id":              g.ID,
		"title":           g.Title,
		"description":     g.Description,
		"due_date":        nilIfEmpty(g.DueDate),
		"project":         projectMap(g.Project),
		"checklist_items": items,
	}
}

// queueViews adapts one group's items to the shared renderer. The project is
// the group's own — a queue crosses tasks, so it is per group rather than per
// call.
func queueViews(g *client.QueueTask) []itemView {
	views := make([]itemView, len(g.ChecklistItems))
	for i, it := range g.ChecklistItems {
		views[i] = itemView{item: it, project: g.Project}
	}
	return views
}

// queueGroupHeader introduces one task's block: `<id> <title>` in bold, the
// project and due date faint on the same line, then the description wrapped
// beneath — the same header `task <id>` shows, plus the id, because here the
// reader has not typed it and needs it for the `task <id> --full` that follows.
func queueGroupHeader(g *client.QueueTask) string {
	head := sty.render(sty.bold, fmt.Sprintf("%d %s", g.ID, g.Title))
	var meta []string
	if g.Project != nil {
		meta = append(meta, g.Project.Name)
	}
	if strings.TrimSpace(g.DueDate) != "" {
		meta = append(meta, "due "+g.DueDate)
	}
	if len(meta) > 0 {
		head += sty.render(sty.faint, "  "+strings.Join(meta, " · "))
	}
	lines := []string{head}
	if strings.TrimSpace(g.Description) != "" {
		lines = append(lines, wrapLines(g.Description, outputWidth)...)
	}
	return strings.Join(lines, "\n")
}

// queueText is the human view: a heading naming the state and the item count,
// then one block per task — its header and the same item table every other
// item view renders, minus the checkbox and STATE columns. Both are constant
// within one queue — every queued item is incomplete by construction, and the
// state is the query, so it goes in the heading rather than repeating
// identically down a column. The task and project are constant within a block,
// so they live in its header rather than in columns.
//
// Each block is its own table — header, rule and widths — rather than one table
// interrupted by task headers. Deliberate: a block then reads exactly like
// `task <id>` for that task, which is the command the reader runs next, and a
// block's columns size to its own titles instead of the longest title anywhere
// in the queue. The cost is that columns don't line up across blocks; the
// blank line between them is what keeps that from reading as misalignment.
func queueText(groups []*client.QueueTask, state string, full bool) string {
	total := 0
	for _, g := range groups {
		total += len(g.ChecklistItems)
	}
	if total == 0 {
		return sty.render(sty.faint, fmt.Sprintf("(no items in %s)", stateLabel(state)))
	}
	var b strings.Builder
	b.WriteString(sty.render(sty.bold, stateLabel(state)))
	b.WriteString(sty.render(sty.faint, fmt.Sprintf("  (%d)", total)))
	for _, g := range groups {
		if len(g.ChecklistItems) == 0 {
			continue
		}
		// A blank line before each block; renderTable then opens with its own
		// newline, which is the blank line between the header and the table,
		// matching `task <id>`.
		b.WriteString("\n\n")
		b.WriteString(queueGroupHeader(g))
		b.WriteString("\n")
		b.WriteString(itemTable(queueViews(g), itemCols{}, full))
	}
	return b.String()
}
