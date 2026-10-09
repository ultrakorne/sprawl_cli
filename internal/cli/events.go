package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// `events` reads the calling agent key's assignment events: the server's
// record of an item being assigned to this key or taken off it. `watch` follows
// the feed as a JSON-lines stream for a listener to act on; `show` reads one
// event back, e.g. to confirm who assigned an item.

func newEventsCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Follow or read the calling agent key's assignment events (watch, show)",
		Long: "An assignment event is the server's record that an item was assigned to the calling " +
			"agent key (`assigned`) or taken off it (`removed`). Only the key's own events are " +
			"visible, in the selected workspace and, under a project key, that project.\n\n" +
			"`watch` streams them as JSON lines with a cursor per line; `show <id>` reads one back. " +
			"Events carry identifiers only — read the item with `item <checklist_item_id>`, and " +
			"`queue --assignee me` for everything currently assigned.",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.SilenceErrors = true
	cmd.AddCommand(newEventsWatchCmd(opts))
	cmd.AddCommand(newEventsShowCmd(opts))
	return cmd
}

// -- show -------------------------------------------------------------------

func newEventsShowCmd(opts *runtimeOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one assignment event (GET /api/v1/assignment_events/:id)",
		Long: "Reads one of the calling agent key's assignment events by id — the `id` of a " +
			"`watch` line's event (the MCP form `ae_<id>` is accepted too). Use it to confirm who " +
			"assigned an item: the item's `last_actor` moves with every later edit, the event's " +
			"`actor` does not. Another key's event, or one outside the selected workspace or " +
			"confined project, is 404.",
		Args: textArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEventsShow(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], opts)
		},
	}
	cmd.SilenceErrors = true
	return cmd
}

func runEventsShow(ctx context.Context, stdout, stderr io.Writer, id string, opts *runtimeOpts) error {
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	ev, err := c.GetAssignmentEvent(ctx, id)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	ev.Cursor = "" // a feed position, meaningless on a single read
	return renderPayload(stdout, map[string]any{"event": ev}, eventDetailText(ev), opts)
}

// eventDetailText is `events show`'s human view: the change and id as the
// heading, then one faint-keyed line per identifier.
func eventDetailText(ev *client.AssignmentEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", sty.render(sty.bold, fallback(ev.Change, "?")),
		sty.render(sty.faint, fmt.Sprintf("event %d", ev.ID)))
	rows := [][2]string{
		{"item:", fmt.Sprintf("%d", ev.ChecklistItemID)},
		{"task:", fmt.Sprintf("%d", ev.TaskID)},
		{"workspace:", fmt.Sprintf("%d", ev.WorkspaceID)},
		{"revision:", fmt.Sprintf("%d", ev.AssignmentRevision)},
		{"by:", eventActorText(ev.Actor)},
		{"at:", fallback(ev.OccurredAt, "-")},
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "  %s %s\n", sty.render(sty.faint, fmt.Sprintf("%-10s", r[0])), r[1])
	}
	return strings.TrimRight(b.String(), "\n")
}

func eventActorText(a *client.Actor) string {
	if a == nil {
		return "-"
	}
	return fmt.Sprintf("%s:%d", a.Type, a.ID)
}

// -- watch ------------------------------------------------------------------

const (
	defaultEventWait = 25
	minRetryDelay    = time.Second
	maxRetryDelay    = 30 * time.Second
)

// The three line types `events watch` emits, in the order they can appear.
const (
	lineStart         = "start"
	lineEvent         = "assignment_event"
	lineCursorExpired = "cursor_expired"
)

func newEventsWatchCmd(opts *runtimeOpts) *cobra.Command {
	var after string
	var wait int
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream the calling agent key's assignment events as JSON lines (GET /api/v1/assignment_events)",
		Long: "Long-polls the assignment feed and prints one JSON line per event, flushed as it " +
			"arrives, until interrupted (SIGINT / SIGTERM exit 0).\n\n" +
			"Lines:\n" +
			"  {\"type\":\"start\",\"cursor\":…}             once, when started without --after\n" +
			"  {\"type\":\"assignment_event\",\"cursor\":…,\"event\":{…}}\n" +
			"  {\"type\":\"cursor_expired\",\"cursor\":…}    the cursor expired (30 days after its line); resumed from now\n\n" +
			"Each line's cursor is the position just after it. sprawl never stores it: persist it " +
			"yourself once you have handled the line, and restart with --after <cursor> to resume " +
			"there (at-least-once). After `start` or `cursor_expired`, reconcile with " +
			"`queue --assignee me` — events from before that point are not replayed.\n\n" +
			"Network errors, timeouts, 408 / 429 and 5xx are retried with a backoff from 1s to 30s, " +
			"reported on stderr. Any other refusal (401, 403, 404, 422 — e.g. a malformed or " +
			"another key's cursor) ends the stream with the usual error envelope and a non-zero exit.",
		Args: textArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := watchConfig{after: strings.TrimSpace(after), wait: wait}
			return runEventsWatch(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cfg, opts)
		},
	}
	cmd.Flags().StringVar(&after, "after", "",
		"resume after this cursor (from a previous line); omit to start from now")
	cmd.Flags().IntVar(&wait, "wait", defaultEventWait,
		fmt.Sprintf("seconds each request waits for an event before polling again (1-%d)", client.MaxEventWait))
	cmd.SilenceErrors = true
	return cmd
}

// watchConfig is the resolved invocation. sleep is the backoff timer; nil means
// a real one. Tests inject a recorder so retries cost no wall-clock time.
type watchConfig struct {
	after string
	wait  int
	sleep func(context.Context, time.Duration) error
}

func runEventsWatch(ctx context.Context, stdout, stderr io.Writer, cfg watchConfig, opts *runtimeOpts) error {
	// A wait of 0 would turn the long poll into a busy loop; the server's own
	// ceiling is 30.
	if cfg.wait < 1 || cfg.wait > client.MaxEventWait {
		err := fmt.Errorf("--wait must be between 1 and %d seconds", client.MaxEventWait)
		return reportErr(stdout, stderr, err, opts)
	}
	f, err := resolveFormat(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	c, err := newAuthedClient(opts)
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	w := &eventWatcher{c: c, out: stdout, diag: stderr, text: f == FormatText, wait: cfg.wait, sleep: cfg.sleep}
	if w.sleep == nil {
		w.sleep = sleepCtx
	}
	err = w.run(ctx, cfg.after)
	// An interrupt is how a watch is meant to end, not a failure.
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return reportErr(stdout, stderr, err, opts)
	}
	return nil
}

type eventWatcher struct {
	c     *client.Client
	out   io.Writer // the stream
	diag  io.Writer // retry diagnostics
	text  bool
	wait  int
	sleep func(context.Context, time.Duration) error
}

// errNoCursor guards the one server answer that would otherwise spin: a head
// without a cursor, polled again "from now" with no wait, forever.
var errNoCursor = errors.New("the server's assignment feed returned no cursor")

// run follows the feed from cursor ("" = from now) until ctx ends or a refusal
// that retrying can't fix.
func (w *eventWatcher) run(ctx context.Context, cursor string) error {
	if cursor == "" {
		head, err := w.head(ctx)
		if err != nil {
			return err
		}
		cursor = head
		if err := w.emit(ctx, watchLine{Type: lineStart, Cursor: cursor}); err != nil {
			return err
		}
	}
	for {
		page, err := w.poll(ctx, cursor)
		if isCursorExpired(err) {
			head, herr := w.head(ctx)
			if herr != nil {
				return herr
			}
			cursor = head
			if err := w.emit(ctx, watchLine{Type: lineCursorExpired, Cursor: cursor}); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		last := len(page.Events) - 1
		for i, ev := range page.Events {
			at := ev.Cursor
			if at == "" && i == last {
				at = page.NextCursor
			}
			out := *ev
			out.Cursor = ""
			if err := w.emit(ctx, watchLine{Type: lineEvent, Cursor: at, Event: &out}); err != nil {
				return err
			}
			if at != "" {
				cursor = at
			}
		}
		// next_cursor can run past events this scope never sees (filtered by
		// workspace or project), so it beats the last event's own cursor.
		if page.NextCursor != "" {
			cursor = page.NextCursor
		}
	}
}

// head asks for the cursor that marks "now".
func (w *eventWatcher) head(ctx context.Context) (string, error) {
	page, err := w.poll(ctx, "")
	if err != nil {
		return "", err
	}
	if page.NextCursor == "" {
		return "", errNoCursor
	}
	return page.NextCursor, nil
}

// poll makes one feed request, retrying transient failures with a capped
// exponential backoff (1s, 2s, 4s … 30s) that starts over with every call, so
// a recovered connection is back to a 1s first retry next time.
func (w *eventWatcher) poll(ctx context.Context, after string) (*client.AssignmentEventPage, error) {
	delay := minRetryDelay
	for {
		page, err := w.c.ListAssignmentEvents(ctx, after, w.wait)
		if err == nil {
			return page, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !isTransientFeedErr(err) {
			return nil, err
		}
		// A bodiless 5xx renders as "http 503: "; drop the dangling separator.
		msg := strings.TrimRight(err.Error(), ": ")
		_ = writeCtx(ctx, func() error {
			_, err := fmt.Fprintf(w.diag, "%s events watch: %s — retrying in %s\n", build.AppName, msg, delay)
			return err
		})
		if err := w.sleep(ctx, delay); err != nil {
			return nil, err
		}
		delay = min(delay*2, maxRetryDelay)
	}
}

// watchLine is one line of the stream. A struct rather than a map so the keys
// come out in the documented order: type, cursor, event.
type watchLine struct {
	Type   string                  `json:"type"`
	Cursor string                  `json:"cursor"`
	Event  *client.AssignmentEvent `json:"event,omitempty"`
}

// emit writes one line in a single Write, then flushes when the writer
// buffers, so a consumer reading the pipe sees each line as it happens rather
// than when the process exits. It gives up when ctx ends first (writeCtx).
func (w *eventWatcher) emit(ctx context.Context, l watchLine) error {
	var b []byte
	if !w.text {
		var err error
		if b, err = json.Marshal(l); err != nil {
			return fmt.Errorf("write event line: %w", err)
		}
		b = append(b, '\n')
	}
	return writeCtx(ctx, func() error {
		var err error
		if w.text {
			err = writeHuman(w.out, watchLineText(l)+"\n")
		} else {
			_, err = w.out.Write(b)
		}
		if err != nil {
			return fmt.Errorf("write event line: %w", err)
		}
		if f, ok := w.out.(interface{ Flush() error }); ok {
			return f.Flush()
		}
		return nil
	})
}

// writeCtx runs write on its own goroutine and returns ctx's error when ctx
// ends first. A write blocked on a full pipe (a consumer that stopped reading)
// can't be interrupted, and the watch must still end on SIGINT / SIGTERM:
// returning from main ends the process, stuck goroutine and all.
func writeCtx(ctx context.Context, write func() error) error {
	done := make(chan error, 1)
	go func() { done <- write() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// watchLineText is the human rendering of one line: the stream a person
// watches in a terminal, not something to parse.
func watchLineText(l watchLine) string {
	switch l.Type {
	case lineStart:
		return sty.render(sty.faint, "watching assignment events from now — Ctrl+C to stop")
	case lineCursorExpired:
		return sty.render(sty.warn, fmt.Sprintf(
			"cursor expired; resumed from now — earlier events are not replayed, check `%s queue --assignee me`",
			build.AppName))
	}
	ev := l.Event
	return fmt.Sprintf("%s  %s  item %d  task %d  by %s  %s",
		sty.render(sty.faint, ev.OccurredAt), sty.render(sty.bold, fmt.Sprintf("%-8s", ev.Change)),
		ev.ChecklistItemID, ev.TaskID, eventActorText(ev.Actor), sty.render(sty.faint, fmt.Sprintf("event %d", ev.ID)))
}

func isCursorExpired(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && apiErr.Status == 410
}

// isTransientFeedErr reports whether retrying could help: anything that never
// reached a server answer (network, timeout, a truncated body), plus 408, 429
// and 5xx. Every other 4xx is the server's final word on these credentials or
// this cursor, and 410 has its own recovery path.
func isTransientFeedErr(err error) bool {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return true
	}
	switch s := apiErr.Status; {
	case s == 408, s == 429:
		return true
	case s >= 500:
		return true
	}
	return false
}

// sleepCtx waits d, or until ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
