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
			return runQueue(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), wire, opts)
		},
	}
	cmd.Flags().StringVar(&state, "state", client.StateReadyToPickup,
		"state to list: ready|progress|review (or the wire values ready_to_pickup|in_progress|in_review)")
	cmd.SilenceErrors = true
	return cmd
}

func runQueue(ctx context.Context, stdout, stderr io.Writer, state string, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	items, err := c.ListChecklistItemsByState(ctx, state)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	rows := make([]any, 0, len(items))
	for _, it := range items {
		rows = append(rows, queueItemMap(it))
	}
	payload := map[string]any{"checklist_items": rows}
	return renderPayload(stdout, payload, queueText(items, state), opts)
}

// queueItemMap is the item shape plus the parent-task stub the endpoint embeds.
// It adds one key the server doesn't send: `pr_url`, the resolved GitHub link
// (null when the item has no PR number, the task has no project, or the project
// has no github_url). Building it here is the whole point of the project's
// github_url riding along — every consumer would otherwise concatenate it by
// hand, and the "render it unlinked" rule is easy to get wrong.
func queueItemMap(it *client.QueueItem) map[string]any {
	m := checklistItemMap(&it.ChecklistItem, false)
	m["pr_url"] = nilIfEmpty(client.PRURL(it.Task.Project, it.PRNumber))
	m["task"] = map[string]any{
		"id":      it.Task.ID,
		"title":   it.Task.Title,
		"project": projectMap(it.Task.Project),
	}
	return m
}

// queueText is the human view: one row per item with its parent task, so the
// list reads as work rather than as orphaned item ids. PR numbers stay bare in
// the table (the full URL would dominate the width) — json/toon carry pr_url.
func queueText(items []*client.QueueItem, state string) string {
	if len(items) == 0 {
		return sty.render(sty.faint, fmt.Sprintf("(no items in %s)", stateLabel(state)))
	}
	rows := make([][]col, len(items))
	for i, it := range items {
		rows[i] = []col{
			plainCol(fmt.Sprintf("%d", it.ID)),
			plainCol(prCell(it.PRNumber)),
			plainCol(notesFlag(it.HasNotes)),
			plainCol(it.Title),
			plainCol(fmt.Sprintf("#%d %s", it.Task.ID, it.Task.Title)),
			plainCol(projectLabel(it.Task.Project)),
		}
	}
	// The state is the query, not a per-row value — it goes in the heading
	// instead of repeating identically down a column.
	head := sty.render(sty.bold, stateLabel(state)) + sty.render(sty.faint, fmt.Sprintf("  (%d)", len(items)))
	return head + renderTable([]string{"ID", "PR", "NOTES", "TITLE", "TASK", "PROJECT"}, rows)
}
