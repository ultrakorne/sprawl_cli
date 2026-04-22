package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// optsWith builds a runtimeOpts for a specific output format. Every call site
// in these tests wants this one-liner; keep it local to avoid a helper file.
func optsWith(format string) *runtimeOpts {
	return &runtimeOpts{format: format}
}

func TestResolveFormat_FlagWins(t *testing.T) {
	t.Setenv("SPRAWL_OUTPUT", "text")
	f, err := resolveFormat(optsWith("json"))
	if err != nil {
		t.Fatalf("resolveFormat: %v", err)
	}
	if f != FormatJSON {
		t.Fatalf("format = %q, want json", f)
	}
}

func TestResolveFormat_EnvWhenFlagUnset(t *testing.T) {
	t.Setenv("SPRAWL_OUTPUT", "text")
	f, err := resolveFormat(optsWith(""))
	if err != nil {
		t.Fatalf("resolveFormat: %v", err)
	}
	if f != FormatText {
		t.Fatalf("format = %q, want text", f)
	}
}

func TestResolveFormat_DefaultIsTOON(t *testing.T) {
	t.Setenv("SPRAWL_OUTPUT", "")
	f, err := resolveFormat(optsWith(""))
	if err != nil {
		t.Fatalf("resolveFormat: %v", err)
	}
	if f != FormatTOON {
		t.Fatalf("format = %q, want toon", f)
	}
}

func TestResolveFormat_Invalid(t *testing.T) {
	if _, err := resolveFormat(optsWith("yaml")); err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestRenderPayload_Text(t *testing.T) {
	var buf bytes.Buffer
	if err := renderPayload(&buf, map[string]any{"status": "ok"}, "200 ok", optsWith("text")); err != nil {
		t.Fatalf("renderPayload: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "200 ok" {
		t.Fatalf("text output = %q", got)
	}
}

func TestRenderPayload_JSON(t *testing.T) {
	var buf bytes.Buffer
	if err := renderPayload(&buf, map[string]any{"status": "ok"}, "200 ok", optsWith("json")); err != nil {
		t.Fatalf("renderPayload: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("not valid JSON: %v (%q)", err, buf.String())
	}
	if out["status"] != "ok" {
		t.Fatalf("output = %+v", out)
	}
}

func TestRenderPayload_TOON(t *testing.T) {
	var buf bytes.Buffer
	if err := renderPayload(&buf, map[string]any{"status": "ok"}, "200 ok", optsWith("toon")); err != nil {
		t.Fatalf("renderPayload: %v", err)
	}
	// TOON is its own format — we don't re-parse it, just assert the
	// payload substring is present and we didn't emit the text fallback.
	s := buf.String()
	if !strings.Contains(s, "status") || !strings.Contains(s, "ok") {
		t.Fatalf("toon output missing fields: %q", s)
	}
	if strings.TrimSpace(s) == "200 ok" {
		t.Fatalf("toon rendered the text fallback: %q", s)
	}
}

func TestReportErr_TextGoesToStderrOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	orig := errors.New("boom")
	got := reportErr(&stdout, &stderr, orig, optsWith("text"))
	if got != orig {
		t.Fatalf("reportErr should return the original error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout should be empty in text mode, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr missing error: %q", stderr.String())
	}
}

func TestReportErr_JSONStructuresAPIError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	apiErr := &client.APIError{Status: 403, Code: "forbidden", Body: `{"error":"forbidden"}`}
	_ = reportErr(&stdout, &stderr, apiErr, optsWith("json"))
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty for structured format, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	if out["status"] != "error" {
		t.Fatalf("status = %v", out["status"])
	}
	if out["error"] != "forbidden" {
		t.Fatalf("error = %v", out["error"])
	}
	// JSON unmarshal turns numbers into float64.
	if got, _ := out["http_status"].(float64); int(got) != 403 {
		t.Fatalf("http_status = %v", out["http_status"])
	}
}

func TestReportErr_JSONPlainErrorHasNoHTTPStatus(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_ = reportErr(&stdout, &stderr, errors.New("no network"), optsWith("json"))
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	if _, ok := out["http_status"]; ok {
		t.Fatalf("plain errors should not set http_status: %+v", out)
	}
	if out["error"] != "no network" {
		t.Fatalf("error = %v", out["error"])
	}
}

func TestReportErr_APIErrorWithoutCodeFallsBackToBody(t *testing.T) {
	var stdout, stderr bytes.Buffer
	apiErr := &client.APIError{Status: 500, Body: "internal boom"}
	_ = reportErr(&stdout, &stderr, apiErr, optsWith("json"))
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if out["error"] != "internal boom" {
		t.Fatalf("error field = %v, want body fallback", out["error"])
	}
}

// TestReportErr_ChangesetErrorsSurfaceDetails covers the shared fallback shape
// the server uses for changeset failures: `{"errors": {"field": [...]}}`.
// APIError.Code is empty in that case; reportErr should decode the body and
// emit error="invalid" + details=<errors map> rather than dumping the raw
// JSON body into the error string.
func TestReportErr_ChangesetErrorsSurfaceDetails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	apiErr := &client.APIError{
		Status: 422,
		// Code is empty — the shared fallback shape doesn't set a top-level
		// `error` field.
		Body: `{"errors":{"title":["can't be blank"]}}`,
	}
	_ = reportErr(&stdout, &stderr, apiErr, optsWith("json"))
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, stdout.String())
	}
	if out["error"] != "invalid" {
		t.Fatalf("error = %v, want invalid", out["error"])
	}
	details, ok := out["details"].(map[string]any)
	if !ok {
		t.Fatalf("details missing or wrong type: %+v", out["details"])
	}
	msgs, _ := details["title"].([]any)
	if len(msgs) != 1 || msgs[0] != "can't be blank" {
		t.Fatalf("details.title = %v", details["title"])
	}
}

// TestReportErr_TOON_APIError covers the previously unasserted FormatTOON
// branch of reportErr. Shape is loose (TOON is its own syntax) — we just
// assert the key fields made it into the emitted payload on stdout and that
// stderr stayed clean.
func TestReportErr_TOON_APIError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	apiErr := &client.APIError{Status: 404, Code: "not_found", Body: `{"error":"not_found"}`}
	_ = reportErr(&stdout, &stderr, apiErr, optsWith("toon"))
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty for TOON, got %q", stderr.String())
	}
	s := stdout.String()
	if !strings.Contains(s, "error") || !strings.Contains(s, "not_found") {
		t.Fatalf("toon error payload missing fields: %q", s)
	}
	if !strings.Contains(s, "http_status") || !strings.Contains(s, "404") {
		t.Fatalf("toon error payload missing http_status: %q", s)
	}
}
