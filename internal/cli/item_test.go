package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// itemDetailBody is the GET /api/v1/checklist_items/:id payload: the item, its
// notes (always — there is no ?full variant on this route), and the parent-task
// stub carrying the project.
func itemDetailBody(notes any) map[string]any {
	return map[string]any{
		"checklist_item": map[string]any{
			"id": 203, "title": "add the migration", "completed": false,
			"position": 2, "state": "in_review", "pr_number": 412,
			"has_notes": notes != nil, "notes": notes,
			"last_actor": nil,
			"task": map[string]any{
				"id": 119, "title": "Ship the CLI consolidation",
				"project": map[string]any{
					"id": 42, "name": "sprawl_cli", "key": "sc", "color": "",
					"github_url": "https://github.com/ultrakorne/sprawl_cli",
				},
			},
		},
	}
}

// -- item <id> (the read) ---------------------------------------------------

func TestRunItemShow_JSONEnvelope(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/checklist_items/203" || r.Method != http.MethodGet {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(itemDetailBody("blocked on the backfill"))
	})

	var stdout, stderr bytes.Buffer
	if err := runItemShow(context.Background(), &stdout, &stderr, "203", fx.Opts); err != nil {
		t.Fatalf("runItemShow: %v", err)
	}
	var out struct {
		Item map[string]any `json:"checklist_item"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	// The detail view always carries notes; state is short-form; pr_url is
	// resolved through the task stub's project; position is gone.
	if out.Item["notes"] != "blocked on the backfill" {
		t.Errorf("notes = %v", out.Item["notes"])
	}
	if out.Item["state"] != "review" {
		t.Errorf("state = %v, want the short form %q", out.Item["state"], "review")
	}
	if out.Item["pr_url"] != "https://github.com/ultrakorne/sprawl_cli/pull/412" {
		t.Errorf("pr_url = %v", out.Item["pr_url"])
	}
	if _, present := out.Item["position"]; present {
		t.Errorf("position resurfaced: %+v", out.Item)
	}
	// The task stub rides in the machine payload even though -h doesn't show it:
	// it's the only thing tying an item id back to its task.
	task, ok := out.Item["task"].(map[string]any)
	if !ok || task["id"] != float64(119) {
		t.Fatalf("task stub = %+v", out.Item["task"])
	}
	if task["project"] == nil {
		t.Errorf("task stub must carry the project — it's what resolved pr_url")
	}
}

func TestRunItemShow_EmptyNotesIsNull(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(itemDetailBody(nil))
	})
	var stdout, stderr bytes.Buffer
	if err := runItemShow(context.Background(), &stdout, &stderr, "203", fx.Opts); err != nil {
		t.Fatalf("runItemShow: %v", err)
	}
	var out struct {
		Item map[string]any `json:"checklist_item"`
	}
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if v, present := out.Item["notes"]; !present || v != nil {
		t.Errorf("notes = %v (present=%v), want a present null", v, present)
	}
}

func TestRunItemShow_TextRendersOneRowAndItsNote(t *testing.T) {
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(itemDetailBody("blocked on the backfill"))
	})
	var stdout, stderr bytes.Buffer
	if err := runItemShow(context.Background(), &stdout, &stderr, "203", fx.Opts); err != nil {
		t.Fatalf("runItemShow: %v", err)
	}
	got := stdout.String()
	for _, want := range []string{"ID", "STATE", "PR", "NOTES", "TITLE", "203", "#412", "o", "add the migration", "note: blocked on the backfill"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// No task header of ANY kind — you asked about an item, you get the item.
	if strings.Contains(got, "Ship the CLI consolidation") {
		t.Errorf("item <id> must not render parent-task context:\n%s", got)
	}
}

// An item outside a project key's project is 403 forbidden, NOT 404 — the CLI
// surfaces it as an error with the project-key-aware guidance rather than
// pretending the item doesn't exist.
func TestRunItemShow_OutsideProjectKeyIs403(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	})
	fx.Opts.projectKey = "per"

	var stdout, stderr bytes.Buffer
	if err := runItemShow(context.Background(), &stdout, &stderr, "4", fx.Opts); err == nil {
		t.Fatal("expected an error on 403")
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	if out["error"] != "forbidden" || out["http_status"] != float64(403) {
		t.Fatalf("payload = %+v", out)
	}
	hint, _ := out["hint"].(string)
	if !strings.Contains(hint, "per") {
		t.Errorf("hint should name the confining project key, got %q", hint)
	}
}

func TestRunItemShow_NotFound(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})
	var stdout, stderr bytes.Buffer
	if err := runItemShow(context.Background(), &stdout, &stderr, "99999", fx.Opts); err == nil {
		t.Fatal("expected an error on 404")
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if out["error"] != "not_found" {
		t.Fatalf("payload = %+v", out)
	}
}

// -- item add ---------------------------------------------------------------

func writeItemBody(notes any) map[string]any {
	m := map[string]any{
		"id": 206, "title": "step", "completed": false, "position": 2,
		"state": nil, "pr_number": nil, "has_notes": notes != nil, "last_actor": nil,
	}
	if notes != nil {
		m["notes"] = notes
	}
	return map[string]any{"checklist_item": m}
}

func TestRunItemAdd_PostsToTask(t *testing.T) {
	var gotBody []byte
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/tasks/119/checklist" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(writeItemBody(nil))
	})

	var stdout, stderr bytes.Buffer
	attrs := map[string]any{"title": "step"}
	if err := runItemAdd(context.Background(), &stdout, &stderr, "119", attrs, fx.Opts); err != nil {
		t.Fatalf("runItemAdd: %v", err)
	}
	var sent struct {
		Item map[string]any `json:"checklist_item"`
	}
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if sent.Item["title"] != "step" {
		t.Fatalf("title = %v", sent.Item["title"])
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if out["checklist_item"] == nil {
		t.Fatalf("response missing checklist_item envelope: %+v", out)
	}
}

func TestRunItemAdd_TextIsAOneLineSummary(t *testing.T) {
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(writeItemBody(nil))
	})
	var stdout, stderr bytes.Buffer
	if err := runItemAdd(context.Background(), &stdout, &stderr, "119", map[string]any{"title": "step"}, fx.Opts); err != nil {
		t.Fatalf("runItemAdd: %v", err)
	}
	got := strings.TrimSpace(stdout.String())
	if got != "✓ added item 206  step" {
		t.Errorf("summary = %q", got)
	}
	// No table, no card — a write says what it did and stops.
	if strings.Contains(got, "TITLE") || strings.Contains(got, "╭") {
		t.Errorf("write output should be one line, got:\n%s", got)
	}
}

// -- item update ------------------------------------------------------------

func TestRunItemUpdate_WrapsInEnvelopeAndEchoesNotes(t *testing.T) {
	var gotBody []byte
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/checklist_items/203" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(writeItemBody("the saved note"))
	})

	var stdout, stderr bytes.Buffer
	attrs := map[string]any{"notes": "the saved note"}
	if err := runItemUpdate(context.Background(), &stdout, &stderr, "203", attrs, fx.Opts); err != nil {
		t.Fatalf("runItemUpdate: %v", err)
	}
	// The server requires the checklist_item envelope and 422s an unwrapped
	// body, so the wrapping is load-bearing rather than cosmetic.
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode sent: %v", err)
	}
	inner, ok := sent["checklist_item"].(map[string]any)
	if !ok {
		t.Fatalf("body was not wrapped in a checklist_item envelope: %s", gotBody)
	}
	if inner["notes"] != "the saved note" {
		t.Errorf("sent notes = %v", inner["notes"])
	}
	// PATCH is the one write route that serialises the item in full, so its
	// response carries the saved note — that echo is what makes the write
	// verifiable instead of assumed.
	var out struct {
		Item map[string]any `json:"checklist_item"`
	}
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if out.Item["notes"] != "the saved note" {
		t.Errorf("response notes = %v, want the echo", out.Item["notes"])
	}
}

func TestItemWriteFlags_NotesDashReadsStdin(t *testing.T) {
	f := &itemWriteFlags{notes: "-", hasNotes: true}
	attrs, err := f.buildAttrs(strings.NewReader("piped body\nsecond line"))
	if err != nil {
		t.Fatalf("buildAttrs: %v", err)
	}
	if attrs["notes"] != "piped body\nsecond line" {
		t.Errorf("notes = %v, want the stdin body", attrs["notes"])
	}
}

func TestItemWriteFlags_EmptyNotesClears(t *testing.T) {
	// `--notes ""` is the documented way to clear a note, so an empty value must
	// still reach the wire — this is why hasNotes exists at all.
	f := &itemWriteFlags{notes: "", hasNotes: true}
	attrs, err := f.buildAttrs(strings.NewReader(""))
	if err != nil {
		t.Fatalf("buildAttrs: %v", err)
	}
	v, present := attrs["notes"]
	if !present || v != "" {
		t.Errorf("notes = %v (present=%v), want a present empty string", v, present)
	}
	// Without the flag, notes must not appear — an absent key means "leave it".
	f = &itemWriteFlags{notes: "", hasNotes: false}
	attrs, _ = f.buildAttrs(strings.NewReader(""))
	if _, present := attrs["notes"]; present {
		t.Errorf("notes leaked into the payload when the flag wasn't passed: %+v", attrs)
	}
}

func TestItemWriteFlags_NotesAndJSONCannotBothReadStdin(t *testing.T) {
	f := &itemWriteFlags{notes: "-", hasNotes: true, fromJSON: "-"}
	if _, err := f.buildAttrs(strings.NewReader(`{"title":"x"}`)); err == nil {
		t.Fatal("expected an error when both --notes - and --from-json - claim stdin")
	}
}

func TestItemWriteFlags_FlagsBeatFromJSON(t *testing.T) {
	f := &itemWriteFlags{title: "from flag", fromJSON: "-"}
	attrs, err := f.buildAttrs(strings.NewReader(`{"title":"from json","notes":"kept"}`))
	if err != nil {
		t.Fatalf("buildAttrs: %v", err)
	}
	if attrs["title"] != "from flag" {
		t.Errorf("title = %v, want the flag to win", attrs["title"])
	}
	if attrs["notes"] != "kept" {
		t.Errorf("notes = %v, want the JSON value preserved", attrs["notes"])
	}
}

// -- item check / uncheck ---------------------------------------------------

func TestRunItemSetCompleted_SendsBooleanAndSummarises(t *testing.T) {
	for _, tc := range []struct {
		completed bool
		want      string
	}{{true, "✓ checked item 206  step"}, {false, "✓ unchecked item 206  step"}} {
		var gotBody []byte
		fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/checklist_items/206/completed" {
				t.Errorf("path = %q", r.URL.Path)
			}
			gotBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(writeItemBody(nil))
		})

		var stdout, stderr bytes.Buffer
		if err := runItemSetCompleted(context.Background(), &stdout, &stderr, "206", tc.completed, fx.Opts); err != nil {
			t.Fatalf("runItemSetCompleted: %v", err)
		}
		var sent struct {
			Completed *bool `json:"completed"`
		}
		if err := json.Unmarshal(gotBody, &sent); err != nil {
			t.Fatalf("decode sent: %v", err)
		}
		if sent.Completed == nil || *sent.Completed != tc.completed {
			t.Fatalf("sent completed = %v, want %v", sent.Completed, tc.completed)
		}
		if got := strings.TrimSpace(stdout.String()); got != tc.want {
			t.Errorf("summary = %q, want %q", got, tc.want)
		}
	}
}

// -- item delete ------------------------------------------------------------

func TestRunItemDelete_Success204(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/checklist_items/206" || r.Method != http.MethodDelete {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	var stdout, stderr bytes.Buffer
	if err := runItemDelete(context.Background(), &stdout, &stderr, "206", fx.Opts); err != nil {
		t.Fatalf("runItemDelete: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	if out["id"] != "206" || out["deleted"] != true || out["existed"] != true {
		t.Fatalf("payload = %+v", out)
	}
}

func TestRunItemDelete_404TreatedAsSuccess(t *testing.T) {
	// Idempotent UX: a delete against an unknown id surfaces as success (so
	// retries are no-ops) but `existed:false` lets callers tell a real delete
	// from a typo or a second run.
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})

	var stdout, stderr bytes.Buffer
	if err := runItemDelete(context.Background(), &stdout, &stderr, "999", fx.Opts); err != nil {
		t.Fatalf("runItemDelete (404): %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if out["id"] != "999" || out["deleted"] != true || out["existed"] != false {
		t.Fatalf("payload = %+v", out)
	}
}

func TestRunItemDelete_404TextFallback(t *testing.T) {
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})

	var stdout, stderr bytes.Buffer
	if err := runItemDelete(context.Background(), &stdout, &stderr, "999", fx.Opts); err != nil {
		t.Fatalf("runItemDelete (404): %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "Item #999 already gone") {
		t.Fatalf("stdout = %q, want it to mention 'already gone'", got)
	}
	if strings.Contains(got, "Deleted item #999") {
		t.Fatalf("stdout = %q, should NOT claim a delete happened on 404", got)
	}
}

// A 403 is NOT idempotent success: the item exists, it's simply out of reach.
// Reporting "already gone" there would claim a delete that never happened.
func TestRunItemDelete_403Forbidden(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	})

	var stdout, stderr bytes.Buffer
	if err := runItemDelete(context.Background(), &stdout, &stderr, "5", fx.Opts); err == nil {
		t.Fatal("expected error on 403")
	}
	var out map[string]any
	if jerr := json.Unmarshal(stdout.Bytes(), &out); jerr != nil {
		t.Fatalf("not JSON: %v (%q)", jerr, stdout.String())
	}
	if out["error"] != "forbidden" || out["status"] != "error" {
		t.Fatalf("payload = %+v", out)
	}
}

func TestRunItemDelete_TextFallback(t *testing.T) {
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	var stdout, stderr bytes.Buffer
	if err := runItemDelete(context.Background(), &stdout, &stderr, "206", fx.Opts); err != nil {
		t.Fatalf("runItemDelete: %v", err)
	}
	if !strings.Contains(stdout.String(), "Deleted item #206") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

// -- shared render primitives -----------------------------------------------

func TestCheckboxAndNotesFlag(t *testing.T) {
	if checkbox(true) != "[x]" || checkbox(false) != "[ ]" {
		t.Fatal("checkbox glyphs drifted")
	}
	// A marker, not a word: the column answers "is there a note?" and its header
	// already says NOTES, so spelling it out just widens the column.
	if notesFlag(true) != "o" || notesFlag(false) != "-" {
		t.Fatal("notesFlag labels drifted")
	}
}
