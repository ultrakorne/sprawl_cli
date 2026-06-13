package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// -- pure text helpers ------------------------------------------------------

func TestChecklistItemsText_Empty(t *testing.T) {
	if got := checklistItemsText(nil); got != "(no checklist items)" {
		t.Fatalf("empty = %q", got)
	}
}

func TestChecklistItemsText_FormatsRows(t *testing.T) {
	items := []*client.ChecklistItem{
		{ID: 5, Title: "a", Position: 0, Completed: false, HasNotes: false},
		{ID: 6, Title: "b", Position: 1, Completed: true, HasNotes: true},
	}
	got := checklistItemsText(items)
	for _, want := range []string{"ID", "POS", "NOTES", "[ ]", "[x]", "notes", "a", "b"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestChecklistItemLine(t *testing.T) {
	done := &client.ChecklistItem{ID: 5, Title: "step", Completed: true}
	if got := checklistItemLine(done); got != "[x] #5 step" {
		t.Fatalf("completed line = %q", got)
	}
	pending := &client.ChecklistItem{ID: 9, Title: "next", Completed: false}
	if got := checklistItemLine(pending); got != "[ ] #9 next" {
		t.Fatalf("pending line = %q", got)
	}
}

func TestCheckboxAndNotesFlag(t *testing.T) {
	if checkbox(true) != "[x]" || checkbox(false) != "[ ]" {
		t.Fatal("checkbox glyphs drifted")
	}
	if notesFlag(true) != "notes" || notesFlag(false) != "-" {
		t.Fatal("notesFlag labels drifted")
	}
}

// -- runChecklist (list) ----------------------------------------------------

func TestRunChecklist_JSONEnvelope(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/4/checklist" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"checklist_items": []any{
				map[string]any{
					"id": 5, "title": "step", "completed": false, "position": 0,
					"has_notes": false, "last_actor": nil,
				},
			},
		})
	})
	var stdout, stderr bytes.Buffer
	if err := runChecklist(context.Background(), &stdout, &stderr, "4", false, fx.Opts); err != nil {
		t.Fatalf("runChecklist: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	items, ok := out["checklist_items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("checklist_items = %+v", out["checklist_items"])
	}
}

func TestRunChecklist_FullSendsParamAndEmitsNotes(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/4/checklist" || r.URL.Query().Get("full") != "true" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"checklist_items": []any{
				map[string]any{
					"id": 5, "title": "step", "completed": false, "position": 0,
					"has_notes": true, "notes": "the note body", "last_actor": nil,
				},
				map[string]any{
					"id": 6, "title": "empty", "completed": false, "position": 1,
					"has_notes": false, "notes": nil, "last_actor": nil,
				},
			},
		})
	})
	var stdout, stderr bytes.Buffer
	if err := runChecklist(context.Background(), &stdout, &stderr, "4", true, fx.Opts); err != nil {
		t.Fatalf("runChecklist: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	items := out["checklist_items"].([]any)
	item := items[0].(map[string]any)
	if item["notes"] != "the note body" {
		t.Fatalf("notes = %+v", item["notes"])
	}
	// Empty notes on the full path emit a present-but-null key (not omitted, not
	// ""), mirroring the server's null and `note show`.
	empty := items[1].(map[string]any)
	if v, present := empty["notes"]; !present || v != nil {
		t.Fatalf("empty notes = %+v (present=%v), want null", v, present)
	}
}

func TestRunChecklist_FullTextRendersNotesBlock(t *testing.T) {
	// Text mode swaps the table for a per-item block; notes (incl. the
	// empty-notes "(no notes)" case) render under each item.
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"checklist_items": []any{
				map[string]any{
					"id": 5, "title": "with notes", "completed": true, "position": 0,
					"has_notes": true, "notes": "line one\nline two", "last_actor": nil,
				},
				map[string]any{
					"id": 6, "title": "no notes", "completed": false, "position": 1,
					"has_notes": false, "notes": nil, "last_actor": nil,
				},
			},
		})
	})
	var stdout, stderr bytes.Buffer
	if err := runChecklist(context.Background(), &stdout, &stderr, "4", true, fx.Opts); err != nil {
		t.Fatalf("runChecklist: %v", err)
	}
	got := stdout.String()
	for _, want := range []string{"[x] #5 with notes", "notes: line one", "line two", "[ ] #6 no notes", "(no notes)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("text output missing %q:\n%s", want, got)
		}
	}
}

// -- runChecklistAdd --------------------------------------------------------

func TestRunChecklistAdd_PostsToTask(t *testing.T) {
	var gotBody []byte
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/tasks/4/checklist" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"checklist_item": map[string]any{
				"id": 8, "title": "step", "completed": false, "position": 2,
				"has_notes": false, "last_actor": nil,
			},
		})
	})

	var stdout, stderr bytes.Buffer
	attrs := map[string]any{"title": "step"}
	if err := runChecklistAdd(context.Background(), &stdout, &stderr, "4", attrs, fx.Opts); err != nil {
		t.Fatalf("runChecklistAdd: %v", err)
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
	// Response should land under the checklist_item envelope on stdout.
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if out["checklist_item"] == nil {
		t.Fatalf("response missing checklist_item envelope: %+v", out)
	}
}

// -- runChecklistSetCompleted -----------------------------------------------

func TestRunChecklistSetCompleted_SendsBoolean(t *testing.T) {
	var gotBody []byte
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/checklist_items/8/completed" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"checklist_item": map[string]any{
				"id": 8, "title": "x", "completed": true, "position": 0,
				"has_notes": false, "last_actor": nil,
			},
		})
	})

	var stdout, stderr bytes.Buffer
	if err := runChecklistSetCompleted(context.Background(), &stdout, &stderr, "8", true, fx.Opts); err != nil {
		t.Fatalf("runChecklistSetCompleted: %v", err)
	}
	var sent struct {
		Completed *bool `json:"completed"`
	}
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode sent: %v", err)
	}
	if sent.Completed == nil || !*sent.Completed {
		t.Fatalf("expected completed=true, got %v", sent.Completed)
	}
}

// -- runChecklistDelete -----------------------------------------------------

func TestRunChecklistDelete_Success204(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/checklist_items/8" || r.Method != http.MethodDelete {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	var stdout, stderr bytes.Buffer
	if err := runChecklistDelete(context.Background(), &stdout, &stderr, "8", fx.Opts); err != nil {
		t.Fatalf("runChecklistDelete: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	if out["id"] != "8" || out["deleted"] != true || out["existed"] != true {
		t.Fatalf("payload = %+v, want {id:8, deleted:true, existed:true}", out)
	}
}

func TestRunChecklistDelete_404TreatedAsSuccess(t *testing.T) {
	// Idempotent UX: a delete against an unknown id surfaces as success
	// (so retries are no-ops) but `existed:false` lets callers tell a real
	// delete from a typo or a second-run.
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})

	var stdout, stderr bytes.Buffer
	if err := runChecklistDelete(context.Background(), &stdout, &stderr, "999", fx.Opts); err != nil {
		t.Fatalf("runChecklistDelete (404): %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	if out["id"] != "999" || out["deleted"] != true || out["existed"] != false {
		t.Fatalf("payload = %+v, want {id:999, deleted:true, existed:false}", out)
	}
}

func TestRunChecklistDelete_404TextFallback(t *testing.T) {
	// The text fallback distinguishes 404 from 204 too — agents reading
	// the stream-of-bytes view shouldn't get the same line for both.
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	})

	var stdout, stderr bytes.Buffer
	if err := runChecklistDelete(context.Background(), &stdout, &stderr, "999", fx.Opts); err != nil {
		t.Fatalf("runChecklistDelete (404): %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "Checklist item #999 already gone") {
		t.Fatalf("stdout = %q, want it to mention 'already gone'", got)
	}
	if strings.Contains(got, "Deleted checklist item #999") {
		t.Fatalf("stdout = %q, should NOT claim a delete happened on 404", got)
	}
}

func TestRunChecklistDelete_403Forbidden(t *testing.T) {
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	})

	var stdout, stderr bytes.Buffer
	err := runChecklistDelete(context.Background(), &stdout, &stderr, "5", fx.Opts)
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
}

func TestRunChecklistDelete_TextFallback(t *testing.T) {
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	var stdout, stderr bytes.Buffer
	if err := runChecklistDelete(context.Background(), &stdout, &stderr, "8", fx.Opts); err != nil {
		t.Fatalf("runChecklistDelete: %v", err)
	}
	if !strings.Contains(stdout.String(), "Deleted checklist item #8") {
		t.Fatalf("stdout = %q, want it to contain 'Deleted checklist item #8'", stdout.String())
	}
}
