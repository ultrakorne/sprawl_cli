package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestTextArgs_PrintsUsageOnFailure locks in the invariant that arg-count
// failures surface to the user even when the command has SilenceErrors=true
// (so reportErr from RunE can stay the sole printer for API-level errors).
func TestTextArgs_PrintsUsageOnFailure(t *testing.T) {
	cmd := &cobra.Command{Use: "demo <id>", SilenceErrors: true, SilenceUsage: true}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	err := textArgs(cobra.ExactArgs(1))(cmd, []string{})
	if err == nil {
		t.Fatal("expected error")
	}
	s := stderr.String()
	if !strings.Contains(s, "Error:") {
		t.Fatalf("stderr missing Error line: %q", s)
	}
	if !strings.Contains(s, "Usage: demo <id>") {
		t.Fatalf("stderr missing Usage line: %q", s)
	}
}

// TestRootCmd_DashHIsHuman guards the -h reclaim: -h is --human (text), not the
// help shorthand, while --help still works.
func TestRootCmd_DashHIsHuman(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"version", "-h"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute version -h: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "api:") {
		t.Fatalf("-h should run the command (human output), not show help:\n%s", got)
	}
	if strings.Contains(got, "Usage:") {
		t.Fatalf("-h must not trigger help:\n%s", got)
	}
}

func TestTextArgs_PassesThroughWhenValid(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	if err := textArgs(cobra.NoArgs)(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty when Args passes, got %q", stderr.String())
	}
}

// TestRootCmd_InvalidFormatIsLoudAndPreHTTP locks in two things that were
// previously broken together: an unusable --format / $SPRAWL_OUTPUT must (a)
// tell the user, and (b) fail before the request goes out.
//
// (a) Every subcommand sets SilenceErrors=true, so an error returned out of
// PersistentPreRunE is swallowed by cobra — the user got an empty exit 1.
// (b) The format used to be resolved at render time, i.e. *after* the HTTP
// call, so a bad value on a mutating command let the write land server-side
// and still exited non-zero — indistinguishable, to a caller that retries on
// failure, from a write that never happened.
func TestRootCmd_InvalidFormatIsLoudAndPreHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("request escaped before format validation: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SPRAWL_API_URL", srv.URL)
	t.Setenv("SPRAWL_TOKEN", "the-token")
	t.Setenv("SPRAWL_AGENT_SECRET", "the-secret")
	t.Setenv("SPRAWL_NO_UPDATE_CHECK", "1")

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"whoami", "--format=yaml"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected a non-zero exit for an invalid format")
	}
	if !strings.Contains(stderr.String(), "invalid format") {
		t.Fatalf("stderr should explain the bad format, got %q", stderr.String())
	}
}
