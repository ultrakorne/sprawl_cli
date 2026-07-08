package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func secretModel(fc *fakeClient) *Model {
	m := newModel(context.Background(), Deps{
		LoggedIn:  true,
		NewClient: func(string) Client { return fc },
	})
	m.width, m.height = 100, 30
	return m // starts on screenSecret (no secret provided)
}

// #1 — a non-auth validation error must re-enable the prompt, not soft-lock it.
func TestSecretPrompt_NonAuthErrorReenablesPrompt(t *testing.T) {
	fc := &fakeClient{listErr: &client.APIError{Status: 500, Body: "boom"}}
	m := secretModel(fc)
	m.secretInput.setValue("mysecret")

	cmd := m.press("enter")
	if !m.secretBusy {
		t.Fatal("submitting should set secretBusy")
	}
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if m.secretBusy {
		t.Fatal("a non-auth validation error must reset secretBusy (soft-lock bug)")
	}
	if m.current() != screenSecret {
		t.Fatalf("should stay on the secret screen, got %v", m.current())
	}
	if m.secretErr == "" {
		t.Fatal("expected an error message on the prompt")
	}
	// Prompt is live again: a keypress edits the field instead of being dead.
	m.press("z")
	if !strings.HasSuffix(m.secretInput.String(), "z") {
		t.Fatalf("prompt should accept input after the error, got %q", m.secretInput.String())
	}
}

// #5 — bracketed paste routes into the active field (incl. the masked prompt).
func TestPaste_RoutesToActiveField(t *testing.T) {
	fc := &fakeClient{}
	m := secretModel(fc)
	m.send(tea.PasteMsg{Content: "secret-from-manager\n"}) // trailing newline dropped
	if got := m.secretInput.String(); got != "secret-from-manager" {
		t.Fatalf("paste into secret prompt = %q, want %q", got, "secret-from-manager")
	}

	lm := newListModel(&fakeClient{}, []*client.Task{{ID: 1, Title: "A"}})
	lm.press("/")
	lm.send(tea.PasteMsg{Content: "query"})
	if got := lm.searchInput.String(); got != "query" {
		t.Fatalf("paste into search = %q, want %q", got, "query")
	}
}

// #3 — secret-auth classification: 401 and secret-coded 403 bounce; 403
// forbidden / 500 stay footer errors.
func TestIsSecretAuthErr(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&client.APIError{Status: 401}, true},
		{&client.APIError{Status: 403, Code: "invalid_agent_secret"}, true},
		{&client.APIError{Status: 403, Code: "agent_key_revoked"}, true},
		{&client.APIError{Status: 403, Code: "agent_secret_required"}, true},
		{&client.APIError{Status: 403, Code: "forbidden"}, false},
		{&client.APIError{Status: 500}, false},
		{context.Canceled, false},
	}
	for _, c := range cases {
		if got := isSecretAuthErr(c.err); got != c.want {
			t.Fatalf("isSecretAuthErr(%v) = %v, want %v", c.err, got, c.want)
		}
	}
	if _, ok := opErrMsg(&client.APIError{Status: 401}, "x").(authFailedMsg); !ok {
		t.Fatal("opErrMsg(401) should be authFailedMsg")
	}
	if _, ok := opErrMsg(&client.APIError{Status: 403, Code: "forbidden"}, "x").(errMsg); !ok {
		t.Fatal("opErrMsg(403 forbidden) should stay errMsg")
	}
}

// #3 — a toggle failing on a revoked secret bounces to the prompt.
func TestToggle_SecretAuthErrorBouncesToPrompt(t *testing.T) {
	fc, m, _ := checklistFixture()
	fc.toggleErr = &client.APIError{Status: 403, Code: "invalid_agent_secret"}
	cmd := m.press("space")
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if m.current() != screenSecret {
		t.Fatalf("secret-auth toggle failure should bounce to the prompt, at %v", m.current())
	}
	if m.validated {
		t.Fatal("validated should be cleared after an auth bounce")
	}
}

// #2 — a stale out-of-order task fetch must not overwrite the open task.
func TestTaskLoaded_StaleResponseIgnored(t *testing.T) {
	m := newListModel(&fakeClient{}, []*client.Task{{ID: 1, Title: "A"}, {ID: 2, Title: "B"}})
	m.pendingTaskID = 2 // task 2 is the in-flight request
	stale := &client.Task{ID: 1, Title: "A-full"}
	m.send(taskLoadedMsg{task: stale, wantID: 1})
	if m.detail != nil {
		t.Fatal("stale response (wantID 1 != pending 2) must be discarded")
	}
	fresh := &client.Task{ID: 2, Title: "B-full"}
	m.send(taskLoadedMsg{task: fresh, wantID: 2})
	if m.detail != fresh {
		t.Fatal("matching response should populate detail")
	}
}

// #6 — a nil task body must not crash either the copy or the drill-in path.
func TestTaskLoaded_NilTaskDoesNotPanic(t *testing.T) {
	m := newListModel(&fakeClient{}, []*client.Task{{ID: 1, Title: "A"}})
	m.send(taskLoadedMsg{task: nil, forCopy: true}) // must not panic
	if !m.statusErr {
		t.Fatal("nil copy task should surface a footer error")
	}

	m.push(screenChecklist)
	m.detail = nil
	m.pendingTaskID = 5
	m.send(taskLoadedMsg{task: nil, wantID: 5}) // must not panic
	if m.current() != screenList {
		t.Fatalf("nil drill-in should pop back to the list, at %v", m.current())
	}
}

// #4 — note scroll offset is clamped to the real max, so scroll-up stays live
// after G / over-scrolling (no 1<<20 sentinel).
func TestNoteScroll_ClampedNotSentinel(t *testing.T) {
	_, m, detail := checklistFixture()
	detail.ChecklistItems[0].Notes = strptr(strings.Repeat("a line of note text\n", 60))
	m.push(screenNote)
	m.itemSel = 0

	maxOff := m.noteMaxOff()
	if maxOff == 0 {
		t.Fatal("test note should exceed one screen")
	}
	m.press("G")
	if m.noteOff != maxOff {
		t.Fatalf("G should pin to noteMaxOff (%d), got %d", maxOff, m.noteOff)
	}
	m.press("up")
	if m.noteOff != maxOff-1 {
		t.Fatalf("up after G should step back to %d, got %d (sentinel bug)", maxOff-1, m.noteOff)
	}
	// over-scroll down never exceeds max
	for i := 0; i < 100; i++ {
		m.press("down")
	}
	if m.noteOff != maxOff {
		t.Fatalf("down over-scroll should clamp at %d, got %d", maxOff, m.noteOff)
	}
}
