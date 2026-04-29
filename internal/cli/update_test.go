package cli

import (
	"bytes"
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
