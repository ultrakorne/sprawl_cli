package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func newActivityCmd(opts *runtimeOpts) *cobra.Command {
	var date, daysAgo string
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Show completed tasks and checklist items for a day (GET /api/v1/activity_log)",
		Long: "Returns the calling agent's completed tasks and completed checklist items for " +
			"a single day, scoped by the same permission cascade as `task list`. " +
			"Default is today in the user's timezone. --date and --days-ago are mutually " +
			"exclusive; --days-ago must be an integer in 0..365 (the upper bound is " +
			"enforced server-side).",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runActivity(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), date, daysAgo, opts)
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "specific date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&daysAgo, "days-ago", "", "non-negative integer (0=today, 1=yesterday, …)")
	cmd.SilenceErrors = true
	return cmd
}

func runActivity(ctx context.Context, stdout, stderr io.Writer, date, daysAgo string, opts *runtimeOpts) error {
	if err := validateActivityParams(date, daysAgo); err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	log, err := c.GetActivityLog(ctx, date, daysAgo)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return renderPayload(stdout, activityPayload(log), activityText(log), opts)
}

// validateActivityParams catches the obvious local errors before any HTTP
// round-trip. The server still has the final say on the 365-day cap; here we
// only enforce mutual exclusion, integer-ness of --days-ago (with a
// non-negative lower bound), and YYYY-MM-DD parseability of --date.
func validateActivityParams(date, daysAgo string) error {
	if date != "" && daysAgo != "" {
		return errors.New("pass --date OR --days-ago, not both")
	}
	if daysAgo != "" {
		n, err := strconv.Atoi(daysAgo)
		if err != nil || n < 0 {
			return fmt.Errorf("--days-ago must be a non-negative integer, got %q", daysAgo)
		}
	}
	if date != "" {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return fmt.Errorf("--date must be YYYY-MM-DD, got %q", date)
		}
	}
	return nil
}

// activityPayload preserves the wire shape (`date`, `completed_tasks`,
// `completed_items`) for json / toon and adds the `status: ok` envelope the
// other read commands carry. Tasks reuse taskMap so the shape stays identical
// to `task list` / `task <id>` output; items go through activityItemMap.
func activityPayload(log *client.ActivityLog) map[string]any {
	tasks := make([]any, 0, len(log.CompletedTasks))
	for _, t := range log.CompletedTasks {
		tasks = append(tasks, taskMap(t))
	}
	items := make([]any, 0, len(log.CompletedItems))
	for _, it := range log.CompletedItems {
		items = append(items, activityItemMap(it))
	}
	return map[string]any{
		"status":          "ok",
		"date":            log.Date,
		"completed_tasks": tasks,
		"completed_items": items,
	}
}

func activityItemMap(it *client.ActivityChecklistItem) map[string]any {
	return map[string]any{
		"id":           it.ID,
		"title":        it.Title,
		"completed":    it.Completed,
		"completed_at": it.CompletedAt,
		"position":     it.Position,
		"has_notes":    it.HasNotes,
		"last_actor":   actorMap(it.LastActor),
		"task": map[string]any{
			"id":      it.Task.ID,
			"title":   it.Task.Title,
			"project": projectMap(it.Task.Project),
		},
	}
}

// activityText is the human-friendly fallback. Two tabwriter-aligned tables,
// one per array, separated by a blank line. Empty-on-both renders as a single
// line so `(no activity)` is unambiguous.
func activityText(log *client.ActivityLog) string {
	if len(log.CompletedTasks) == 0 && len(log.CompletedItems) == 0 {
		return fmt.Sprintf("activity for %s\n(no activity)", log.Date)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "activity for %s\n", log.Date)

	if len(log.CompletedTasks) > 0 {
		fmt.Fprintf(&b, "\ncompleted tasks (%d)\n", len(log.CompletedTasks))
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, taskListHeader)
		for _, t := range log.CompletedTasks {
			fmt.Fprintln(tw, taskRowFields(t))
		}
		_ = tw.Flush()
	}

	if len(log.CompletedItems) > 0 {
		fmt.Fprintf(&b, "\ncompleted items (%d)\n", len(log.CompletedItems))
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tCOMPLETED_AT\tPROJECT\tTASK\tTITLE")
		for _, it := range log.CompletedItems {
			fmt.Fprintf(tw, "%d\t%s\t%s\t#%d %s\t%s\n",
				it.ID,
				fallback(it.CompletedAt, "-"),
				projectLabel(it.Task.Project),
				it.Task.ID,
				it.Task.Title,
				it.Title,
			)
		}
		_ = tw.Flush()
	}

	return strings.TrimRight(b.String(), "\n")
}
