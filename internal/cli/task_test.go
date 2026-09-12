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
	// Keep the fixture hermetic: a project key exported in the developer's
	// shell must not leak into tests and change which headers go out.
	t.Setenv("SPRAWL_PROJECT_KEY", "")
	t.Setenv("SPRAWL_WORKSPACE", "")

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

func TestTaskShowText_HeaderIsTitleAndDescription(t *testing.T) {
	task := &client.Task{
		ID: 17, Title: "hello", Status: "done", Description: "body copy",
		ChecklistProgress: client.ChecklistProgress{Done: 2, Total: 3},
		Project:           &client.Project{ID: 1, Name: "P"},
		CreatedBy:         &client.Actor{Type: "user", ID: 5},
		ChecklistItems:    []*client.ChecklistItem{{ID: 5, Title: "a step"}},
	}
	got := taskShowText(task, false)
	for _, want := range []string{"hello", "body copy", "TITLE", "a step"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The bordered card is gone: no box, no meta grid, no project / due /
	// progress. task list carries all three, and --format=json carries them here.
	for _, unwanted := range []string{"╭─ ", "#17", "progress", "2/3", "user#5"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("card leftover %q in:\n%s", unwanted, got)
		}
	}
}

func TestTaskShowText_TitleAloneWhenNoDescription(t *testing.T) {
	task := &client.Task{ID: 1, Title: "x", Status: "done",
		ChecklistItems: []*client.ChecklistItem{{ID: 5, Title: "a step"}}}
	got := taskShowText(task, false)
	// The title, then the blank line renderTable's leading newline pairs with,
	// then the table. Never two blank lines from an absent description.
	if !strings.HasPrefix(got, "x\n\n") {
		t.Fatalf("header should be the title alone:\n%q", got)
	}
	if strings.HasPrefix(got, "x\n\n\n") {
		t.Fatalf("blank description left a trailing blank block:\n%q", got)
	}
}

// A task whose checklist is empty says so rather than rendering a headerless
// table with no rows.
func TestTaskShowText_EmptyChecklist(t *testing.T) {
	got := taskShowText(&client.Task{ID: 1, Title: "x", ChecklistItems: []*client.ChecklistItem{}}, false)
	if !strings.Contains(got, "(no items)") {
		t.Fatalf("empty checklist = %q", got)
	}
}

// --full gates the note bodies, not the table: the NOTES column is there either
// way, because "is there a note?" is the question it answers.
func TestTaskShowText_FullGatesNoteBodiesNotTheColumn(t *testing.T) {
	note := "the note body"
	task := &client.Task{
		ID: 1, Title: "x",
		ChecklistItems: []*client.ChecklistItem{
			{ID: 5, Title: "a step", HasNotes: true, Notes: &note},
		},
	}
	brief := taskShowText(task, false)
	if !strings.Contains(brief, "NOTES") || !strings.Contains(brief, "o") {
		t.Errorf("non-full view must still flag the note:\n%s", brief)
	}
	if strings.Contains(brief, note) {
		t.Errorf("non-full view must not expand the note:\n%s", brief)
	}
	if full := taskShowText(task, true); !strings.Contains(full, "note: "+note) {
		t.Errorf("--full should expand the note:\n%s", full)
	}
}

// TestTaskFullText_WrapsNotesWithHangingIndent locks in the note reflow: when
// outputWidth is known, a long note wraps to the remaining width and every
// continuation line is indented to where the note BODY starts, never exceeding
// the terminal width. Wrapping runs on plain text, so the styled render strips
// back to the plain one (the package invariant).
func TestTaskFullText_WrapsNotesWithHangingIndent(t *testing.T) {
	defer func() { stylesEnabled = false; outputWidth = 0 }()

	const width = 64
	longNote := "maybe we need some sort of approve state to distinguish between an agent finishing an item and a human approving it"
	task := &client.Task{
		ID: 295, Title: "Agentic", Status: "in_progress",
		ChecklistProgress: client.ChecklistProgress{Done: 0, Total: 1},
		ChecklistItems: []*client.ChecklistItem{
			{ID: 295, Title: "approve as agents mark items done", HasNotes: true, Notes: &longNote},
		},
	}

	outputWidth = width
	stylesEnabled = false
	plain := taskShowText(task, true)

	var noteLines int
	for _, ln := range strings.Split(plain, "\n") {
		if strings.TrimSpace(ln) == "" || !strings.HasPrefix(ln, " ") {
			continue
		}
		noteLines++
		if w := len([]rune(ln)); w > width {
			t.Fatalf("wrapped note line exceeds width %d (got %d): %q", width, w, ln)
		}
	}
	if noteLines < 2 {
		t.Fatalf("expected the long note to wrap onto multiple lines, got %d:\n%s", noteLines, plain)
	}

	stylesEnabled = true
	styled := taskShowText(task, true)
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
	// omitted, not "" — for a uniform "empty ⇒ null" contract.
	second := items[1].(map[string]any)
	if v, present := second["notes"]; !present || v != nil {
		t.Fatalf("second item notes = %+v (present=%v), want null", v, present)
	}
}

// The non-full read carries the items WITHOUT their bodies: has_notes says
// whether there is a note, and no `notes` key claims to know what it says.
// This is what makes `task <id>` cheap — reach for --full when you want the
// bodies.
func TestRunTaskShow_NonFullCarriesItemsWithoutNoteBodies(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("full"); q != "" {
			t.Errorf("full query should be absent, got %q", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task": map[string]any{
				"id": 42, "title": "hello", "description": "", "status": "done",
				"due_date": nil, "project": nil,
				"checklist_progress": map[string]any{"done": 0, "total": 1},
				"created_by":         nil, "last_actor": nil,
				"checklist_items": []any{
					map[string]any{
						"id": 5, "title": "a step", "completed": false, "position": 0,
						"state": nil, "pr_number": nil, "has_notes": true, "last_actor": nil,
					},
				},
			},
		})
	})
	var stdout, stderr bytes.Buffer
	if err := runTaskShow(context.Background(), &stdout, &stderr, "42", false, fx.Opts); err != nil {
		t.Fatalf("runTaskShow: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	items, ok := out["task"].(map[string]any)["checklist_items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("checklist_items = %+v", out["task"].(map[string]any)["checklist_items"])
	}
	first := items[0].(map[string]any)
	if first["has_notes"] != true {
		t.Errorf("has_notes must ride on the non-full read — it drives the NOTES column: %+v", first)
	}
	if _, present := first["notes"]; present {
		t.Errorf("notes must be absent when they weren't fetched: %+v", first)
	}
}

// A task list / search response has no checklist_items key at all (embedding
// every item per task would multiply the payload by every checklist's length),
// so the rendered envelope must not grow one.
func TestTaskMap_SuppressesAbsentChecklistItems(t *testing.T) {
	m := taskMap(&client.Task{ID: 1, Title: "x"}, false)
	if _, present := m["checklist_items"]; present {
		t.Fatalf("checklist_items must stay absent when the wire had none: %+v", m)
	}
	// An empty checklist is [] on the wire, and that is NOT the same thing —
	// it means "this task has no items", which the renderer says out loud.
	m = taskMap(&client.Task{ID: 1, Title: "x", ChecklistItems: []*client.ChecklistItem{}}, false)
	items, present := m["checklist_items"]
	if !present {
		t.Fatalf("an empty checklist must survive as []: %+v", m)
	}
	if len(items.([]any)) != 0 {
		t.Fatalf("checklist_items = %+v", items)
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
	// The --full text view is the title header plus the shared item table, with
	// each note expanded under its row.
	for _, want := range []string{
		"ship it",                     // header: the title, no id, no card
		"[x]", "ID", "NOTES", "TITLE", // the shared table
		"5", "done step", "note: did it", // completed item + its expanded note
		"6", "todo step", // open item
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("text output missing %q:\n%s", want, got)
		}
	}
	// An item with no note gets nothing at all — the "(no notes)" placeholder is
	// gone from the CLI entirely.
	if strings.Contains(got, "no notes") {
		t.Fatalf("the (no notes) placeholder is back:\n%s", got)
	}
	// And the card is gone with it.
	for _, unwanted := range []string{"╭─ ", "#42", "CHECKLIST", "☑", "☐"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("card leftover %q in:\n%s", unwanted, got)
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

	// Each hit renders like `task <id>`: the title header, then a table of the
	// items that matched — not a list row with an indented sub-block.
	for _, want := range []string{
		"items-row-A", "first hit",
		"title-only-row",
		"items-row-B", "second, with comma", "third hit",
		"ID", "TITLE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The old list shape is gone: no DUE / PROGRESS / PROJECT columns, and no
	// "matched checklist:" bullet block.
	for _, unwanted := range []string{"matched checklist:", "PROGRESS", "DUE", "- first hit"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("old search rendering leftover %q in:\n%s", unwanted, out)
		}
	}
	// A title-only match (matched_checklist_items == []) gets the header alone —
	// the title already said why it's here. So it must not be followed by a table.
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		if !strings.Contains(ln, "title-only-row") {
			continue
		}
		for _, after := range lines[i+1:] {
			if strings.TrimSpace(after) == "" {
				continue
			}
			if strings.Contains(after, "ID") && strings.Contains(after, "TITLE") {
				t.Errorf("a title-only match must not render an items table:\n%s", out)
			}
			break
		}
	}
}

// Search returns only {id, title} per matched item, so that is exactly what the
// block shows. Printing an unchecked box or an empty STATE for a hit whose real
// state the payload never carried would read as data rather than as absence —
// search is a lookup, and `item <id>` is how you read one.
func TestTaskSearchText_ShowsOnlyIDAndTitle(t *testing.T) {
	got := taskSearchText([]*client.Task{{
		ID: 1, Title: "a task",
		Project:               &client.Project{ID: 42, Name: "p", GithubURL: "https://github.com/o/r"},
		MatchedChecklistItems: []client.MatchedChecklistItem{{ID: 11, Title: "a hit"}},
	}})
	for _, want := range []string{"a task", "ID", "TITLE", "11", "a hit"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"[x]", "[ ]", "STATE", "PR", "NOTES"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("search must not render %q — the payload carries no such field:\n%s", unwanted, got)
		}
	}
}

// Several hits stack as several blocks, each with its own title and its own
// matched items, so a broad query stays readable.
func TestTaskSearchText_StacksBlocksPerTask(t *testing.T) {
	got := taskSearchText([]*client.Task{
		{ID: 1, Title: "first task", MatchedChecklistItems: []client.MatchedChecklistItem{{ID: 11, Title: "hit one"}}},
		{ID: 2, Title: "second task", MatchedChecklistItems: []client.MatchedChecklistItem{{ID: 22, Title: "hit two"}}},
	})
	first, second := strings.Index(got, "first task"), strings.Index(got, "second task")
	h1, h2 := strings.Index(got, "hit one"), strings.Index(got, "hit two")
	if first < 0 || second < 0 || h1 < 0 || h2 < 0 {
		t.Fatalf("missing a task or a hit in:\n%s", got)
	}
	// Each task's items sit under that task, not pooled at the end.
	if !(first < h1 && h1 < second && second < h2) {
		t.Errorf("blocks are interleaved wrongly:\n%s", got)
	}
}

func TestTaskSearchText_Empty(t *testing.T) {
	if got := taskSearchText(nil); got != "(no tasks)" {
		t.Fatalf("empty search = %q", got)
	}
}

func TestTaskMap_OmitsMatchedWhenNil(t *testing.T) {
	// Sanity guard: a task decoded from a non-search endpoint (nil
	// MatchedChecklistItems) must not surface the key in rendered output.
	// Otherwise list/show envelopes would silently grow a field.
	task := &client.Task{ID: 1, Title: "x", Status: "done"}
	m := taskMap(task, false)
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
	m := taskMap(task, false)
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
