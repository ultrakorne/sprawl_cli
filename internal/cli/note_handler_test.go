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

func TestRunNoteSet_RoundTrip(t *testing.T) {
	var gotBody []byte
	fx := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/checklist_items/9/notes" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"notes": "line one\nline two"})
	})

	var stdout, stderr bytes.Buffer
	if err := runNoteSet(context.Background(), &stdout, &stderr, "9", "line one\nline two", fx.Opts); err != nil {
		t.Fatalf("runNoteSet: %v", err)
	}
	var sent struct {
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode sent: %v", err)
	}
	if sent.Notes != "line one\nline two" {
		t.Fatalf("sent.notes = %q", sent.Notes)
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if out["notes"] != "line one\nline two" {
		t.Fatalf("stdout.notes = %v", out["notes"])
	}
}

// TestRunNoteSet_TextFallbackForEmpty guards the "(notes cleared)" message
// when the saved blob is empty — in text mode we want a human-readable hint
// rather than an empty line masquerading as a successful output.
func TestRunNoteSet_TextFallbackForEmpty(t *testing.T) {
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"notes": ""})
	})
	var stdout, stderr bytes.Buffer
	if err := runNoteSet(context.Background(), &stdout, &stderr, "9", "", fx.Opts); err != nil {
		t.Fatalf("runNoteSet: %v", err)
	}
	if !strings.Contains(stdout.String(), "(notes cleared)") {
		t.Fatalf("stdout = %q, want (notes cleared) fallback", stdout.String())
	}
}

func TestRunNoteShow_TextFallbackIsRawBody(t *testing.T) {
	// Text mode is the "| less" use case — notes body should be emitted verbatim
	// without wrapper decoration.
	fx := newAuthedFixture(t, "text", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"notes": "hello\nthere"})
	})
	var stdout, stderr bytes.Buffer
	if err := runNoteShow(context.Background(), &stdout, &stderr, "9", fx.Opts); err != nil {
		t.Fatalf("runNoteShow: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "hello\nthere" {
		t.Fatalf("stdout = %q, want raw notes body", stdout.String())
	}
}
