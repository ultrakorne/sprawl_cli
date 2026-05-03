package skill

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

// installFixtureTarball builds a tarball containing the four files
// `sprawl skill install` cares about, with version frontmatter.
func installFixtureTarball(t *testing.T) []byte {
	t.Helper()
	files := map[string]string{
		".claude/skills/sprawl/SKILL.md":        "---\nname: sprawl\nversion: \"0.5.0\"\n---\nbody\n",
		".claude/skills/sprawl/SETUP.md":        "setup",
		".claude/agents/sprawl-bookkeeper.md":   "---\nversion: 0.6.0\n---\nclaude agent",
		".opencode/agents/sprawl-bookkeeper.md": "---\nversion: 0.7.0\n---\nopencode agent",
	}
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{Name: "prefix/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatalf("tar: %v", err)
	}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: "prefix/" + name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("tar: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	return buf.Bytes()
}

// pinHomeAndConfig redirects HOME and XDG_CONFIG_HOME to a scratch dir and
// returns it. The skill installer reads UserHomeDir; the config layer reads
// XDG_CONFIG_HOME — both must point at the scratch space so the test
// doesn't touch the real home.
func pinHomeAndConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	return dir
}

func pinTarballServer(t *testing.T, body []byte) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	old := baseURL
	baseURL = srv.URL
	t.Cleanup(func() { baseURL = old })
}

func TestInstall_GlobalAllTools_WritesFilesAndConfig(t *testing.T) {
	home := pinHomeAndConfig(t)
	pinTarballServer(t, installFixtureTarball(t))

	// Blank = both items, blank = both tools, "1" = global, "y" = confirm.
	stdin := strings.NewReader("\n\n1\ny\n")
	var stdout bytes.Buffer

	if err := Install(context.Background(), "/cwd-unused", stdin, &stdout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	expectFiles := []string{
		filepath.Join(home, ".claude", "skills", "sprawl", "SKILL.md"),
		filepath.Join(home, ".config", "opencode", "skills", "sprawl", "SKILL.md"),
		filepath.Join(home, ".claude", "agents", "sprawl-bookkeeper.md"),
		filepath.Join(home, ".config", "opencode", "agents", "sprawl-bookkeeper.md"),
	}
	for _, p := range expectFiles {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}

	cfg, err := config.Load(build.AppName)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SkillInstalls) != 4 {
		t.Fatalf("SkillInstalls = %d, want 4 (%+v)", len(cfg.SkillInstalls), cfg.SkillInstalls)
	}
	for _, inst := range cfg.SkillInstalls {
		switch inst.Kind {
		case "skill":
			if inst.Version != "0.5.0" {
				t.Fatalf("skill version = %q, want 0.5.0", inst.Version)
			}
		case "agent":
			want := "0.6.0"
			if inst.Tool == "opencode" {
				want = "0.7.0"
			}
			if inst.Version != want {
				t.Fatalf("agent (%s) version = %q, want %s", inst.Tool, inst.Version, want)
			}
		}
	}
}

func TestInstall_LocalScope_UsesCwd(t *testing.T) {
	pinHomeAndConfig(t)
	pinTarballServer(t, installFixtureTarball(t))
	cwd := t.TempDir()

	// "1" = skill only, "1" = claude only, "2" = local, "y" = confirm.
	stdin := strings.NewReader("1\n1\n2\ny\n")
	var stdout bytes.Buffer
	if err := Install(context.Background(), cwd, stdin, &stdout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	dst := filepath.Join(cwd, ".claude", "skills", "sprawl", "SKILL.md")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("local skill missing at %s: %v", dst, err)
	}
}

func TestInstall_AbortAtConfirm_NoFilesWritten(t *testing.T) {
	home := pinHomeAndConfig(t)
	pinTarballServer(t, installFixtureTarball(t))

	stdin := strings.NewReader("\n\n1\nn\n")
	var stdout bytes.Buffer
	if err := Install(context.Background(), "/cwd", stdin, &stdout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !strings.Contains(stdout.String(), "Aborted") {
		t.Fatalf("expected abort message, got %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "sprawl")); err == nil {
		t.Fatalf("skill dir created despite abort")
	}
}

func TestInstall_DownloadFailure_Surfaces(t *testing.T) {
	pinHomeAndConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	old := baseURL
	baseURL = srv.URL
	t.Cleanup(func() { baseURL = old })

	stdin := strings.NewReader("\n\n1\ny\n")
	var stdout bytes.Buffer
	err := Install(context.Background(), "/cwd", stdin, &stdout)
	if err == nil {
		t.Fatal("expected error on download failure")
	}
	if !strings.Contains(err.Error(), "download") {
		t.Fatalf("err = %v, want a download error", err)
	}
}
