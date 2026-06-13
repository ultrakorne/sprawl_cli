package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// authedFixture stitches together the preconditions every `run*` test needs:
// a scratch XDG config dir with a token, SPRAWL_API_URL pointing at the mock
// server, an httptest.Server, and a runtimeOpts carrying an agent secret +
// format. Each caller's handler shapes the response; the fixture owns auth.
type authedFixture struct {
	Server *httptest.Server
	Opts   *runtimeOpts
}

func newAuthedFixture(t *testing.T, format string, handler http.HandlerFunc) *authedFixture {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := config.Save(build.AppName, &config.Config{Token: "the-token"}); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	t.Setenv("SPRAWL_TOKEN", "the-token")
	t.Setenv("SPRAWL_AGENT_SECRET", "")

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("SPRAWL_API_URL", srv.URL)

	return &authedFixture{
		Server: srv,
		Opts:   &runtimeOpts{format: format, agentSecret: "the-secret"},
	}
}

// -- pure text-format helpers ----------------------------------------------

func TestTaskListText_Empty(t *testing.T) {
	got := taskListText(nil)
	if got != "(no tasks)" {
		t.Fatalf("empty list = %q", got)
	}
}

func TestTaskListText_FormatsRows(t *testing.T) {
	tasks := []*client.Task{
		{
			ID: 1, Title: "First", Status: "not_started",
			ChecklistProgress: client.ChecklistProgress{Done: 0, Total: 0},
		},
		{
			ID: 2, Title: "Second", Status: "in_progress", DueDate: "2026-04-25",
			Project:           &client.Project{ID: 7, Name: "Engineering"},
			ChecklistProgress: client.ChecklistProgress{Done: 1, Total: 3},
		},
	}
	got := taskListText(tasks)
	for _, want := range []string{"ID", "PROGRESS", "First", "Second", "Engineering", "1/3"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Empty due date should render as `-`, not an empty column.
	lines := strings.Split(got, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected blank + header + rule + 2 rows, got %d lines:\n%s", len(lines), got)
	}
	if lines[0] != "" {
		t.Fatalf("expected a leading blank line above the header, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[2], "─") {
		t.Fatalf("expected a header rule on line 3, got %q", lines[2])
	}
}

func TestTaskDetailText_IncludesDescription(t *testing.T) {
	task := &client.Task{
		ID: 17, Title: "hello", Status: "done", Description: "body copy",
		ChecklistProgress: client.ChecklistProgress{Done: 2, Total: 3},
		Project:           &client.Project{ID: 1, Name: "P"},
		CreatedBy:         &client.Actor{Type: "user", ID: 5},
	}
	got := taskDetailText(task)
	// The non-full view is a bordered card: title in the border, a
	// project/due/progress grid, then the description below.
	for _, want := range []string{"╭─ ", "#17  hello", "progress", "2/3", "body copy"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// created_by / last_actor are intentionally dropped from the human card
	// (still in the json/toon payload via taskMap).
	if strings.Contains(got, "user#5") {
		t.Errorf("created_by should not appear in the human card:\n%s", got)
	}
}

func TestTaskDetailText_OmitsBlankDescription(t *testing.T) {
	task := &client.Task{ID: 1, Title: "x", Status: "done"}
	got := taskDetailText(task)
	// Ends with the created_by line, not with the description body.
	if strings.Contains(got, "\n\n") {
		t.Fatalf("blank description should not leave a trailing blank block:\n%s", got)
	}
}

// TestTaskFullText_WrapsNotesWithHangingIndent locks in the full view's note
// reflow: when outputWidth is known, a long note wraps to the remaining width
// and every continuation line is indented to where the note text starts (under
// the title), never exceeding the terminal width. Wrapping runs on plain text,
// so the styled render strips back to the plain one (the package invariant).
func TestTaskFullText_WrapsNotesWithHangingIndent(t *testing.T) {
	defer func() { stylesEnabled = false; outputWidth = 0 }()

	const width = 64
	longNote := "maybe we need some sort of approve state to distinguish between an agent finishing an item and a human approving it"
	task := &client.Task{
		ID: 295, Title: "Agentic", Status: "in_progress",
		ChecklistProgress: client.ChecklistProgress{Done: 0, Total: 1},
		ChecklistItems: []*client.ChecklistItem{
			{ID: 295, Title: "approve as agents mark items done", Notes: &longNote},
		},
	}

	outputWidth = width
	stylesEnabled = false
	plain := taskDetailText(task)

	// id "#295" ⇒ notes indent is 6 + len("#295") = 10 columns.
	const indent = 10
	pad := strings.Repeat(" ", indent)
	var noteLines int
	for _, ln := range strings.Split(plain, "\n") {
		// Note lines are the faint, indented ones that aren't the item row
		// (which contains the checkbox glyph) and aren't blank.
		if !strings.HasPrefix(ln, pad) || strings.TrimSpace(ln) == "" {
			continue
		}
		if strings.ContainsAny(ln, "☐☑") {
			continue
		}
		noteLines++
		if w := len([]rune(ln)); w > width {
			t.Fatalf("wrapped note line exceeds width %d (got %d): %q", width, w, ln)
		}
		// Continuation alignment: every note line starts exactly at the indent
		// column — no more, no less — so the block is flush under the title.
		if strings.HasPrefix(ln, pad+" ") || !strings.HasPrefix(ln, pad) {
			t.Fatalf("note line not aligned to the %d-col hanging indent: %q", indent, ln)
		}
	}
	if noteLines < 2 {
		t.Fatalf("expected the long note to wrap onto multiple lines, got %d:\n%s", noteLines, plain)
	}

	stylesEnabled = true
	styled := taskDetailText(task)
	if got := stripANSI(styled); got != plain {
		t.Fatalf("wrapping broke stripANSI==plain:\nplain:\n%q\nstripped:\n%q", plain, got)
	}
}

// -- runTaskList ------------------------------------------------------------

func TestRunTaskList_JSONEnvelope(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tasks": []any{
				map[string]any{
					"id": 1, "title": "First", "description": "", "status": "not_started",
					"due_date": nil, "project": nil,
					"checklist_progress": map[string]any{"done": 0, "total": 0},
					"created_by":         nil, "last_actor": nil,
				},
			},
		})
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskList(context.Background(), &stdout, &stderr, fx.Opts); err != nil {
		t.Fatalf("runTaskList: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	tasks, ok := out["tasks"].([]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("tasks shape = %+v", out["tasks"])
	}
	first := tasks[0].(map[string]any)
	if first["title"] != "First" {
		t.Fatalf("title = %v", first["title"])
	}
}

func TestRunTaskList_TextFallbackGoesToStdout(t *testing.T) {
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskList(context.Background(), &stdout, &stderr, fx.Opts); err != nil {
		t.Fatalf("runTaskList: %v", err)
	}
	if !strings.Contains(stdout.String(), "(no tasks)") {
		t.Fatalf("stdout = %q, want (no tasks)", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty on success, got %q", stderr.String())
	}
}

func TestRunTaskList_APIErrorGoesToStdout(t *testing.T) {
	// With --format=json, API errors render as a structured payload on stdout
	// so agents parsing stdout don't have to read stderr separately.
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	})

	var stdout, stderr bytes.Buffer
	err := runTaskList(context.Background(), &stdout, &stderr, fx.Opts)
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty in json mode, got %q", stderr.String())
	}
	var out map[string]any
	if jerr := json.Unmarshal(stdout.Bytes(), &out); jerr != nil {
		t.Fatalf("not JSON: %v (%q)", jerr, stdout.String())
	}
	if out["error"] != "forbidden" || out["status"] != "error" {
		t.Fatalf("payload = %+v", out)
	}
}

// -- runTaskShow ------------------------------------------------------------

func TestRunTaskShow_JSONEnvelope(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/42" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"id": 42, "title": "hello", "description": "d", "status": "done",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 0, "total": 0},
				"created_by":         nil, "last_actor": nil,
			},
		})
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskShow(context.Background(), &stdout, &stderr, "42", false, fx.Opts); err != nil {
		t.Fatalf("runTaskShow: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	task, ok := out["task"].(map[string]any)
	if !ok || task["id"] == nil || task["title"] != "hello" {
		t.Fatalf("task = %+v", out["task"])
	}
}

func TestRunTaskShow_FullEmbedsChecklistAndNotes(t *testing.T) {
	// ?full=true must be sent, and the embedded checklist_items (each with its
	// notes blob) must survive into the rendered envelope verbatim.
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/42" || r.URL.Query().Get("full") != "true" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"id": 42, "title": "hello", "description": "d", "status": "done",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 1, "total": 2},
				"created_by":         nil, "last_actor": nil,
				"checklist_items": []any{
					map[string]any{
						"id": 5, "title": "step one", "completed": true, "position": 0,
						"has_notes": true, "notes": "do the thing", "last_actor": nil,
					},
					map[string]any{
						"id": 6, "title": "step two", "completed": false, "position": 1,
						"has_notes": false, "notes": nil, "last_actor": nil,
					},
				},
			},
		})
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskShow(context.Background(), &stdout, &stderr, "42", true, fx.Opts); err != nil {
		t.Fatalf("runTaskShow: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	task := out["task"].(map[string]any)
	items, ok := task["checklist_items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("checklist_items = %+v", task["checklist_items"])
	}
	first := items[0].(map[string]any)
	if first["notes"] != "do the thing" {
		t.Fatalf("first item notes = %+v", first["notes"])
	}
	// Empty notes on the full path survive as a present-but-null key — not
	// omitted, not "" — mirroring the server's null and `note show`.
	second := items[1].(map[string]any)
	if v, present := second["notes"]; !present || v != nil {
		t.Fatalf("second item notes = %+v (present=%v), want null", v, present)
	}
}

func TestRunTaskShow_NonFullOmitsChecklistItems(t *testing.T) {
	// Without --full the server returns no checklist_items key; the rendered
	// envelope must not grow one (nil slice ⇒ suppressed by taskMap).
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("full"); q != "" {
			t.Errorf("full query should be absent, got %q", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"id": 42, "title": "hello", "description": "", "status": "done",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 0, "total": 0},
				"created_by":         nil, "last_actor": nil,
			},
		})
	})
	var stdout, stderr bytes.Buffer
	if err := runTaskShow(context.Background(), &stdout, &stderr, "42", false, fx.Opts); err != nil {
		t.Fatalf("runTaskShow: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if _, present := out["task"].(map[string]any)["checklist_items"]; present {
		t.Fatalf("checklist_items must be absent on non-full show: %s", stdout.String())
	}
}

func TestRunTaskShow_FullTextRendersChecklistBlock(t *testing.T) {
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"id": 42, "title": "ship it", "description": "", "status": "in_progress",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 1, "total": 2},
				"created_by":         nil, "last_actor": nil,
				"checklist_items": []any{
					map[string]any{
						"id": 5, "title": "done step", "completed": true, "position": 0,
						"has_notes": true, "notes": "did it", "last_actor": nil,
					},
					map[string]any{
						"id": 6, "title": "todo step", "completed": false, "position": 1,
						"has_notes": false, "notes": nil, "last_actor": nil,
					},
				},
			},
		})
	})
	var stdout, stderr bytes.Buffer
	if err := runTaskShow(context.Background(), &stdout, &stderr, "42", true, fx.Opts); err != nil {
		t.Fatalf("runTaskShow: %v", err)
	}
	got := stdout.String()
	// The --full text view is the boxed header + clean checklist: id/title in
	// the box, a CHECKLIST section, unicode checkboxes, and notes nested under
	// each item verbatim (no "notes:" prefix). Empty notes read "(no notes)".
	for _, want := range []string{
		"╭─ ", "#42  ship it", // boxed header with title in the border
		"CHECKLIST",
		"☑ #5", "done step", "did it", // completed item + its inline note
		"☐ #6", "todo step", "(no notes)", // open item, no note
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("text output missing %q:\n%s", want, got)
		}
	}
}

// -- runTaskSearch ----------------------------------------------------------

func TestRunTaskSearch_JSONIncludesMatchedChecklistItems(t *testing.T) {
	// /search results carry a per-task `matched_checklist_items` array. The
	// CLI must pass it through to the rendered envelope verbatim, including
	// the empty-array case (= "matched on title") which is distinct from
	// the field being absent on list/show responses.
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/search" || r.URL.Query().Get("q") != "needle" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tasks": []any{
				map[string]any{
					"id": 1, "title": "needle in title", "description": "", "status": "not_started",
					"due_date": nil, "project": nil,
					"checklist_progress":      map[string]any{"done": 0, "total": 0},
					"created_by":              nil,
					"last_actor":              nil,
					"matched_checklist_items": []any{},
				},
				map[string]any{
					"id": 2, "title": "no title hit", "description": "", "status": "in_progress",
					"due_date": nil, "project": nil,
					"checklist_progress": map[string]any{"done": 1, "total": 3},
					"created_by":         nil,
					"last_actor":         nil,
					"matched_checklist_items": []any{
						map[string]any{"id": 11, "title": "needle item"},
					},
				},
			},
		})
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskSearch(context.Background(), &stdout, &stderr, "needle", fx.Opts); err != nil {
		t.Fatalf("runTaskSearch: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	tasks, ok := out["tasks"].([]any)
	if !ok || len(tasks) != 2 {
		t.Fatalf("tasks shape = %+v", out["tasks"])
	}
	// Title-only row must carry an empty array (not be missing the key).
	first := tasks[0].(map[string]any)
	got, has := first["matched_checklist_items"]
	if !has {
		t.Fatalf("title-only task missing matched_checklist_items: %+v", first)
	}
	if arr, ok := got.([]any); !ok || len(arr) != 0 {
		t.Fatalf("title-only matched_checklist_items = %+v, want []", got)
	}
	// Items row must carry the matched item verbatim.
	second := tasks[1].(map[string]any)
	arr, _ := second["matched_checklist_items"].([]any)
	if len(arr) != 1 {
		t.Fatalf("items-row matched count = %d, want 1", len(arr))
	}
	hit := arr[0].(map[string]any)
	if hit["title"] != "needle item" {
		t.Fatalf("hit = %+v", hit)
	}
}

func TestRunTaskSearch_TextShowsMatchedItems(t *testing.T) {
	// Text mode interleaves an indented "matched checklist:" block,
	// one item per line, beneath any row whose checklist items hit
	// the query. Three tasks of mixed match types in non-trivial
	// order pin the per-task alignment — a regression that confused
	// the row→matches mapping would attach items to the wrong task.
	// One title also contains a comma to prove the renderer doesn't
	// rely on commas as a separator.
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tasks": []any{
				map[string]any{
					"id": 1, "title": "items-row-A", "description": "", "status": "in_progress",
					"due_date": nil, "project": nil,
					"checklist_progress": map[string]any{"done": 0, "total": 2},
					"created_by":         nil, "last_actor": nil,
					"matched_checklist_items": []any{
						map[string]any{"id": 11, "title": "first hit"},
					},
				},
				map[string]any{
					"id": 2, "title": "title-only-row", "description": "", "status": "done",
					"due_date": nil, "project": nil,
					"checklist_progress":      map[string]any{"done": 0, "total": 0},
					"created_by":              nil,
					"last_actor":              nil,
					"matched_checklist_items": []any{},
				},
				map[string]any{
					"id": 3, "title": "items-row-B", "description": "", "status": "in_progress",
					"due_date": nil, "project": nil,
					"checklist_progress": map[string]any{"done": 0, "total": 2},
					"created_by":         nil, "last_actor": nil,
					"matched_checklist_items": []any{
						map[string]any{"id": 21, "title": "second, with comma"},
						map[string]any{"id": 22, "title": "third hit"},
					},
				},
			},
		})
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskSearch(context.Background(), &stdout, &stderr, "x", fx.Opts); err != nil {
		t.Fatalf("runTaskSearch: %v", err)
	}
	out := stdout.String()
	lines := strings.Split(out, "\n")

	// Walk lines and assert per-task expectations: each task row is
	// followed (or not) by exactly the right matched block, in order.
	type expect struct {
		row     string
		matches []string // empty = no matched block expected after the row
	}
	want := []expect{
		{row: "items-row-A", matches: []string{"first hit"}},
		{row: "title-only-row"},
		{row: "items-row-B", matches: []string{"second, with comma", "third hit"}},
	}
	cursor := 0
	for _, exp := range want {
		for cursor < len(lines) && !strings.Contains(lines[cursor], exp.row) {
			cursor++
		}
		if cursor == len(lines) {
			t.Fatalf("row %q not found in:\n%s", exp.row, out)
		}
		cursor++
		if len(exp.matches) == 0 {
			if cursor < len(lines) && strings.Contains(lines[cursor], "matched checklist:") {
				t.Fatalf("row %q must not have a matched-checklist block:\n%s", exp.row, out)
			}
			continue
		}
		if cursor >= len(lines) || !strings.Contains(lines[cursor], "matched checklist:") {
			t.Fatalf("row %q expected matched-checklist header next, got %q:\n%s", exp.row, lines[cursor], out)
		}
		cursor++
		for _, m := range exp.matches {
			if cursor >= len(lines) || !strings.Contains(lines[cursor], "- "+m) {
				t.Fatalf("row %q expected matched item %q at line %d, got %q:\n%s", exp.row, m, cursor, lines[cursor], out)
			}
			cursor++
		}
	}
}

func TestTaskMap_OmitsMatchedWhenNil(t *testing.T) {
	// Sanity guard: a task decoded from a non-search endpoint (nil
	// MatchedChecklistItems) must not surface the key in rendered output.
	// Otherwise list/show envelopes would silently grow a field.
	task := &client.Task{ID: 1, Title: "x", Status: "done"}
	m := taskMap(task)
	if _, has := m["matched_checklist_items"]; has {
		t.Fatalf("taskMap leaked matched_checklist_items for nil slice: %+v", m)
	}
}

func TestTaskMap_KeepsMatchedEmpty(t *testing.T) {
	// The empty-but-non-nil case is the "matched on title" signal — must
	// reach the rendered output as `[]`, distinct from omission.
	task := &client.Task{
		ID: 1, Title: "x", Status: "done",
		MatchedChecklistItems: []client.MatchedChecklistItem{},
	}
	m := taskMap(task)
	got, has := m["matched_checklist_items"]
	if !has {
		t.Fatal("taskMap dropped non-nil empty MatchedChecklistItems")
	}
	if arr, ok := got.([]any); !ok || len(arr) != 0 {
		t.Fatalf("matched_checklist_items = %+v, want []any{}", got)
	}
}

// -- runTaskDue -------------------------------------------------------------

func TestRunTaskDue_SetsToday(t *testing.T) {
	var gotBody []byte
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/42/due_date" || r.Method != http.MethodPatch {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"id": 42, "title": "hello", "description": "", "status": "in_progress",
				"due_date": "2026-04-25", "project": nil,
				"checklist_progress": map[string]any{"done": 0, "total": 0},
				"created_by":         nil, "last_actor": nil,
			},
		})
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskDue(context.Background(), &stdout, &stderr, "42", "today", fx.Opts); err != nil {
		t.Fatalf("runTaskDue: %v", err)
	}
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode sent body: %v (%q)", err, gotBody)
	}
	if sent["due"] != "today" {
		t.Fatalf("sent due = %v, want %q", sent["due"], "today")
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	task, _ := out["task"].(map[string]any)
	if task == nil || task["due_date"] != "2026-04-25" {
		t.Fatalf("rendered task = %+v", out["task"])
	}
}

func TestRunTaskDue_ClearsWithNone(t *testing.T) {
	var gotBody []byte
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/42/due_date" || r.Method != http.MethodPatch {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"id": 42, "title": "hello", "description": "", "status": "in_progress",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 0, "total": 0},
				"created_by":         nil, "last_actor": nil,
			},
		})
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskDue(context.Background(), &stdout, &stderr, "42", "none", fx.Opts); err != nil {
		t.Fatalf("runTaskDue: %v", err)
	}
	// `none` must wire as a literal JSON null, with the key present — that's
	// the signal the server uses to distinguish "clear" from "missing field".
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode sent body: %v (%q)", err, gotBody)
	}
	if _, ok := sent["due"]; !ok {
		t.Fatalf("sent body missing `due` key: %s", gotBody)
	}
	if sent["due"] != nil {
		t.Fatalf("sent due = %v, want JSON null", sent["due"])
	}
}

func TestRunTaskDue_RejectsUnknownPreset(t *testing.T) {
	// Local validation must run before any HTTP call. The handler t.Errorf's
	// if reached so a regression that punts validation to the server fails.
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP call: %s %s", r.Method, r.URL.Path)
	})

	var stdout, stderr bytes.Buffer
	err := runTaskDue(context.Background(), &stdout, &stderr, "42", "tomorrow", fx.Opts)
	if err == nil {
		t.Fatal("expected an error for unknown preset")
	}
	if !strings.Contains(err.Error(), "yesterday|today|week|none") {
		t.Fatalf("error = %v, want it to list the four valid presets", err)
	}
	var out map[string]any
	if jerr := json.Unmarshal(stdout.Bytes(), &out); jerr != nil {
		t.Fatalf("not JSON: %v (%q)", jerr, stdout.String())
	}
	if out["status"] != "error" {
		t.Fatalf("payload = %+v, want status:error", out)
	}
}

// -- runTaskDelete ----------------------------------------------------------

func TestRunTaskDelete_Success204(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/42" || r.Method != http.MethodDelete {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskDelete(context.Background(), &stdout, &stderr, "42", fx.Opts); err != nil {
		t.Fatalf("runTaskDelete: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	if out["id"] != "42" || out["deleted"] != true || out["existed"] != true {
		t.Fatalf("payload = %+v, want {id:42, deleted:true, existed:true}", out)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty on success, got %q", stderr.String())
	}
}

func TestRunTaskDelete_404TreatedAsSuccess(t *testing.T) {
	// Idempotent UX: a second delete (or a delete on a never-existed id)
	// surfaces as success so retries are no-ops, but `existed:false` lets
	// callers tell a real delete from a typo or a second-run.
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskDelete(context.Background(), &stdout, &stderr, "999", fx.Opts); err != nil {
		t.Fatalf("runTaskDelete (404): %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	if out["id"] != "999" || out["deleted"] != true || out["existed"] != false {
		t.Fatalf("payload = %+v, want {id:999, deleted:true, existed:false}", out)
	}
}

func TestRunTaskDelete_404TextFallback(t *testing.T) {
	// The text fallback distinguishes 404 from 204 too — humans skimming
	// the line shouldn't get a misleading "Deleted ..." for a no-op.
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskDelete(context.Background(), &stdout, &stderr, "999", fx.Opts); err != nil {
		t.Fatalf("runTaskDelete (404): %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "Task #999 already gone") {
		t.Fatalf("stdout = %q, want it to mention 'already gone'", got)
	}
	if strings.Contains(got, "Deleted task #999") {
		t.Fatalf("stdout = %q, should NOT claim a delete happened on 404", got)
	}
}

func TestRunTaskDelete_403Forbidden(t *testing.T) {
	// Other 4xx still surface as errors. With --format=json the error
	// envelope lands on stdout for agents.
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	})

	var stdout, stderr bytes.Buffer
	err := runTaskDelete(context.Background(), &stdout, &stderr, "5", fx.Opts)
	if err == nil {
		t.Fatal("expected error on 403")
	}
	var out map[string]any
	if jerr := json.Unmarshal(stdout.Bytes(), &out); jerr != nil {
		t.Fatalf("not JSON: %v (%q)", jerr, stdout.String())
	}
	if out["error"] != "forbidden" || out["status"] != "error" {
		t.Fatalf("payload = %+v", out)
	}
	if v, ok := out["http_status"].(float64); !ok || int(v) != 403 {
		t.Fatalf("http_status = %v", out["http_status"])
	}
}

func TestRunTaskDelete_TextFallback(t *testing.T) {
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	var stdout, stderr bytes.Buffer
	if err := runTaskDelete(context.Background(), &stdout, &stderr, "42", fx.Opts); err != nil {
		t.Fatalf("runTaskDelete: %v", err)
	}
	if !strings.Contains(stdout.String(), "Deleted task #42") {
		t.Fatalf("stdout = %q, want it to contain 'Deleted task #42'", stdout.String())
	}
}

// -- runTaskCreate ----------------------------------------------------------

func TestRunTaskCreate_WiresEnvelope(t *testing.T) {
	var gotBody []byte
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"id": 17, "title": "hello", "description": "", "status": "not_started",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 0, "total": 0},
				"created_by":         nil, "last_actor": nil,
			},
		})
	})

	var stdout, stderr bytes.Buffer
	attrs := map[string]any{"title": "hello"}
	if err := runTaskCreate(context.Background(), &stdout, &stderr, attrs, fx.Opts); err != nil {
		t.Fatalf("runTaskCreate: %v", err)
	}
	var sent struct {
		Task map[string]any `json:"task"`
	}
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.Task["title"] != "hello" {
		t.Fatalf("sent title = %v, want hello", sent.Task["title"])
	}
}
