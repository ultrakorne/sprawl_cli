package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// TestOnApproved_PersistsAndPointsAtSettings covers the post-approval UX:
// the token lands in config.toml and the message points the user at the
// settings page (phase 6) for the owner agent secret.
func TestOnApproved_PersistsAndPointsAtSettings(t *testing.T) {
	dir := scratchConfigDir(t)
	t.Setenv("SPRAWL_API_URL", "https://example.test")

	var buf bytes.Buffer
	if err := onApproved(&buf, "tok-abc"); err != nil {
		t.Fatalf("onApproved: %v", err)
	}

	loaded, err := config.Load(build.AppName)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if loaded.Token != "tok-abc" {
		t.Fatalf("token on disk = %q, want tok-abc", loaded.Token)
	}

	out := buf.String()
	for _, want := range []string{
		"Logged in",
		"SPRAWL_AGENT_SECRET",
		"https://example.test/auth-settings",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// Sanity: the config file really lives under the scratch dir.
	if !strings.HasPrefix(loaded.Token, "tok-") {
		t.Fatalf("token roundtrip failed")
	}
	if dir == "" {
		t.Fatal("scratchConfigDir returned empty")
	}
}

func TestDeviceTokenName_SystemAndHost(t *testing.T) {
	got := deviceTokenName()
	if got == "" {
		t.Fatal("deviceTokenName returned empty")
	}
	if !strings.HasPrefix(got, runtime.GOOS) {
		t.Fatalf("deviceTokenName = %q, want prefix %q", got, runtime.GOOS)
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		want := runtime.GOOS + " · " + host
		if got != want && !strings.HasPrefix(want, got) /* truncated */ {
			t.Fatalf("deviceTokenName = %q, want %q", got, want)
		}
	}
	if rc := utf8.RuneCountInString(got); rc > 64 {
		t.Fatalf("rune count = %d, want ≤ 64", rc)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes(strings.Repeat("é", 80), 64); utf8.RuneCountInString(got) != 64 {
		t.Fatalf("truncateRunes(80×é, 64) rune count = %d, want 64", utf8.RuneCountInString(got))
	}
	if got := truncateRunes("short", 64); got != "short" {
		t.Fatalf("truncateRunes shouldn't shrink short input: %q", got)
	}
	if got := truncateRunes("anything", 0); got != "" {
		t.Fatalf("truncateRunes with n=0 should return empty: %q", got)
	}
}

func TestSanitizeNamePart_StripsControlChars(t *testing.T) {
	in := "  he\x01llo\tworld\x7f  "
	got := sanitizeNamePart(in)
	if got != "helloworld" {
		t.Fatalf("sanitizeNamePart = %q, want %q", got, "helloworld")
	}
}

// TestRunLogin_PromptsForSettingsBeforeDeviceFlow covers the phase 6 copy:
// the settings URL must appear before the 'To authorise this device' prompt
// so the user can grab their owner secret while the device grant is pending.
func TestRunLogin_PromptsForSettingsBeforeDeviceFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/device":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":               "dev-code",
				"user_code":                 "USER-123",
				"verification_uri":          "https://example.test/auth/device",
				"verification_uri_complete": "https://example.test/auth/device?user_code=USER-123",
				"expires_in":                600,
				"interval":                  60,
			})
		case "/api/auth/device/token":
			// Never polled in this test — runLogin is cancelled right after
			// the prelude prints. Fail loudly if we ever get here.
			t.Errorf("poll endpoint hit; test should cancel before first tick")
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SPRAWL_API_URL", srv.URL)
	scratchConfigDir(t)

	// Cancel the context before the first poll tick (60s) so runLogin
	// returns after printing the prelude + the device-flow prompt.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	_ = runLogin(ctx, &buf) // non-nil error (context cancelled) is expected.

	out := buf.String()
	idxSettings := strings.Index(out, srv.URL+"/auth-settings")
	idxAuthorise := strings.Index(out, "To authorise this device")
	if idxSettings < 0 {
		t.Fatalf("prelude missing settings URL:\n%s", out)
	}
	if idxAuthorise < 0 {
		t.Fatalf("device-flow prompt missing:\n%s", out)
	}
	if idxSettings > idxAuthorise {
		t.Fatalf("settings URL must come before 'To authorise this device' prompt:\n%s", out)
	}
}
