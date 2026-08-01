package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// projectKeyModel builds a model confined to a project with no agent secret —
// the shape a repo that only exports SPRAWL_PROJECT_KEY produces.
func projectKeyModel(fc *fakeClient, key string) *Model {
	m := newModel(context.Background(), Deps{
		LoggedIn:   true,
		ProjectKey: key,
		NewClient:  func(string) Client { return fc },
	})
	m.width, m.height = 100, 30
	return m
}

// A project key is a narrowing factor on its own, so the masked secret prompt
// has nothing to ask for: go straight to the (server-filtered) list.
func TestProjectKey_SkipsSecretPrompt(t *testing.T) {
	m := projectKeyModel(&fakeClient{}, "acme")
	if m.current() != screenList {
		t.Fatalf("current screen = %v, want screenList", m.current())
	}
	if m.initCmd == nil {
		t.Fatal("expected the first list fetch to be queued")
	}
	if m.secret != "" {
		t.Fatalf("secret = %q, want empty", m.secret)
	}
}

// Without either factor the prompt is still the entry point.
func TestNoProjectKey_StillPromptsForSecret(t *testing.T) {
	m := projectKeyModel(&fakeClient{}, "")
	if m.current() != screenSecret {
		t.Fatalf("current screen = %v, want screenSecret", m.current())
	}
}

// A bad key / bad bearer under project-key-only mode must not drop the user
// into a masked prompt they can't use — surface the error where they are.
func TestProjectKey_AuthFailureStaysOnList(t *testing.T) {
	fc := &fakeClient{listErr: &client.APIError{Status: 403, Code: "invalid_project_key"}}
	m := projectKeyModel(fc, "acmee")
	for _, msg := range runCmd(m.initCmd) {
		m.send(msg)
	}
	if m.current() == screenSecret {
		t.Fatal("project-key-only mode must not bounce to the secret prompt")
	}
	if !m.statusErr || !strings.Contains(m.status, "invalid_project_key") {
		t.Fatalf("expected the failure on the status line, got %q", m.status)
	}
	if m.loading {
		t.Fatal("loading should be cleared after the failure")
	}
}

// With a secret actually in play, a rejected secret still bounces to the
// prompt so it can be re-entered — the project key doesn't change that.
func TestProjectKey_WithSecretStillBouncesToPrompt(t *testing.T) {
	fc := &fakeClient{listErr: &client.APIError{Status: 403, Code: "invalid_agent_secret"}}
	m := newModel(context.Background(), Deps{
		LoggedIn:       true,
		Secret:         "K7X2M9QA",
		SecretProvided: true,
		ProjectKey:     "acme",
		NewClient:      func(string) Client { return fc },
	})
	m.width, m.height = 100, 30
	for _, msg := range runCmd(m.initCmd) {
		m.send(msg)
	}
	if m.current() != screenSecret {
		t.Fatalf("current screen = %v, want screenSecret", m.current())
	}
}

// The list is server-filtered to one project, which is otherwise invisible —
// name it in the header so an empty list reads correctly.
func TestProjectKey_ShownInListHeader(t *testing.T) {
	m := projectKeyModel(&fakeClient{}, "acme")
	got := ansi.Strip(m.viewList())
	if !strings.Contains(got, "sprawl · tasks · acme") {
		t.Fatalf("header should name the confined project:\n%s", got)
	}
}

func TestNoProjectKey_HeaderUnchanged(t *testing.T) {
	m := newModel(context.Background(), Deps{
		LoggedIn: true, Secret: "sek", SecretProvided: true,
		NewClient: func(string) Client { return &fakeClient{} },
	})
	m.width, m.height = 100, 30
	got := ansi.Strip(m.viewList())
	if !strings.Contains(got, "sprawl · tasks") || strings.Contains(got, "tasks · ") {
		t.Fatalf("unconfined header changed:\n%s", got)
	}
}
