package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// -- fixtures -----------------------------------------------------------------

// lineRecorder is the watch's stdout: it counts writes and flushes and is safe
// to read from the fake server's goroutine while the watch is mid-request.
type lineRecorder struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	writes  int
	flushes int
}

func (r *lineRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes++
	return r.buf.Write(p)
}

func (r *lineRecorder) Flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushes++
	return nil
}

func (r *lineRecorder) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := strings.TrimRight(r.buf.String(), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// feedScript answers the watch's requests in order, one step per request,
// then cancels the watch (as SIGINT would) and holds the last request open
// until the cancellation tears it down.
type feedScript struct {
	mu     sync.Mutex
	reqs   []*url.URL
	steps  []http.HandlerFunc
	cancel context.CancelFunc
}

func (s *feedScript) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	i := len(s.reqs)
	s.reqs = append(s.reqs, r.URL)
	s.mu.Unlock()
	if i < len(s.steps) {
		s.steps[i](w, r)
		return
	}
	s.cancel()
	<-r.Context().Done()
}

func (s *feedScript) requests() []*url.URL {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*url.URL(nil), s.reqs...)
}

func feedPage(next string, events ...map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if events == nil {
			events = []map[string]any{}
		}
		writeFeedJSON(w, 200, map[string]any{"events": events, "next_cursor": next, "has_more": false})
	}
}

func feedStatus(status int, code string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeFeedJSON(w, status, map[string]any{"error": code})
	}
}

// feedDrop closes the connection without an answer: a network failure.
func feedDrop(w http.ResponseWriter, r *http.Request) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func writeFeedJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func feedEvent(id int64, change, cursor string) map[string]any {
	return map[string]any{
		"id": id, "change": change, "assignment_revision": 2, "workspace_id": 7, "task_id": 1001,
		"checklist_item_id": 5555, "actor": map[string]any{"type": "user", "id": 3},
		"occurred_at": "2026-10-09T12:00:00.123456Z", "cursor": cursor,
	}
}

type watchRun struct {
	script *feedScript
	out    *lineRecorder
	stderr *bytes.Buffer
	sleeps []time.Duration
	err    error
}

// runWatch drives runEventsWatch against a scripted feed until the script runs
// out. A step that inspects stdout mid-stream builds the recorder itself and
// passes it to runWatchInto. Keep-alives are off so every request gets a fresh connection: the
// transport would otherwise silently replay a GET whose reused connection was
// dropped, and a dropped-connection step would be answered twice.
func runWatch(t *testing.T, format string, cfg watchConfig, mutate func(*runtimeOpts), steps ...http.HandlerFunc) *watchRun {
	t.Helper()
	return runWatchInto(t, &lineRecorder{}, format, cfg, mutate, steps...)
}

func runWatchInto(t *testing.T, out *lineRecorder, format string, cfg watchConfig, mutate func(*runtimeOpts), steps ...http.HandlerFunc) *watchRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	run := &watchRun{script: &feedScript{steps: steps, cancel: cancel}, out: out, stderr: &bytes.Buffer{}}
	fx := newAuthedFixture(t, format, run.script.handle)
	fx.Server.Config.SetKeepAlivesEnabled(false)
	if mutate != nil {
		mutate(fx.Opts)
	}
	if cfg.wait == 0 {
		cfg.wait = defaultEventWait
	}
	cfg.sleep = func(_ context.Context, d time.Duration) error {
		run.sleeps = append(run.sleeps, d)
		return nil
	}
	run.err = runEventsWatch(ctx, run.out, run.stderr, cfg, fx.Opts)
	return run
}

// -- watch --------------------------------------------------------------------

// Without --after the watch opens on the head cursor (a `start` line), then
// prints each event as its own flushed line while the next request is still
// pending — a consumer sees it before the process exits.
func TestEventsWatch_StartLineThenStreamsEachLine(t *testing.T) {
	midStream := make(chan []string, 1)
	out := &lineRecorder{}
	run := runWatchInto(t, out, "json", watchConfig{}, nil,
		feedPage("h0"),
		feedPage("c2", feedEvent(1, "assigned", "c1"), feedEvent(2, "removed", "c2")),
		func(w http.ResponseWriter, r *http.Request) {
			midStream <- out.lines()
			feedPage("c2")(w, r)
		},
	)
	if run.err != nil {
		t.Fatalf("watch: %v — %s", run.err, run.stderr.String())
	}
	want := []string{
		`{"type":"start","cursor":"h0"}`,
		`{"type":"assignment_event","cursor":"c1","event":{"id":1,"change":"assigned","assignment_revision":2,"workspace_id":7,"task_id":1001,"checklist_item_id":5555,"actor":{"type":"user","id":3},"occurred_at":"2026-10-09T12:00:00.123456Z"}}`,
		`{"type":"assignment_event","cursor":"c2","event":{"id":2,"change":"removed","assignment_revision":2,"workspace_id":7,"task_id":1001,"checklist_item_id":5555,"actor":{"type":"user","id":3},"occurred_at":"2026-10-09T12:00:00.123456Z"}}`,
	}
	got := run.out.lines()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if seen := <-midStream; len(seen) != 3 {
		t.Fatalf("lines visible while the next poll was pending = %d, want 3", len(seen))
	}
	if run.out.writes != 3 || run.out.flushes != 3 {
		t.Fatalf("writes = %d, flushes = %d; want one write and one flush per line", run.out.writes, run.out.flushes)
	}

	reqs := run.script.requests()
	if len(reqs[0].Query()) != 0 {
		t.Fatalf("head request carried params: %v", reqs[0].Query())
	}
	for i, wantAfter := range []string{"h0", "c2", "c2"} {
		q := reqs[i+1].Query()
		if q.Get("after") != wantAfter || q.Get("wait") != "25" {
			t.Fatalf("request %d query = %v, want after=%s wait=25", i+1, q, wantAfter)
		}
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q", run.stderr.String())
	}
}

func TestEventsWatch_AfterResumesWithoutStartLine(t *testing.T) {
	run := runWatch(t, "json", watchConfig{after: "c7", wait: 9}, nil,
		feedPage("c8", feedEvent(8, "assigned", "c8")),
	)
	if run.err != nil {
		t.Fatalf("watch: %v", run.err)
	}
	lines := run.out.lines()
	if len(lines) != 1 || !strings.HasPrefix(lines[0], `{"type":"assignment_event","cursor":"c8"`) {
		t.Fatalf("lines = %v", lines)
	}
	reqs := run.script.requests()
	if q := reqs[0].Query(); q.Get("after") != "c7" || q.Get("wait") != "9" {
		t.Fatalf("first query = %v", q)
	}
	if q := reqs[1].Query(); q.Get("after") != "c8" {
		t.Fatalf("second query = %v", q)
	}
}

// next_cursor can run past events this scope never sees, so it wins over the
// last event's own cursor; an empty page that advances it is followed too.
func TestEventsWatch_FollowsNextCursorPastFilteredEvents(t *testing.T) {
	run := runWatch(t, "json", watchConfig{after: "c1"}, nil,
		feedPage("c5", feedEvent(2, "assigned", "c2")),
		feedPage("c9"),
	)
	if run.err != nil {
		t.Fatal(run.err)
	}
	reqs := run.script.requests()
	if reqs[1].Query().Get("after") != "c5" || reqs[2].Query().Get("after") != "c9" {
		t.Fatalf("afters = %s, %s", reqs[1].Query().Get("after"), reqs[2].Query().Get("after"))
	}
}

// 410 → a cursor_expired line carrying a fresh head, then the watch carries on
// from that head.
func TestEventsWatch_ExpiredCursorResumesFromHead(t *testing.T) {
	run := runWatch(t, "json", watchConfig{after: "stale"}, nil,
		feedStatus(410, "cursor_expired"),
		feedPage("h1"),
		feedPage("c3", feedEvent(3, "assigned", "c3")),
	)
	if run.err != nil {
		t.Fatalf("watch: %v", run.err)
	}
	lines := run.out.lines()
	if len(lines) != 2 || lines[0] != `{"type":"cursor_expired","cursor":"h1"}` ||
		!strings.HasPrefix(lines[1], `{"type":"assignment_event","cursor":"c3"`) {
		t.Fatalf("lines = %v", lines)
	}
	reqs := run.script.requests()
	if len(reqs[1].Query()) != 0 {
		t.Fatalf("recovery should ask for the head, got %v", reqs[1].Query())
	}
	if reqs[2].Query().Get("after") != "h1" {
		t.Fatalf("resume after = %q", reqs[2].Query().Get("after"))
	}
	if len(run.sleeps) != 0 {
		t.Fatalf("expiry is not a retry: slept %v", run.sleeps)
	}
}

// Network failures, 408/429 and 5xx back off 1s → 30s on the same cursor,
// reported on stderr; a success resets the backoff.
func TestEventsWatch_BacksOffOnTransientFailures(t *testing.T) {
	run := runWatch(t, "json", watchConfig{after: "c1"}, nil,
		feedStatus(503, ""),
		feedDrop,
		feedStatus(502, ""),
		feedStatus(500, "internal"),
		feedStatus(429, "rate_limited"),
		feedStatus(408, ""),
		feedStatus(500, ""),
		feedStatus(504, ""),
		feedPage("c1"),
		feedStatus(500, ""),
	)
	if run.err != nil {
		t.Fatalf("watch: %v", run.err)
	}
	s := time.Second
	want := []time.Duration{s, 2 * s, 4 * s, 8 * s, 16 * s, 30 * s, 30 * s, 30 * s, s}
	if len(run.sleeps) != len(want) {
		t.Fatalf("sleeps = %v, want %v", run.sleeps, want)
	}
	for i := range want {
		if run.sleeps[i] != want[i] {
			t.Fatalf("sleeps = %v, want %v", run.sleeps, want)
		}
	}
	for i, r := range run.script.requests() {
		if r.Query().Get("after") != "c1" {
			t.Fatalf("request %d moved the cursor: %v", i, r.Query())
		}
	}
	if got := strings.Count(run.stderr.String(), "retrying in"); got != len(want) {
		t.Fatalf("stderr diagnostics = %d, want %d:\n%s", got, len(want), run.stderr.String())
	}
	if len(run.out.lines()) != 0 {
		t.Fatalf("retries must not reach stdout: %v", run.out.lines())
	}
}

// Refusals that retrying can't fix end the stream with the server's error and
// a non-zero exit, without a retry.
func TestEventsWatch_FatalRefusalsExitWithServerError(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{
		{401, "unauthenticated"},
		{403, "forbidden"},
		{422, "invalid_cursor"},
		{404, "not_found"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			run := runWatch(t, "json", watchConfig{after: "c1"}, nil, feedStatus(tc.status, tc.code))
			if run.err == nil {
				t.Fatal("want a non-zero exit")
			}
			if len(run.sleeps) != 0 || len(run.script.requests()) != 1 {
				t.Fatalf("retried a fatal refusal: sleeps %v, requests %d", run.sleeps, len(run.script.requests()))
			}
			var env struct {
				Status     string `json:"status"`
				Error      string `json:"error"`
				HTTPStatus int    `json:"http_status"`
			}
			lines := run.out.lines()
			if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &env) != nil {
				t.Fatalf("stdout = %v", lines)
			}
			if env.Status != "error" || env.Error != tc.code || env.HTTPStatus != tc.status {
				t.Fatalf("envelope = %+v", env)
			}
		})
	}
}

// Cancellation (SIGINT / SIGTERM via main's signal context) mid-request ends
// the watch cleanly: exit 0, nothing on stderr, no error envelope.
func TestEventsWatch_InterruptExitsCleanly(t *testing.T) {
	run := runWatch(t, "json", watchConfig{}, nil, feedPage("h0"))
	if run.err != nil {
		t.Fatalf("interrupt should exit 0, got %v", run.err)
	}
	if lines := run.out.lines(); len(lines) != 1 || lines[0] != `{"type":"start","cursor":"h0"}` {
		t.Fatalf("stdout = %v", lines)
	}
	if run.stderr.Len() != 0 {
		t.Fatalf("stderr = %q", run.stderr.String())
	}
}

// stalledPipe is a stream whose reader stopped reading: every Write blocks
// until the test releases it, like a write to a full pipe.
type stalledPipe struct {
	entered chan struct{}
	release chan struct{}
}

func newStalledPipe() *stalledPipe {
	return &stalledPipe{entered: make(chan struct{}, 1), release: make(chan struct{})}
}

func (p *stalledPipe) Write(b []byte) (int, error) {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	<-p.release
	return len(b), nil
}

// An interrupt ends the watch even while a write to stdout (a line) or stderr
// (a retry diagnostic) is blocked on a consumer that stopped reading.
func TestEventsWatch_InterruptWhileAWriteIsBlocked(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"stdout line", 200},
		{"stderr diagnostic", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
				writeFeedJSON(w, tc.status, map[string]any{"events": []any{}, "next_cursor": "h0", "has_more": false})
			})
			pipe := newStalledPipe()
			defer close(pipe.release) // lets the abandoned write goroutine finish
			stdout, stderr := io.Writer(&lineRecorder{}), io.Writer(&lineRecorder{})
			if tc.status == 200 {
				stdout = pipe
			} else {
				stderr = pipe
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- runEventsWatch(ctx, stdout, stderr, watchConfig{wait: defaultEventWait}, fx.Opts)
			}()

			select {
			case <-pipe.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("the watch never wrote")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("interrupt should exit 0, got %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the watch did not return while its write was blocked")
			}
		})
	}
}

// Also during a backoff sleep: the real timer returns as soon as ctx ends.
func TestSleepCtx_ReturnsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); err == nil {
		t.Fatal("want ctx error")
	}
}

func TestEventsWatch_WorkspaceSelectorPrefixesPath(t *testing.T) {
	run := runWatch(t, "json", watchConfig{}, func(o *runtimeOpts) { o.workspace = "7" }, feedPage("h0"))
	if run.err != nil {
		t.Fatal(run.err)
	}
	for _, r := range run.script.requests() {
		if r.Path != "/api/v1/workspaces/7/assignment_events" {
			t.Fatalf("path = %q", r.Path)
		}
	}
}

func TestEventsWatch_RejectsWaitOutOfRangeBeforeHTTP(t *testing.T) {
	for _, wait := range []int{-1, 31} {
		run := runWatch(t, "json", watchConfig{wait: wait}, nil)
		if run.err == nil || len(run.script.requests()) != 0 {
			t.Fatalf("wait %d: err %v, requests %d", wait, run.err, len(run.script.requests()))
		}
	}
}

func TestEventsWatchCmd_ZeroWaitRefused(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("request escaped: %s", r.URL)
	})
	cmd := newEventsWatchCmd(fx.Opts)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--wait", "0"})
	if err := cmd.Execute(); err == nil || !strings.Contains(out.String(), "--wait must be between 1 and 30") {
		t.Fatalf("err = %v, out = %s", err, out.String())
	}
}

func TestEventsWatch_TextFormatIsHumanLines(t *testing.T) {
	run := runWatch(t, "text", watchConfig{}, nil,
		feedPage("h0"),
		feedPage("c1", feedEvent(1, "assigned", "c1")),
		feedStatus(410, "cursor_expired"),
		feedPage("h2"),
	)
	if run.err != nil {
		t.Fatal(run.err)
	}
	lines := run.out.lines()
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.Contains(lines[0], "watching") ||
		!strings.Contains(lines[1], "assigned") || !strings.Contains(lines[1], "item 5555") || !strings.Contains(lines[1], "by user:3") ||
		!strings.Contains(lines[2], "cursor expired") || !strings.Contains(lines[2], "queue --assignee me") {
		t.Fatalf("lines = %q", lines)
	}
}

// -- show ---------------------------------------------------------------------

func TestEventsShow_JSONAndText(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			var gotPath string
			fx := newAuthedFixture(t, format, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				writeFeedJSON(w, 200, map[string]any{"assignment_event": feedEvent(123, "assigned", "")})
			})
			fx.Opts.workspace = "7"
			var out, stderr bytes.Buffer
			if err := runEventsShow(context.Background(), &out, &stderr, "123", fx.Opts); err != nil {
				t.Fatalf("show: %v — %s", err, stderr.String())
			}
			if gotPath != "/api/v1/workspaces/7/assignment_events/123" {
				t.Fatalf("path = %q", gotPath)
			}
			if format == "text" {
				for _, want := range []string{"assigned", "event 123", "item:", "5555", "task:", "1001", "by:", "user:3", "revision:"} {
					if !strings.Contains(out.String(), want) {
						t.Fatalf("missing %q in:\n%s", want, out.String())
					}
				}
				return
			}
			want := `{"event":{"id":123,"change":"assigned","assignment_revision":2,"workspace_id":7,"task_id":1001,"checklist_item_id":5555,"actor":{"type":"user","id":3},"occurred_at":"2026-10-09T12:00:00.123456Z"}}`
			if strings.TrimSpace(out.String()) != want {
				t.Fatalf("out = %s\nwant %s", out.String(), want)
			}
		})
	}
}

func TestEventsShow_NotFound(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		writeFeedJSON(w, 404, map[string]any{"error": "not_found"})
	})
	var out bytes.Buffer
	if err := runEventsShow(context.Background(), &out, &bytes.Buffer{}, "9", fx.Opts); err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(out.String(), `"http_status":404`) {
		t.Fatalf("out = %s", out.String())
	}
}

// -- queue --assignee ---------------------------------------------------------

func TestQueueCmd_AssigneeFilter(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want url.Values
	}{
		{"assignee alone drops the default state", []string{"--assignee", "me"}, url.Values{"assignee": {"me"}}},
		{"assignee narrowed by state", []string{"--assignee", "ME", "--state", "review"}, url.Values{"assignee": {"me"}, "state": {"in_review"}}},
		{"assignee with full", []string{"--assignee", "me", "--full"}, url.Values{"assignee": {"me"}, "full": {"true"}}},
		{"no assignee keeps the ready default", nil, url.Values{"state": {"ready_to_pickup"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got url.Values
			fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query()
				writeFeedJSON(w, 200, map[string]any{"tasks": []any{}})
			})
			cmd := newQueueCmd(fx.Opts)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got.Encode() != tc.want.Encode() {
				t.Fatalf("query = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestQueueCmd_AssigneeRejectsOthersAndNoneBeforeHTTP(t *testing.T) {
	for _, args := range [][]string{{"--assignee", "agent_key:7"}, {"--assignee", "me", "--state", "none"}} {
		fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("request escaped: %s", r.URL)
		})
		cmd := newQueueCmd(fx.Opts)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("%v: want an error", args)
		}
	}
}

// An assignee-only queue spans states, so its table keeps the STATE column the
// by-state queue drops, and its heading says whose items these are.
func TestQueueText_AssigneeHeadingAndStateColumn(t *testing.T) {
	groups := []*client.QueueTask{{ID: 3, Title: "Ship", ChecklistItems: []*client.ChecklistItem{
		{ID: 7, Title: "migrate", State: client.StateInProgress, Assignee: &client.Assignee{Type: "agent_key", ID: 4}},
	}}}
	mine := queueText(groups, client.QueueFilter{Assignee: client.AssigneeMe})
	if !strings.Contains(mine, "assigned to me") || !strings.Contains(mine, "STATE") {
		t.Fatalf("assignee queue:\n%s", mine)
	}
	both := queueText(groups, client.QueueFilter{Assignee: client.AssigneeMe, State: client.StateInProgress})
	if !strings.Contains(both, "assigned to me · progress") || strings.Contains(both, "STATE") {
		t.Fatalf("assignee+state queue:\n%s", both)
	}
	if empty := queueText(nil, client.QueueFilter{Assignee: client.AssigneeMe}); !strings.Contains(empty, "no items assigned to me") {
		t.Fatalf("empty = %q", empty)
	}
}

// The item's assignment_revision rides through every item view, null when the
// server predates it.
func TestItemMap_AssignmentRevision(t *testing.T) {
	rev := int64(4)
	b, _ := json.Marshal(itemMap(&client.ChecklistItem{ID: 1, AssignmentRevision: &rev}, nil, false))
	if !strings.Contains(string(b), `"assignment_revision":4`) {
		t.Fatalf("map = %s", b)
	}
	b, _ = json.Marshal(itemMap(&client.ChecklistItem{ID: 1}, nil, false))
	if !strings.Contains(string(b), `"assignment_revision":null`) {
		t.Fatalf("map = %s", b)
	}
}
