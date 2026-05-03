package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSkillDir_WipesAndRepopulates(t *testing.T) {
	dst := t.TempDir()
	// Pre-existing leftover that should be removed by the wipe.
	stale := filepath.Join(dst, "stale.md")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	files := map[string][]byte{
		".claude/skills/sprawl/SKILL.md":      []byte("---\nname: sprawl\nversion: \"0.3.1\"\n---\nbody\n"),
		".claude/skills/sprawl/SETUP.md":      []byte("setup body"),
		".claude/agents/sprawl-bookkeeper.md": []byte("agent — different target"),
	}
	got, err := writeSkillDir(files, ".claude/skills/sprawl", dst)
	if err != nil {
		t.Fatalf("writeSkillDir: %v", err)
	}
	if got != "0.3.1" {
		t.Fatalf("version = %q, want 0.3.1", got)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale file survived wipe (err=%v)", err)
	}
	body, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if string(body) != "---\nname: sprawl\nversion: \"0.3.1\"\n---\nbody\n" {
		t.Fatalf("SKILL.md mismatch: %q", body)
	}
	if _, err := os.Stat(filepath.Join(dst, "SETUP.md")); err != nil {
		t.Fatalf("SETUP.md missing: %v", err)
	}
}

func TestWriteSkillDir_MissingSrc(t *testing.T) {
	_, err := writeSkillDir(map[string][]byte{
		"unrelated/file": []byte("x"),
	}, ".claude/skills/sprawl", t.TempDir())
	if err == nil {
		t.Fatal("expected error when source not in tarball")
	}
}

func TestWriteAgentFile(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "nested", "agents", "sprawl-bookkeeper.md")
	files := map[string][]byte{
		".claude/agents/sprawl-bookkeeper.md": []byte("---\nversion: 1.2.3\n---\nbody"),
	}
	v, err := writeAgentFile(files, ".claude/agents/sprawl-bookkeeper.md", dst)
	if err != nil {
		t.Fatalf("writeAgentFile: %v", err)
	}
	if v != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", v)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "---\nversion: 1.2.3\n---\nbody" {
		t.Fatalf("body mismatch: %q", got)
	}
}

func TestWriteTarget_DispatchesByKind(t *testing.T) {
	dst := t.TempDir()
	files := map[string][]byte{
		".claude/skills/sprawl/SKILL.md":      []byte("---\nversion: 0.1.0\n---\n"),
		".claude/agents/sprawl-bookkeeper.md": []byte("---\nversion: 0.2.0\n---\n"),
	}

	v, err := writeTarget(files, Target{Kind: "skill", SrcPath: ".claude/skills/sprawl", DstPath: filepath.Join(dst, "skill")})
	if err != nil {
		t.Fatalf("skill: %v", err)
	}
	if v != "0.1.0" {
		t.Fatalf("skill version = %q", v)
	}
	v, err = writeTarget(files, Target{Kind: "agent", SrcPath: ".claude/agents/sprawl-bookkeeper.md", DstPath: filepath.Join(dst, "agent.md")})
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	if v != "0.2.0" {
		t.Fatalf("agent version = %q", v)
	}
}
