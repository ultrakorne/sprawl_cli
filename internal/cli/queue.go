package cli

import (
	"context"
	"fmt"
	"io"

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
		Short: "List checklist items in a given state across every visible task (GET /api/v1/checklist_items?state=)",
		Long: "List checklist items carrying a state, across every task the caller can read, in " +
			"one call. Defaults to `ready_to_pickup` — the agent's \"what can I pick up?\" query.\n\n" +
			"Accepts the short forms `ready` / `progress` / `review` as well as the wire values " +
			"`ready_to_pickup` / `in_progress` / `in_review`. Results are confined by a project " +
			"key like every other read.\n\n" +
			"Every returned item is incomplete: completing an item clears its state, so no " +
			"`completed` filter is needed. Each item carries its parent task and that task's " +
			"project, so a PR number resolves to a full link without a second call.",
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
	items, err := c.ListChecklistItemsByState(ctx, state, full)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	rows := make([]any, 0, len(items))
	for _, it := range items {
		rows = append(rows, queueItemMap(it, full))
	}
	payload := map[string]any{"checklist_items": rows}
	return renderPayload(stdout, payload, queueText(items, state, full), opts)
}

// queueItemMap is the shared item shape plus the parent-task stub the endpoint
// embeds. pr_url is resolved through the stub's project — the reason the project
// rides along at all.
func queueItemMap(it *client.ItemDetail, full bool) map[string]any {
	m := itemMap(&it.ChecklistItem, it.Task.Project, full)
	m["task"] = itemTaskMap(&it.Task)
	return m
}

// queueViews adapts queue elements to the shared renderer. Each item's project
// comes from its own parent task — unlike `task <id>`, a queue crosses tasks, so
// the project is per row.
func queueViews(items []*client.ItemDetail) []itemView {
	views := make([]itemView, len(items))
	for i, it := range items {
		views[i] = itemView{item: &it.ChecklistItem, project: it.Task.Project, task: &it.Task}
	}
	return views
}

// queueText is the human view: the same table every other item view renders,
// minus the checkbox and STATE columns. Both are constant within one queue —
// every queued item is incomplete by construction, and the state is the query,
// so it goes in the heading rather than repeating identically down a column.
// TASK and PROJECT take their place, because this is the one item view that
// crosses tasks and an id alone wouldn't say whose work it is.
func queueText(items []*client.ItemDetail, state string, full bool) string {
	if len(items) == 0 {
		return sty.render(sty.faint, fmt.Sprintf("(no items in %s)", stateLabel(state)))
	}
	head := sty.render(sty.bold, stateLabel(state)) + sty.render(sty.faint, fmt.Sprintf("  (%d)", len(items)))
	// renderTable opens with one newline; the extra one is the blank line that
	// separates the heading from the table, matching `task <id>`.
	return head + "\n" + itemTable(queueViews(items), itemCols{task: true, project: true}, full)
}
