package skill

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// installFixtureTarball builds a tarball containing the files
// `sprawl skill install` cares about, with version markers.
func installFixtureTarball(t *testing.T) []byte {
	t.Helper()
	files := map[string]string{
		".claude/skills/sprawl/SKILL.md":                     "---\nname: sprawl\nversion: \"0.5.0\"\n---\nbody\n",
		".claude/skills/sprawl/SETUP.md":                     "setup",
		".claude/agents/sprawl-bookkeeper.md":                "---\nversion: 0.6.0\n---\nclaude agent",
		".opencode/agents/sprawl-bookkeeper.md":              "---\nversion: 0.7.0\n---\nopencode agent",
		"internal/skill/assets/sprawl-bookkeeper.codex.toml": "# version: 0.8.0\nname = \"sprawl-bookkeeper\"\ndeveloper_instructions = \"x\"\n",
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
// returns it. The skill installer reads UserHomeDir; the config layer
// reads XDG_CONFIG_HOME — both must point at the scratch space so the
// test doesn't touch the real home.
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

// stubPrompts replaces the bubbletea-driven prompts with deterministic
// callbacks. Tests skip the TUI plumbing entirely; the prompt models are
// covered by prompt_test.go.
func stubPrompts(t *testing.T, choice Choice, proceed bool) {
	t.Helper()
	prevC, prevP := promptChoiceFunc, promptConfirmFunc
	promptChoiceFunc = func(_ io.Reader, _ io.Writer, _ string) (Choice, error) {
		return choice, nil
	}
	promptConfirmFunc = func(_ io.Reader, _ io.Writer, _ string) (bool, error) {
		return proceed, nil
	}
	t.Cleanup(func() {
		promptChoiceFunc = prevC
		promptConfirmFunc = prevP
	})
}

// stubPromptsCancelled simulates the user hitting esc/ctrl+c at the
// initial choice screen.
func stubPromptsCancelled(t *testing.T) {
	t.Helper()
	prevC, prevP := promptChoiceFunc, promptConfirmFunc
	promptChoiceFunc = func(_ io.Reader, _ io.Writer, _ string) (Choice, error) {
		return Choice{}, errPromptCancelled
	}
	t.Cleanup(func() {
		promptChoiceFunc = prevC
		promptConfirmFunc = prevP
	})
}

func TestInstall_GlobalAllTools_WritesFilesAndConfig(t *testing.T) {
	home := pinHomeAndConfig(t)
	pinTarballServer(t, installFixtureTarball(t))
	stubPrompts(t, Choice{
		What:  []string{"skill", "agent"},
		Tools: []string{"claude", "opencode", "codex"},
		Scope: "global",
	}, true)

	var stdout bytes.Buffer
	if err := Install(context.Background(), "/cwd-unused", strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Install: %v", err)
	}

	expectFiles := []string{
		filepath.Join(home, ".claude", "skills", "sprawl", "SKILL.md"),
		filepath.Join(home, ".config", "opencode", "skills", "sprawl", "SKILL.md"),
		filepath.Join(home, ".agents", "skills", "sprawl", "SKILL.md"),
		filepath.Join(home, ".claude", "agents", "sprawl-bookkeeper.md"),
		filepath.Join(home, ".config", "opencode", "agents", "sprawl-bookkeeper.md"),
		filepath.Join(home, ".codex", "agents", "sprawl-bookkeeper.toml"),
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
	if len(cfg.SkillInstalls) != 6 {
		t.Fatalf("SkillInstalls = %d, want 6 (%+v)", len(cfg.SkillInstalls), cfg.SkillInstalls)
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
			} else if inst.Tool == "codex" {
				want = "0.8.0"
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
	stubPrompts(t, Choice{
		What:  []string{"skill"},
		Tools: []string{"claude"},
		Scope: "local",
	}, true)

	var stdout bytes.Buffer
	if err := Install(context.Background(), cwd, strings.NewReader(""), &stdout); err != nil {
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
	stubPrompts(t, Choice{
		What:  []string{"skill"},
		Tools: []string{"claude"},
		Scope: "global",
	}, false) // user said no at confirm

	var stdout bytes.Buffer
	if err := Install(context.Background(), "/cwd", strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !strings.Contains(stdout.String(), "Aborted") {
		t.Fatalf("expected abort message, got %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "sprawl")); err == nil {
		t.Fatalf("skill dir created despite abort")
	}
}

func TestInstall_CancelAtChoice_NoFilesWritten(t *testing.T) {
	home := pinHomeAndConfig(t)
	pinTarballServer(t, installFixtureTarball(t))
	stubPromptsCancelled(t)

	var stdout bytes.Buffer
	if err := Install(context.Background(), "/cwd", strings.NewReader(""), &stdout); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !strings.Contains(stdout.String(), "Cancelled") {
		t.Fatalf("expected cancelled message, got %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "sprawl")); err == nil {
		t.Fatalf("skill dir created despite cancel")
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

	stubPrompts(t, Choice{
		What:  []string{"skill"},
		Tools: []string{"claude"},
		Scope: "global",
	}, true)

	var stdout bytes.Buffer
	err := Install(context.Background(), "/cwd", strings.NewReader(""), &stdout)
	if err == nil {
		t.Fatal("expected error on download failure")
	}
	if !strings.Contains(err.Error(), "download") {
		t.Fatalf("err = %v, want a download error", err)
	}
}
