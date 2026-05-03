package skill

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// pinRawServer points rawBaseURL at a stub serving SKILL.md / agent .md
// frontmatter. versions is keyed by repo path.
func pinRawServer(t *testing.T, versions map[string]string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// URL is /<owner>/<repo>/master/<repoPath>
		parts := strings.SplitN(r.URL.Path, "/master/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		v, ok := versions[parts[1]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("---\nversion: \"" + v + "\"\n---\n"))
	}))
	t.Cleanup(srv.Close)
	old := rawBaseURL
	rawBaseURL = srv.URL
	t.Cleanup(func() { rawBaseURL = old })
}

func TestFetchRemoteVersions(t *testing.T) {
	pinRawServer(t, map[string]string{
		".claude/skills/sprawl/SKILL.md":        "0.5.0",
		".claude/agents/sprawl-bookkeeper.md":   "0.6.0",
		".opencode/agents/sprawl-bookkeeper.md": "0.7.0",
	})
	rv := FetchRemoteVersions(context.Background())
	if rv.Skill != "0.5.0" || rv.ClaudeAgent != "0.6.0" || rv.OpenCodeAgent != "0.7.0" {
		t.Fatalf("RemoteVersions = %+v", rv)
	}
}

func TestFetchRemoteVersions_PartialFailure_ReturnsEmpty(t *testing.T) {
	pinRawServer(t, map[string]string{
		".claude/skills/sprawl/SKILL.md": "0.5.0",
		// other two missing → 404 → ""
	})
	rv := FetchRemoteVersions(context.Background())
	if rv.Skill != "0.5.0" {
		t.Fatalf("Skill = %q", rv.Skill)
	}
	if rv.ClaudeAgent != "" || rv.OpenCodeAgent != "" {
		t.Fatalf("expected empty strings on missing files, got %+v", rv)
	}
}

func TestRemoteVersions_VersionFor(t *testing.T) {
	rv := RemoteVersions{Skill: "1", ClaudeAgent: "2", OpenCodeAgent: "3"}
	if v := rv.VersionFor("skill", "claude"); v != "1" {
		t.Fatalf("skill/claude = %q", v)
	}
	if v := rv.VersionFor("agent", "claude"); v != "2" {
		t.Fatalf("agent/claude = %q", v)
	}
	if v := rv.VersionFor("agent", "opencode"); v != "3" {
		t.Fatalf("agent/opencode = %q", v)
	}
	if v := rv.VersionFor("nope", "claude"); v != "" {
		t.Fatalf("unknown kind = %q, want empty", v)
	}
}

func TestUpdate_NoInstalls(t *testing.T) {
	pinHomeAndConfig(t)
	pinRawServer(t, map[string]string{})
	var out bytes.Buffer
	if err := Update(context.Background(), &out); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !strings.Contains(out.String(), "No skill installs recorded") {
		t.Fatalf("expected no-installs message, got %q", out.String())
	}
}

func TestUpdate_AllUpToDate(t *testing.T) {
	pinHomeAndConfig(t)
	pinRawServer(t, map[string]string{
		".claude/skills/sprawl/SKILL.md":      "0.5.0",
		".claude/agents/sprawl-bookkeeper.md": "0.6.0",
	})
	cfg := &config.Config{SkillInstalls: []config.SkillInstall{
		{Kind: "skill", Name: "sprawl", Tool: "claude", Scope: "global", Path: "/somewhere/skill", Version: "0.5.0"},
		{Kind: "agent", Name: "sprawl-bookkeeper", Tool: "claude", Scope: "global", Path: "/somewhere/agent.md", Version: "0.6.0"},
	}}
	if err := config.Save(build.AppName, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var out bytes.Buffer
	if err := Update(context.Background(), &out); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Fatalf("expected up-to-date markers, got %q", out.String())
	}
	if strings.Contains(out.String(), "Fetching latest") {
		t.Fatalf("tarball fetched despite all up to date: %q", out.String())
	}
}

func TestUpdate_StaleInstall_RewritesAndUpdatesConfig(t *testing.T) {
	home := pinHomeAndConfig(t)
	pinRawServer(t, map[string]string{
		".claude/skills/sprawl/SKILL.md": "0.5.0",
	})
	pinTarballServer(t, installFixtureTarball(t))

	skillDst := filepath.Join(home, ".claude", "skills", "sprawl")
	cfg := &config.Config{SkillInstalls: []config.SkillInstall{
		{Kind: "skill", Name: "sprawl", Tool: "claude", Scope: "global", Path: skillDst, Version: "0.1.0"},
	}}
	if err := config.Save(build.AppName, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var out bytes.Buffer
	if err := Update(context.Background(), &out); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// File on disk now matches fixture content.
	got, err := os.ReadFile(filepath.Join(skillDst, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.Contains(string(got), "version: \"0.5.0\"") {
		t.Fatalf("SKILL.md not refreshed: %q", got)
	}

	// Config record bumped to the new version.
	loaded, err := config.Load(build.AppName)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SkillInstalls[0].Version != "0.5.0" {
		t.Fatalf("recorded version = %q, want 0.5.0", loaded.SkillInstalls[0].Version)
	}
}

func TestUpdate_RemoteProbeMisses_SkipsRecord(t *testing.T) {
	pinHomeAndConfig(t)
	// Empty raw server → all probes return "".
	pinRawServer(t, map[string]string{})

	cfg := &config.Config{SkillInstalls: []config.SkillInstall{
		{Kind: "skill", Name: "sprawl", Tool: "claude", Scope: "global", Path: "/x", Version: "0.1.0"},
	}}
	if err := config.Save(build.AppName, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var out bytes.Buffer
	if err := Update(context.Background(), &out); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !strings.Contains(out.String(), "couldn't probe") {
		t.Fatalf("expected probe-skip message, got %q", out.String())
	}
}
