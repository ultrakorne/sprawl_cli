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
		NewClient:  func(string, string, string) Client { return fc },
	})
	m.width, m.height = 100, 30
	return m
}

// A project key is a narrowing factor on its own, so the credentials prompt
// has nothing to ask for: go straight to the (server-filtered) list.
func TestProjectKey_SkipsCredsPrompt(t *testing.T) {
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
func TestNoProjectKey_StillPromptsForCreds(t *testing.T) {
	m := projectKeyModel(&fakeClient{}, "")
	if m.current() != screenCreds {
		t.Fatalf("current screen = %v, want screenCreds", m.current())
	}
}

// A typo'd key under project-key-only mode is fixable at the prompt now that it
// takes either factor: bounce there with the bad key pre-filled and focused.
func TestProjectKey_AuthFailureReturnsToPromptWithKey(t *testing.T) {
	fc := &fakeClient{listErr: &client.APIError{Status: 403, Code: "invalid_project_key"}}
	m := projectKeyModel(fc, "acmee")
	for _, msg := range runCmd(m.initCmd) {
		m.send(msg)
	}
	if m.current() != screenCreds {
		t.Fatalf("current screen = %v, want screenCreds", m.current())
	}
	if m.credFocus != credProjectKey {
		t.Fatal("the project key is the only factor in play — it should hold focus")
	}
	if got := m.keyInput.String(); got != "acmee" {
		t.Fatalf("key field = %q, want the rejected key pre-filled for editing", got)
	}
	if m.credErr == "" {
		t.Fatal("expected an inline error on the prompt")
	}
	if m.loading {
		t.Fatal("loading should be cleared after the failure")
	}

	// Correcting the key re-validates with it — and only it.
	m.press("backspace")
	m.press("enter")
	if m.projectKey != "acme" || m.secret != "" {
		t.Fatalf("submit should send the corrected key alone: key=%q secret=%q", m.projectKey, m.secret)
	}
	if !m.credBusy {
		t.Fatal("submit should set credBusy")
	}
}

// The prompt takes either factor: tab moves to the project-key field and a key
// alone is a valid submission (no secret required).
func TestCredsPrompt_ProjectKeyOnly(t *testing.T) {
	fc := &fakeClient{}
	m := projectKeyModel(fc, "")
	if m.credFocus != credSecret {
		t.Fatal("a cold start should focus the agent secret field")
	}
	m.press("tab")
	if m.credFocus != credProjectKey {
		t.Fatal("tab should move focus to the project key field")
	}
	for _, k := range []string{"a", "c", "m", "e"} {
		m.press(k)
	}
	if m.secretInput.String() != "" {
		t.Fatalf("typing after tab must not land in the secret field: %q", m.secretInput.String())
	}
	m.press("enter")
	if m.projectKey != "acme" || m.secret != "" {
		t.Fatalf("key-only submit: key=%q secret=%q", m.projectKey, m.secret)
	}
	if !m.credBusy || m.client == nil {
		t.Fatal("submit should build the client and mark the prompt busy")
	}
	// Both fields are shown in the clear so a wrong value can be spotted.
	got := ansi.Strip(m.viewCreds())
	if !strings.Contains(got, "acme") {
		t.Fatalf("prompt should echo the typed key:\n%s", got)
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
		NewClient:      func(string, string, string) Client { return fc },
	})
	m.width, m.height = 100, 30
	for _, msg := range runCmd(m.initCmd) {
		m.send(msg)
	}
	if m.current() != screenCreds {
		t.Fatalf("current screen = %v, want screenCreds", m.current())
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
		NewClient: func(string, string, string) Client { return &fakeClient{} },
	})
	m.width, m.height = 100, 30
	got := ansi.Strip(m.viewList())
	if !strings.Contains(got, "sprawl · tasks") || strings.Contains(got, "tasks · ") {
		t.Fatalf("unconfined header changed:\n%s", got)
	}
}
