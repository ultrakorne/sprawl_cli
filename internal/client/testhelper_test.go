package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// recordedRequest captures the fields we care about asserting on in tests.
type recordedRequest struct {
	Method        string
	Path          string
	Authorization string
	AgentSecret   string
	ContentType   string
	Accept        string
	Body          []byte
}

// testServer wraps httptest.Server with a request log and installs
// SPRAWL_API_URL for the test's lifetime so `New()` / `NewAuthed()` target it.
type testServer struct {
	Server   *httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
}

// newTestServer spins up a mock backend. The handler controls the response;
// header capture is done by the wrapper before the handler runs.
func newTestServer(t *testing.T, handler http.HandlerFunc) *testServer {
	t.Helper()
	ts := &testServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ts.mu.Lock()
		ts.requests = append(ts.requests, recordedRequest{
			Method:        r.Method,
			Path:          r.URL.Path,
			Authorization: r.Header.Get("Authorization"),
			AgentSecret:   r.Header.Get("X-Agent-Secret"),
			ContentType:   r.Header.Get("Content-Type"),
			Accept:        r.Header.Get("Accept"),
			Body:          body,
		})
		ts.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(ts.Server.Close)
	t.Setenv("SPRAWL_API_URL", ts.Server.URL)
	return ts
}

func (ts *testServer) Requests() []recordedRequest {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]recordedRequest, len(ts.requests))
	copy(out, ts.requests)
	return out
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
