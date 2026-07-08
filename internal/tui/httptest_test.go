package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// TestModel_WithRealClientOverHTTPTest drives the model through a real
// *client.Client wired to an httptest server (the pattern from
// internal/client/testhelper_test.go), proving the concrete client satisfies
// the injected Client interface end-to-end, including auth-header propagation
// and the 401→secret-prompt path.
func TestModel_WithRealClientOverHTTPTest(t *testing.T) {
	var gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Agent-Secret")
		if gotSecret != "good" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tasks":[{"id":42,"title":"From server","checklist_progress":{"done":0,"total":0}}]}`))
	}))
	defer srv.Close()
	t.Setenv("SPRAWL_API_URL", srv.URL)

	newClient := func(secret string) Client { return client.NewAuthed("tok", secret) }

	// Wrong secret from env → validation fetch → 401 → secret prompt.
	m := newModel(context.Background(), Deps{
		LoggedIn: true, Secret: "bad", SecretProvided: true, NewClient: newClient,
	})
	m.width, m.height = 80, 24
	for _, msg := range runCmd(m.Init()) {
		m.send(msg)
	}
	if m.current() != screenSecret {
		t.Fatalf("a rejected env secret should drop to the prompt, got screen %d", m.current())
	}

	// Type the correct secret and submit → validates → list screen with the task.
	for _, k := range []string{"g", "o", "o", "d"} {
		m.press(k)
	}
	cmd := m.press("enter")
	for _, msg := range runCmd(cmd) {
		m.send(msg)
	}
	if m.current() != screenList {
		t.Fatalf("correct secret should validate to the list, got screen %d", m.current())
	}
	if len(m.tasks) != 1 || m.tasks[0].ID != 42 {
		t.Fatalf("list not populated from server: %+v", m.tasks)
	}
	if gotSecret != "good" {
		t.Fatalf("server saw secret %q, want good", gotSecret)
	}
}
