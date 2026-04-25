package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// -- pure text-format helpers ----------------------------------------------

func TestWhoamiText_OwnerNoProjects(t *testing.T) {
	got := whoamiText(&client.Whoami{
		Agent: client.Agent{
			ID: 1, Name: "owner", Emoji: "👑",
			IsOwner: true, DefaultPermission: "write_create",
		},
	})
	for _, want := range []string{"agent: 👑 owner #1", "role:    owner", "(none)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestWhoamiText_GroupsByLevel(t *testing.T) {
	got := whoamiText(&client.Whoami{
		Agent: client.Agent{
			ID: 9, Name: "scout", DefaultPermission: "read",
		},
		ProjectPermissions: []client.ProjectPermission{
			{ProjectID: 3, Name: "Foo", Level: "write"},
			{ProjectID: 5, Name: "Bar", Level: "write_create"},
			{ProjectID: 7, Name: "Baz", Level: "write"},
		},
	})
	// write_create comes before write because it's the higher rank.
	wcIdx := strings.Index(got, "write_create in:")
	wIdx := strings.Index(got, "write in:")
	if wcIdx == -1 || wIdx == -1 || wcIdx > wIdx {
		t.Fatalf("expected write_create before write, got:\n%s", got)
	}
	if !strings.Contains(got, "write in: Foo, Baz") {
		t.Errorf("expected 'write in: Foo, Baz' (server-ordered), got:\n%s", got)
	}
	if !strings.Contains(got, "default: read") {
		t.Errorf("missing default line, got:\n%s", got)
	}
}

func TestWhoamiText_UnnamedEmoji(t *testing.T) {
	got := whoamiText(&client.Whoami{
		Agent: client.Agent{ID: 4, DefaultPermission: "none"},
	})
	if !strings.Contains(got, "agent: (unnamed) #4") {
		t.Fatalf("missing fallback name, got:\n%s", got)
	}
}

// -- end-to-end through runWhoami ------------------------------------------

func TestRunWhoami_JSONPassesThroughWireShape(t *testing.T) {
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/whoami" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"agent": map[string]any{
				"id": 1, "name": "owner", "emoji": "👑",
				"is_owner": true, "default_permission": "write_create",
			},
			"project_permissions": []any{},
		})
	})
	var stdout, stderr bytes.Buffer
	if err := runWhoami(context.Background(), &stdout, &stderr, fix.Opts); err != nil {
		t.Fatalf("runWhoami: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{`"status":"ok"`, `"agent":`, `"is_owner":true`, `"default_permission":"write_create"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRunWhoami_403SurfacesAPIError(t *testing.T) {
	fix := newAuthedFixture(t, "json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_agent_secret"})
	})
	var stdout, stderr bytes.Buffer
	err := runWhoami(context.Background(), &stdout, &stderr, fix.Opts)
	if err == nil {
		t.Fatal("expected error")
	}
	out := stdout.String()
	if !strings.Contains(out, `"http_status":403`) || !strings.Contains(out, `"invalid_agent_secret"`) {
		t.Fatalf("error envelope missing fields:\n%s", out)
	}
}
