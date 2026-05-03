package skill

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// buildTarball creates a gzipped tar with the given files under a
// `prefix/` top-level directory, mimicking GitHub's tarball layout.
func buildTarball(t *testing.T, prefix string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	// Top-level dir entry (GitHub includes one).
	if err := tw.WriteHeader(&tar.Header{
		Name:     prefix + "/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}); err != nil {
		t.Fatalf("tar dir: %v", err)
	}
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name:     prefix + "/" + name,
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(content)),
		}); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func TestExtractTarball_StripsPrefix(t *testing.T) {
	gz := buildTarball(t, "ultrakorne-sprawl_cli-abc123", map[string]string{
		".claude/skills/sprawl/SKILL.md":      "skill body",
		".claude/agents/sprawl-bookkeeper.md": "agent body",
	})
	got, err := extractTarball(gz)
	if err != nil {
		t.Fatalf("extractTarball: %v", err)
	}
	if string(got[".claude/skills/sprawl/SKILL.md"]) != "skill body" {
		t.Fatalf("SKILL.md = %q", got[".claude/skills/sprawl/SKILL.md"])
	}
	if string(got[".claude/agents/sprawl-bookkeeper.md"]) != "agent body" {
		t.Fatalf("agent .md = %q", got[".claude/agents/sprawl-bookkeeper.md"])
	}
	if _, exists := got["ultrakorne-sprawl_cli-abc123"]; exists {
		t.Fatal("prefix dir leaked into extracted map")
	}
}

func TestFetchMasterTarball_OK(t *testing.T) {
	want := buildTarball(t, "ultrakorne-sprawl_cli-deadbee", map[string]string{
		"README.md": "hi",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/ultrakorne/sprawl_cli/tarball/master" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(want)
	}))
	t.Cleanup(srv.Close)

	old := baseURL
	baseURL = srv.URL
	t.Cleanup(func() { baseURL = old })

	got, err := fetchMasterTarball(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("body mismatch (len got=%d want=%d)", len(got), len(want))
	}
}

func TestFetchMasterTarball_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	old := baseURL
	baseURL = srv.URL
	t.Cleanup(func() { baseURL = old })

	if _, err := fetchMasterTarball(context.Background()); err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}
