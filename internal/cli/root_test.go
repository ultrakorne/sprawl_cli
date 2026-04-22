package cli

import (
	"bytes"
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
