package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ultrakorne/sprawl_cli/internal/build"
)

// swapBuild overrides build.AppName / build.Version for the duration of the
// test. These are package vars set by ldflags, so direct assignment is fine.
func swapBuild(t *testing.T, app, version string) {
	t.Helper()
	prevApp, prevVer := build.AppName, build.Version
	build.AppName, build.Version = app, version
	t.Cleanup(func() {
		build.AppName, build.Version = prevApp, prevVer
	})
}

// useTempConfig points config.Dir at a temp dir so notify cache I/O
// doesn't touch the real ~/.config.
func useTempConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func swapBaseURL(t *testing.T, url string) {
	t.Helper()
	prev := baseURL
	baseURL = url
	t.Cleanup(func() { baseURL = prev })
}

// -- IsReleaseVersion -------------------------------------------------------

func TestIsReleaseVersion(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"v0.1.0", true},
		{"v1.2.3", true},
		{"", false},
		{"dev", false},
		{"v0.1.0-1-gabc123-dirty", false}, // git describe output
		{"v0.1.0-rc1", false},
		{"v0.1.0+meta", false},
		{"0.1.0", false}, // no v prefix → not valid semver per x/mod
		{"garbage", false},
	}
	for _, c := range cases {
		if got := IsReleaseVersion(c.v); got != c.want {
			t.Errorf("IsReleaseVersion(%q) = %v, want %v", c.v, got, c.want)
		}
	}
}

// -- shouldCheck gating -----------------------------------------------------

func TestShouldCheck_DevBinary(t *testing.T) {
	swapBuild(t, "sprawl_dev", "v0.1.0")
	if shouldCheck() {
		t.Fatal("dev binary must skip update check")
	}
}

func TestShouldCheck_DirtyVersion(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0-1-gabc-dirty")
	if shouldCheck() {
		t.Fatal("dirty version must skip update check")
	}
}

func TestShouldCheck_EnvOptOut(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")
	t.Setenv("SPRAWL_NO_UPDATE_CHECK", "1")
	if shouldCheck() {
		t.Fatal("SPRAWL_NO_UPDATE_CHECK=1 must skip update check")
	}
}

func TestShouldCheck_HappyPath(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")
	t.Setenv("SPRAWL_NO_UPDATE_CHECK", "")
	if !shouldCheck() {
		t.Fatal("clean release on prod binary should run check")
	}
}

// -- banner printer --------------------------------------------------------

func TestPrintBanner_PrintsWhenNewer(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")
	var buf bytes.Buffer
	printBannerIfNewer(&buf, "v0.2.0")
	out := buf.String()
	if !strings.Contains(out, "sprawl 0.2.0 available") {
		t.Fatalf("banner missing: %q", out)
	}
	if !strings.Contains(out, "current: 0.1.0") {
		t.Fatalf("banner missing current: %q", out)
	}
	if !strings.Contains(out, "sprawl update") {
		t.Fatalf("banner missing call to action: %q", out)
	}
}

func TestPrintBanner_NoColorOnNonTTY(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")
	var buf bytes.Buffer
	printBannerIfNewer(&buf, "v0.2.0")
	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("non-TTY writer should produce plain text, got %q", buf.String())
	}
}

func TestPrintBanner_SilentWhenSameOrOlder(t *testing.T) {
	swapBuild(t, "sprawl", "v0.2.0")
	for _, latest := range []string{"v0.2.0", "v0.1.0", ""} {
		var buf bytes.Buffer
		printBannerIfNewer(&buf, latest)
		if buf.Len() != 0 {
			t.Errorf("expected silence for latest=%q, got %q", latest, buf.String())
		}
	}
}

// -- cache I/O -------------------------------------------------------------

func TestCacheRoundtrip(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")
	dir := useTempConfig(t)
	path, err := cacheFilePath()
	if err != nil {
		t.Fatalf("cacheFilePath: %v", err)
	}
	want := updateCache{CheckedAt: time.Now().UTC().Truncate(time.Second), LatestVersion: "v0.2.0"}
	if err := writeCache(path, want); err != nil {
		t.Fatalf("writeCache: %v", err)
	}

	// File should land under the configured XDG dir.
	if !strings.HasPrefix(path, dir) {
		t.Errorf("cache file %q not under XDG dir %q", path, dir)
	}

	got, ok := readCache(path)
	if !ok {
		t.Fatal("readCache reported not-ok")
	}
	if got.LatestVersion != want.LatestVersion {
		t.Errorf("LatestVersion: got %q, want %q", got.LatestVersion, want.LatestVersion)
	}
	if !got.CheckedAt.Equal(want.CheckedAt) {
		t.Errorf("CheckedAt: got %v, want %v", got.CheckedAt, want.CheckedAt)
	}
}

func TestReadCache_MissingReturnsNotOK(t *testing.T) {
	if _, ok := readCache(filepath.Join(t.TempDir(), "nope.json")); ok {
		t.Fatal("expected !ok for missing file")
	}
}

// -- MaybeNotify -----------------------------------------------------------

func TestMaybeNotify_DevBinaryDoesNothing(t *testing.T) {
	swapBuild(t, "sprawl_dev", "v0.1.0")
	useTempConfig(t)
	swapBaseURL(t, "http://127.0.0.1:1") // would fail any HTTP call
	var buf bytes.Buffer
	if err := MaybeNotify(context.Background(), &buf); err != nil {
		t.Fatalf("MaybeNotify: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("dev binary printed banner: %q", buf.String())
	}
}

func TestMaybeNotify_FreshCacheSkipsNetwork(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")
	useTempConfig(t)
	swapBaseURL(t, "http://127.0.0.1:1") // ensure no network call

	// Pre-populate cache with a recent successful check.
	path, _ := cacheFilePath()
	writeCache(path, updateCache{
		CheckedAt:     time.Now().UTC(),
		LatestVersion: "v0.5.0",
	})

	var buf bytes.Buffer
	if err := MaybeNotify(context.Background(), &buf); err != nil {
		t.Fatalf("MaybeNotify: %v", err)
	}
	if !strings.Contains(buf.String(), "0.5.0") {
		t.Fatalf("expected banner with cached version, got %q", buf.String())
	}
}

func TestMaybeNotify_StaleCacheRefreshes(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")
	useTempConfig(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.9.0"})
	}))
	t.Cleanup(srv.Close)
	swapBaseURL(t, srv.URL)

	// Stale cache: more than 24h old, with a stale latest_version.
	path, _ := cacheFilePath()
	writeCache(path, updateCache{
		CheckedAt:     time.Now().UTC().Add(-48 * time.Hour),
		LatestVersion: "v0.5.0",
	})

	var buf bytes.Buffer
	if err := MaybeNotify(context.Background(), &buf); err != nil {
		t.Fatalf("MaybeNotify: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "0.9.0") {
		t.Fatalf("expected refreshed v0.9.0 in banner, got %q", out)
	}

	// Cache should be rewritten with the new tag.
	got, ok := readCache(path)
	if !ok {
		t.Fatal("cache disappeared")
	}
	if got.LatestVersion != "v0.9.0" {
		t.Fatalf("cache LatestVersion = %q, want v0.9.0", got.LatestVersion)
	}
}

func TestMaybeNotify_NetworkErrorIsSilent(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")
	useTempConfig(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	swapBaseURL(t, srv.URL)

	var buf bytes.Buffer
	if err := MaybeNotify(context.Background(), &buf); err != nil {
		t.Fatalf("MaybeNotify must not return error, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected silent failure, got %q", buf.String())
	}

	// Cache should still have been written so we back off until next window.
	path, _ := cacheFilePath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache written even on failure: %v", err)
	}
}

func TestMaybeNotify_OptOutEnvSilent(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")
	useTempConfig(t)
	t.Setenv("SPRAWL_NO_UPDATE_CHECK", "1")

	// If the gate was wrong this would 5xx; instead it must short-circuit.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not have been called")
	}))
	t.Cleanup(srv.Close)
	swapBaseURL(t, srv.URL)

	var buf bytes.Buffer
	if err := MaybeNotify(context.Background(), &buf); err != nil {
		t.Fatalf("MaybeNotify: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected silence with opt-out, got %q", buf.String())
	}
}

// -- fetchLatestTag --------------------------------------------------------

func TestFetchLatestTag_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := fmt.Sprintf("/repos/%s/%s/releases/latest", repoOwner, repoName)
		if r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		if accept := r.Header.Get("Accept"); accept != "application/vnd.github+json" {
			t.Errorf("Accept = %q", accept)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.2.3"})
	}))
	t.Cleanup(srv.Close)
	swapBaseURL(t, srv.URL)

	tag, err := fetchLatestTag(context.Background(), 2*time.Second)
	if err != nil {
		t.Fatalf("fetchLatestTag: %v", err)
	}
	if tag != "v1.2.3" {
		t.Fatalf("tag = %q", tag)
	}
}

func TestFetchLatestTag_RejectsNonSemver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "garbage"})
	}))
	t.Cleanup(srv.Close)
	swapBaseURL(t, srv.URL)
	if _, err := fetchLatestTag(context.Background(), 2*time.Second); err == nil {
		t.Fatal("expected error on non-semver tag")
	}
}
