package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// withFormatFlag sets the package-level formatFlag for the test's duration.
func withFormatFlag(t *testing.T, v string) {
	t.Helper()
	prev := formatFlag
	formatFlag = v
	t.Cleanup(func() { formatFlag = prev })
}

func TestResolveFormat_FlagWins(t *testing.T) {
	withFormatFlag(t, "json")
	t.Setenv("SPRAWL_OUTPUT", "text")
	f, err := resolveFormat()
	if err != nil {
		t.Fatalf("resolveFormat: %v", err)
	}
	if f != FormatJSON {
		t.Fatalf("format = %q, want json", f)
	}
}

func TestResolveFormat_EnvWhenFlagUnset(t *testing.T) {
	withFormatFlag(t, "")
	t.Setenv("SPRAWL_OUTPUT", "text")
	f, err := resolveFormat()
	if err != nil {
		t.Fatalf("resolveFormat: %v", err)
	}
	if f != FormatText {
		t.Fatalf("format = %q, want text", f)
	}
}

func TestResolveFormat_DefaultIsTOON(t *testing.T) {
	withFormatFlag(t, "")
	t.Setenv("SPRAWL_OUTPUT", "")
	f, err := resolveFormat()
	if err != nil {
		t.Fatalf("resolveFormat: %v", err)
	}
	if f != FormatTOON {
		t.Fatalf("format = %q, want toon", f)
	}
}

func TestResolveFormat_Invalid(t *testing.T) {
	withFormatFlag(t, "yaml")
	if _, err := resolveFormat(); err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestRenderPayload_Text(t *testing.T) {
	withFormatFlag(t, "text")
	var buf bytes.Buffer
	if err := renderPayload(&buf, map[string]any{"status": "ok"}, "200 ok"); err != nil {
		t.Fatalf("renderPayload: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "200 ok" {
		t.Fatalf("text output = %q", got)
	}
}

func TestRenderPayload_JSON(t *testing.T) {
	withFormatFlag(t, "json")
	var buf bytes.Buffer
	if err := renderPayload(&buf, map[string]any{"status": "ok"}, "200 ok"); err != nil {
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
	withFormatFlag(t, "toon")
	var buf bytes.Buffer
	if err := renderPayload(&buf, map[string]any{"status": "ok"}, "200 ok"); err != nil {
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
	withFormatFlag(t, "text")
	var stdout, stderr bytes.Buffer
	orig := errors.New("boom")
	got := reportErr(&stdout, &stderr, orig)
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
	withFormatFlag(t, "json")
	var stdout, stderr bytes.Buffer
	apiErr := &client.APIError{Status: 403, Code: "forbidden", Body: `{"error":"forbidden"}`}
	_ = reportErr(&stdout, &stderr, apiErr)
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
	withFormatFlag(t, "json")
	var stdout, stderr bytes.Buffer
	_ = reportErr(&stdout, &stderr, errors.New("no network"))
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
	withFormatFlag(t, "json")
	var stdout, stderr bytes.Buffer
	apiErr := &client.APIError{Status: 500, Body: "internal boom"}
	_ = reportErr(&stdout, &stderr, apiErr)
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if out["error"] != "internal boom" {
		t.Fatalf("error field = %v, want body fallback", out["error"])
	}
}
