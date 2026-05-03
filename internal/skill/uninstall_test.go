package skill

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// stubConfirm replaces the bubbletea confirm prompt with a deterministic
// answer. The choice prompt is unused by Uninstall, so leave it alone.
func stubConfirm(t *testing.T, proceed bool) {
	t.Helper()
	prev := promptConfirmFunc
	promptConfirmFunc = func(_ io.Reader, _ io.Writer, _ string) (bool, error) {
		return proceed, nil
	}
	t.Cleanup(func() { promptConfirmFunc = prev })
}

func stubConfirmCancelled(t *testing.T) {
	t.Helper()
	prev := promptConfirmFunc
	promptConfirmFunc = func(_ io.Reader, _ io.Writer, _ string) (bool, error) {
		return false, errPromptCancelled
	}
	t.Cleanup(func() { promptConfirmFunc = prev })
}

// seedInstalls writes a couple of fake on-disk artefacts (one skill dir,
// one agent file) under home and records both in config. Returns the two
// destination paths so tests can assert their state.
func seedInstalls(t *testing.T, home string) (skillDir, agentFile string) {
	t.Helper()
	skillDir = filepath.Join(home, ".claude", "skills", "sprawl")
	agentFile = filepath.Join(home, ".claude", "agents", "sprawl-bookkeeper.md")

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("body"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(agentFile), 0o755); err != nil {
		t.Fatalf("mkdir agent: %v", err)
	}
	if err := os.WriteFile(agentFile, []byte("agent"), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	cfg := &config.Config{SkillInstalls: []config.SkillInstall{
		{Kind: "skill", Name: "sprawl", Tool: "claude", Scope: "global", Path: skillDir, Version: "0.1.0"},
		{Kind: "agent", Name: "sprawl-bookkeeper", Tool: "claude", Scope: "global", Path: agentFile, Version: "0.2.0"},
	}}
	if err := config.Save(build.AppName, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return skillDir, agentFile
}

func TestUninstall_NoInstalls(t *testing.T) {
	pinHomeAndConfig(t)
	var out bytes.Buffer
	if err := Uninstall(context.Background(), strings.NewReader(""), &out); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !strings.Contains(out.String(), "Nothing installed") {
		t.Fatalf("expected nothing-installed message, got %q", out.String())
	}
}

func TestUninstall_RemovesAllAndClearsConfig(t *testing.T) {
	home := pinHomeAndConfig(t)
	skillDir, agentFile := seedInstalls(t, home)
	stubConfirm(t, true)

	var out bytes.Buffer
	if err := Uninstall(context.Background(), strings.NewReader(""), &out); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatalf("skill dir still present: %v", err)
	}
	if _, err := os.Stat(agentFile); !os.IsNotExist(err) {
		t.Fatalf("agent file still present: %v", err)
	}

	cfg, err := config.Load(build.AppName)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SkillInstalls) != 0 {
		t.Fatalf("SkillInstalls = %d, want 0 (%+v)", len(cfg.SkillInstalls), cfg.SkillInstalls)
	}
	if !strings.Contains(out.String(), "Done.") {
		t.Fatalf("expected Done message, got %q", out.String())
	}
}

func TestUninstall_AbortAtConfirm_NoChanges(t *testing.T) {
	home := pinHomeAndConfig(t)
	skillDir, agentFile := seedInstalls(t, home)
	stubConfirm(t, false)

	var out bytes.Buffer
	if err := Uninstall(context.Background(), strings.NewReader(""), &out); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := os.Stat(skillDir); err != nil {
		t.Fatalf("skill dir removed despite abort: %v", err)
	}
	if _, err := os.Stat(agentFile); err != nil {
		t.Fatalf("agent file removed despite abort: %v", err)
	}

	cfg, err := config.Load(build.AppName)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SkillInstalls) != 2 {
		t.Fatalf("SkillInstalls = %d, want 2", len(cfg.SkillInstalls))
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Fatalf("expected Aborted message, got %q", out.String())
	}
}

func TestUninstall_CancelledPrompt_NoChanges(t *testing.T) {
	home := pinHomeAndConfig(t)
	skillDir, _ := seedInstalls(t, home)
	stubConfirmCancelled(t)

	var out bytes.Buffer
	if err := Uninstall(context.Background(), strings.NewReader(""), &out); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(skillDir); err != nil {
		t.Fatalf("skill dir removed despite cancel: %v", err)
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Fatalf("expected Aborted message, got %q", out.String())
	}
}

func TestUninstall_MissingPath_StillClearsRecord(t *testing.T) {
	home := pinHomeAndConfig(t)
	// Record a path that was never created on disk — uninstall should
	// happily drop the row anyway (RemoveAll on a missing path is nil).
	ghost := filepath.Join(home, ".claude", "skills", "sprawl-ghost")
	cfg := &config.Config{SkillInstalls: []config.SkillInstall{
		{Kind: "skill", Name: "sprawl", Tool: "claude", Scope: "global", Path: ghost, Version: "0.1.0"},
	}}
	if err := config.Save(build.AppName, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	stubConfirm(t, true)

	var out bytes.Buffer
	if err := Uninstall(context.Background(), strings.NewReader(""), &out); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	loaded, err := config.Load(build.AppName)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.SkillInstalls) != 0 {
		t.Fatalf("SkillInstalls = %d, want 0", len(loaded.SkillInstalls))
	}
}
