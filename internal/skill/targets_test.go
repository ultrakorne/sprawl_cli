package skill

import (
	"path/filepath"
	"testing"
)

func TestResolveTargets_GlobalScope_AllTools(t *testing.T) {
	got := ResolveTargets(Choice{
		What:  []string{"skill", "agent"},
		Tools: []string{"claude", "opencode", "codex"},
		Scope: "global",
	}, "/h", "/cwd")

	want := []Target{
		{Kind: "skill", Name: "sprawl", Tool: "claude", Scope: "global",
			SrcPath: ".claude/skills/sprawl",
			DstPath: filepath.Join("/h", ".claude", "skills", "sprawl")},
		{Kind: "skill", Name: "sprawl", Tool: "opencode", Scope: "global",
			SrcPath: ".claude/skills/sprawl",
			DstPath: filepath.Join("/h", ".config", "opencode", "skills", "sprawl")},
		{Kind: "skill", Name: "sprawl", Tool: "codex", Scope: "global",
			SrcPath: ".claude/skills/sprawl",
			DstPath: filepath.Join("/h", ".agents", "skills", "sprawl")},
		{Kind: "agent", Name: "sprawl-bookkeeper", Tool: "claude", Scope: "global",
			SrcPath: ".claude/agents/sprawl-bookkeeper.md",
			DstPath: filepath.Join("/h", ".claude", "agents", "sprawl-bookkeeper.md")},
		{Kind: "agent", Name: "sprawl-bookkeeper", Tool: "opencode", Scope: "global",
			SrcPath: ".opencode/agents/sprawl-bookkeeper.md",
			DstPath: filepath.Join("/h", ".config", "opencode", "agents", "sprawl-bookkeeper.md")},
		{Kind: "agent", Name: "sprawl-bookkeeper", Tool: "codex", Scope: "global",
			SrcPath: "internal/skill/assets/sprawl-bookkeeper.codex.toml",
			DstPath: filepath.Join("/h", ".codex", "agents", "sprawl-bookkeeper.toml")},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Target[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestResolveTargets_LocalScope_UsesCwd(t *testing.T) {
	got := ResolveTargets(Choice{
		What:  []string{"skill", "agent"},
		Tools: []string{"claude", "opencode", "codex"},
		Scope: "local",
	}, "/h", "/cwd")

	wantPaths := []string{
		filepath.Join("/cwd", ".claude", "skills", "sprawl"),
		filepath.Join("/cwd", ".opencode", "skills", "sprawl"),
		filepath.Join("/cwd", ".agents", "skills", "sprawl"),
		filepath.Join("/cwd", ".claude", "agents", "sprawl-bookkeeper.md"),
		filepath.Join("/cwd", ".opencode", "agents", "sprawl-bookkeeper.md"),
		filepath.Join("/cwd", ".codex", "agents", "sprawl-bookkeeper.toml"),
	}
	if len(got) != len(wantPaths) {
		t.Fatalf("len = %d, want %d", len(got), len(wantPaths))
	}
	for i, p := range wantPaths {
		if got[i].DstPath != p {
			t.Fatalf("Target[%d].DstPath = %q, want %q", i, got[i].DstPath, p)
		}
		if got[i].Scope != "local" {
			t.Fatalf("Target[%d].Scope = %q, want local", i, got[i].Scope)
		}
	}
}

func TestResolveTargets_PartialSelection(t *testing.T) {
	got := ResolveTargets(Choice{
		What:  []string{"agent"},
		Tools: []string{"claude"},
		Scope: "global",
	}, "/h", "/cwd")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Kind != "agent" || got[0].Tool != "claude" {
		t.Fatalf("Target = %+v", got[0])
	}
}

func TestResolveTargets_AgentSrcDiffersByTool(t *testing.T) {
	got := ResolveTargets(Choice{
		What:  []string{"agent"},
		Tools: []string{"claude", "opencode", "codex"},
		Scope: "global",
	}, "/h", "/cwd")
	if got[0].SrcPath != ".claude/agents/sprawl-bookkeeper.md" {
		t.Fatalf("claude agent src = %q", got[0].SrcPath)
	}
	if got[1].SrcPath != ".opencode/agents/sprawl-bookkeeper.md" {
		t.Fatalf("opencode agent src = %q", got[1].SrcPath)
	}
	if got[2].SrcPath != "internal/skill/assets/sprawl-bookkeeper.codex.toml" {
		t.Fatalf("codex agent src = %q", got[2].SrcPath)
	}
}
