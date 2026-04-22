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
	if err := runChecklist(context.Background(), &stdout, &stderr, "4", fx.Opts); err != nil {
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
