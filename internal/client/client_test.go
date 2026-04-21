package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/build"
)

// -- BaseURL ----------------------------------------------------------------

func TestBaseURL_EnvOverride(t *testing.T) {
	t.Setenv("SPRAWL_API_URL", "https://env.example")
	if got := BaseURL(); got != "https://env.example" {
		t.Fatalf("BaseURL = %q, want env value", got)
	}
}

func TestBaseURL_FallsBackToBuild(t *testing.T) {
	t.Setenv("SPRAWL_API_URL", "")
	want := strings.TrimRight(build.APIURL, "/")
	if got := BaseURL(); got != want {
		t.Fatalf("BaseURL = %q, want %q (from build.APIURL)", got, want)
	}
}

func TestBaseURL_StripsTrailingSlash(t *testing.T) {
	t.Setenv("SPRAWL_API_URL", "https://env.example/")
	if got := BaseURL(); got != "https://env.example" {
		t.Fatalf("BaseURL = %q, want trailing slash stripped", got)
	}
}

// -- Error types ------------------------------------------------------------

func TestAPIError_FormatsWithCode(t *testing.T) {
	e := &APIError{Status: 403, Code: "forbidden", Body: `{"error":"forbidden"}`}
	if got := e.Error(); got != "http 403: forbidden" {
		t.Fatalf("Error() = %q, want http 403: forbidden", got)
	}
}

func TestAPIError_FormatsWithoutCodeFallsBackToBody(t *testing.T) {
	e := &APIError{Status: 500, Body: "internal boom"}
	if got := e.Error(); got != "http 500: internal boom" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestDevicePollError_Format(t *testing.T) {
	e := &DevicePollError{Code: "authorization_pending"}
	if got := e.Error(); got != "device grant: authorization_pending" {
		t.Fatalf("Error() = %q", got)
	}
}

// -- Device flow ------------------------------------------------------------

func TestCreateDeviceGrant_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"device_code":               "dc",
			"user_code":                 "UC1",
			"verification_uri":          "https://x/auth/device",
			"verification_uri_complete": "https://x/auth/device?user_code=UC1",
			"expires_in":                600,
			"interval":                  5,
		})
	})
	c := New()
	g, err := c.CreateDeviceGrant(context.Background())
	if err != nil {
		t.Fatalf("CreateDeviceGrant: %v", err)
	}
	if g.DeviceCode != "dc" || g.UserCode != "UC1" || g.Interval != 5 || g.ExpiresIn != 600 {
		t.Fatalf("grant = %+v", g)
	}
	reqs := ts.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Method != "POST" || reqs[0].Path != "/api/auth/device" {
		t.Fatalf("request = %+v", reqs[0])
	}
	if reqs[0].Authorization != "" || reqs[0].AgentSecret != "" {
		t.Fatalf("unauth endpoint must not send auth headers: %+v", reqs[0])
	}
}

func TestPollDeviceToken_Success(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"token": "tok-abc"})
	})
	c := New()
	tok, err := c.PollDeviceToken(context.Background(), "dc")
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if tok != "tok-abc" {
		t.Fatalf("token = %q", tok)
	}
}

func TestPollDeviceToken_RFC8628Codes(t *testing.T) {
	// The server returns 400 for every non-success state; the client decodes
	// the `error` field and surfaces it as DevicePollError.
	for _, code := range []string{"authorization_pending", "access_denied", "expired_token", "invalid_grant"} {
		t.Run(code, func(t *testing.T) {
			code := code
			newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 400, map[string]string{"error": code})
			})
			c := New()
			_, err := c.PollDeviceToken(context.Background(), "dc")
			var dpe *DevicePollError
			if !errors.As(err, &dpe) {
				t.Fatalf("want *DevicePollError, got %T: %v", err, err)
			}
			if dpe.Code != code {
				t.Fatalf("DevicePollError.Code = %q, want %q", dpe.Code, code)
			}
		})
	}
}

func TestPollDeviceToken_EmptyBodyStillErrors(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{}) // no token, no error
	})
	c := New()
	_, err := c.PollDeviceToken(context.Background(), "dc")
	if err == nil {
		t.Fatal("expected error on empty success body")
	}
}

// -- Authed endpoints -------------------------------------------------------

func TestHealth_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	c := NewAuthed("tok", "sec")
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	reqs := ts.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Authorization != "Bearer tok" {
		t.Fatalf("Authorization = %q", reqs[0].Authorization)
	}
	if reqs[0].AgentSecret != "sec" {
		t.Fatalf("X-Agent-Secret = %q", reqs[0].AgentSecret)
	}
}

func TestHealth_401MissingBearer(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 401, "missing_bearer")
	})
	c := NewAuthed("tok", "sec")
	err := c.Health(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %T: %v", err, err)
	}
	if ae.Status != 401 || ae.Code != "missing_bearer" {
		t.Fatalf("APIError = %+v", ae)
	}
}

func TestHealth_403InvalidAgentSecret(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeError(w, 403, "invalid_agent_secret")
	})
	c := NewAuthed("tok", "sec")
	err := c.Health(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %T", err)
	}
	if ae.Status != 403 || ae.Code != "invalid_agent_secret" {
		t.Fatalf("APIError = %+v", ae)
	}
}

// -- Theme ------------------------------------------------------------------

func TestGetTheme_Success(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"theme": map[string]string{"id": "tokyo-night", "name": "Tokyo Night"}})
	})
	c := NewAuthed("tok", "sec")
	theme, err := c.GetTheme(context.Background())
	if err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if theme.ID != "tokyo-night" || theme.Name != "Tokyo Night" {
		t.Fatalf("theme = %+v", theme)
	}
}

func TestSetTheme_Success(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"theme": map[string]string{"id": "kanagawa", "name": "Kanagawa"}})
	})
	c := NewAuthed("tok", "sec")
	theme, err := c.SetTheme(context.Background(), "Kanagawa")
	if err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if theme.ID != "kanagawa" {
		t.Fatalf("theme = %+v", theme)
	}
	reqs := ts.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request")
	}
	r := reqs[0]
	if r.Method != "PATCH" || r.Path != "/api/v1/settings/theme" {
		t.Fatalf("request = %+v", r)
	}
	if r.ContentType != "application/json" {
		t.Fatalf("Content-Type = %q", r.ContentType)
	}
	var body struct {
		Theme string `json:"theme"`
	}
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Theme != "Kanagawa" {
		t.Fatalf("body.theme = %q", body.Theme)
	}
}

func TestSetTheme_ErrorMatrix(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		code     string
		wantCode string
	}{
		{"theme_not_found", 404, "theme_not_found", "theme_not_found"},
		{"forbidden (non-owner)", 403, "forbidden", "forbidden"},
		{"invalid_agent_secret", 403, "invalid_agent_secret", "invalid_agent_secret"},
		{"missing_bearer", 401, "missing_bearer", "missing_bearer"},
		{"theme_required", 422, "theme_required", "theme_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeError(w, tc.status, tc.code)
			})
			c := NewAuthed("tok", "sec")
			_, err := c.SetTheme(context.Background(), "anything")
			var ae *APIError
			if !errors.As(err, &ae) {
				t.Fatalf("want APIError, got %T: %v", err, err)
			}
			if ae.Status != tc.status || ae.Code != tc.wantCode {
				t.Fatalf("APIError = %+v, want status %d code %q", ae, tc.status, tc.wantCode)
			}
		})
	}
}

// -- Header invariants ------------------------------------------------------

// TestHeaderInvariants asserts the security-adjacent contract: every
// /api/v1/* call sends both Authorization and X-Agent-Secret; device-flow
// calls send neither; Content-Type is only set on requests with a body.
func TestHeaderInvariants(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/device":
			writeJSON(w, 200, map[string]any{
				"device_code": "dc", "user_code": "UC", "verification_uri": "u",
				"verification_uri_complete": "u", "expires_in": 600, "interval": 5,
			})
		case "/api/auth/device/token":
			writeJSON(w, 200, map[string]string{"token": "tok"})
		case "/api/v1/health":
			writeJSON(w, 200, map[string]string{"status": "ok"})
		case "/api/v1/settings/theme":
			writeJSON(w, 200, map[string]any{"theme": map[string]string{"id": "x", "name": "X"}})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(500)
		}
	})

	unauth := New()
	if _, err := unauth.CreateDeviceGrant(context.Background()); err != nil {
		t.Fatalf("CreateDeviceGrant: %v", err)
	}
	if _, err := unauth.PollDeviceToken(context.Background(), "dc"); err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}

	authed := NewAuthed("the-token", "the-secret")
	if err := authed.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if _, err := authed.GetTheme(context.Background()); err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if _, err := authed.SetTheme(context.Background(), "X"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}

	for _, r := range ts.Requests() {
		if r.Accept != "application/json" {
			t.Errorf("%s %s: Accept = %q, want application/json", r.Method, r.Path, r.Accept)
		}
		switch {
		case strings.HasPrefix(r.Path, "/api/auth/"):
			if r.Authorization != "" {
				t.Errorf("%s %s: unexpected Authorization header", r.Method, r.Path)
			}
			if r.AgentSecret != "" {
				t.Errorf("%s %s: unexpected X-Agent-Secret header", r.Method, r.Path)
			}
		case strings.HasPrefix(r.Path, "/api/v1/"):
			if r.Authorization != "Bearer the-token" {
				t.Errorf("%s %s: Authorization = %q", r.Method, r.Path, r.Authorization)
			}
			if r.AgentSecret != "the-secret" {
				t.Errorf("%s %s: X-Agent-Secret = %q", r.Method, r.Path, r.AgentSecret)
			}
		default:
			t.Errorf("unexpected path bucket: %q", r.Path)
		}
		// Content-Type should only appear on requests with a body.
		hasBody := len(r.Body) > 0
		switch {
		case hasBody && r.ContentType != "application/json":
			t.Errorf("%s %s: body present but Content-Type = %q", r.Method, r.Path, r.ContentType)
		case !hasBody && r.ContentType != "":
			t.Errorf("%s %s: no body but Content-Type = %q", r.Method, r.Path, r.ContentType)
		}
	}
}

// -- Network / transport errors --------------------------------------------

func TestServerClosed_ReturnsNetworkError(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	ts.Server.Close() // hang up before the request
	c := NewAuthed("tok", "sec")
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected network error")
	}
	// Should not be surfaced as an APIError — there was no HTTP response.
	var ae *APIError
	if errors.As(err, &ae) {
		t.Fatalf("unexpected APIError: %+v", ae)
	}
}

func TestServer5xx_ReturnsAPIError(t *testing.T) {
	newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("boom"))
	})
	c := NewAuthed("tok", "sec")
	err := c.Health(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want APIError, got %T", err)
	}
	if ae.Status != 500 {
		t.Fatalf("status = %d", ae.Status)
	}
}
