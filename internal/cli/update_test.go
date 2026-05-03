package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/build"
)

// TestUpdateCmd_DevBinaryRefuses pins the cobra plumbing: the command runs
// updater.RunUpdate with cmd.OutOrStdout/ErrOrStderr/InOrStdin, and on the
// dev binary returns a friendly message via stdout.
func TestUpdateCmd_DevBinaryRefuses(t *testing.T) {
	prev := build.AppName
	build.AppName = "sprawl_dev"
	t.Cleanup(func() { build.AppName = prev })

	cmd := newUpdateCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout.String(), "sprawl_dev is built from source") {
		t.Fatalf("missing dev refusal: %q", stdout.String())
	}
}

// TestPrintAndReturn_PrintsAndPropagates pins the contract that skill
// install / update RunE wrappers depend on: the error is echoed to stderr
// AND returned so cobra's exit-1 path still fires. Without this, the
// SilenceErrors=true commands would crash silently — the exact regression
// flagged in review.
func TestPrintAndReturn_PrintsAndPropagates(t *testing.T) {
	var stderr bytes.Buffer
	in := errors.New("boom")
	got := printAndReturn(&stderr, in)
	if got != in {
		t.Fatalf("returned err %v, want %v (must propagate)", got, in)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr missing error text: %q", stderr.String())
	}
	if !strings.HasPrefix(stderr.String(), "error:") {
		t.Fatalf("stderr should start with 'error:', got %q", stderr.String())
	}
}
