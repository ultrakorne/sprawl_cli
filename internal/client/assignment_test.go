package client

import (
	"context"
	"net/http"
	"testing"
)

func TestAssignmentDiscoveryModelsAndRoutes(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"actors": []any{
				map[string]any{"type": "user", "id": 3, "label": "Alex", "marker": "🦊"},
				map[string]any{"type": "agent_key", "id": 3, "label": "builder", "marker": "🤖"},
			},
		})
	})
	c := NewAuthed("tok", "sek", WithProjectKey("acme"), WithWorkspace("9"))
	actors, err := c.ListWorkspaceActors(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(actors) != 2 || actors[0].Type != "user" || actors[1].Type != "agent_key" || actors[0].ID != actors[1].ID || actors[0].Label != "Alex" || actors[1].Marker != "🤖" {
		t.Fatalf("actors = %+v", actors)
	}
	req := ts.Requests()[0]
	if req.Method != "GET" || req.Path != "/api/v1/workspaces/42/actors" || req.Authorization != "Bearer tok" || req.AgentSecret != "sek" || req.ProjectKey != "acme" {
		t.Fatalf("request = %+v", req)
	}
}
