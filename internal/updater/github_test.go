package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeTarball builds an in-memory gzip'd tarball with a single regular-file
// entry. Returns the bytes.
func makeTarball(t *testing.T, entryName string, contents []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name:     entryName,
		Mode:     0o755,
		Size:     int64(len(contents)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar write header: %v", err)
	}
	if _, err := tw.Write(contents); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// -- extractBinary ---------------------------------------------------------

func TestExtractBinary_HappyPath(t *testing.T) {
	want := []byte("fake-binary-bytes")
	tarPath := filepath.Join(t.TempDir(), "in.tar.gz")
	if err := os.WriteFile(tarPath, makeTarball(t, "sprawl", want), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := extractBinary(tarPath, "sprawl", dst); err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("contents mismatch:\nwant %q\ngot  %q", want, got)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestExtractBinary_MissingEntry(t *testing.T) {
	tarPath := filepath.Join(t.TempDir(), "in.tar.gz")
	if err := os.WriteFile(tarPath, makeTarball(t, "other", []byte("x")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractBinary(tarPath, "sprawl", filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("expected error when entry is missing")
	}
}

func TestExtractBinary_RejectsTraversal(t *testing.T) {
	// Build a tarball whose only entry is "../sprawl" — extractBinary should
	// not match it against entry name "sprawl".
	tarPath := filepath.Join(t.TempDir(), "in.tar.gz")
	if err := os.WriteFile(tarPath, makeTarball(t, "../sprawl", []byte("x")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractBinary(tarPath, "sprawl", filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("expected error: traversal entry must not match")
	}
}

// -- verifyChecksum --------------------------------------------------------

func TestVerifyChecksum_Match(t *testing.T) {
	dir := t.TempDir()
	tar := []byte("payload")
	tarPath := filepath.Join(dir, "sprawl.tar.gz")
	os.WriteFile(tarPath, tar, 0o644)
	cs := fmt.Sprintf("%s  sprawl.tar.gz\n", sha256Hex(tar))
	csPath := filepath.Join(dir, "checksums.txt")
	os.WriteFile(csPath, []byte(cs), 0o644)
	if err := verifyChecksum(csPath, tarPath, "sprawl.tar.gz"); err != nil {
		t.Fatalf("verifyChecksum: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	dir := t.TempDir()
	tar := []byte("payload")
	tarPath := filepath.Join(dir, "sprawl.tar.gz")
	os.WriteFile(tarPath, tar, 0o644)
	cs := "deadbeef" + strings.Repeat("0", 56) + "  sprawl.tar.gz\n"
	csPath := filepath.Join(dir, "checksums.txt")
	os.WriteFile(csPath, []byte(cs), 0o644)
	err := verifyChecksum(csPath, tarPath, "sprawl.tar.gz")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestVerifyChecksum_MissingEntry(t *testing.T) {
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "sprawl.tar.gz")
	os.WriteFile(tarPath, []byte("x"), 0o644)
	csPath := filepath.Join(dir, "checksums.txt")
	os.WriteFile(csPath, []byte("aaaa  other.tar.gz\n"), 0o644)
	if err := verifyChecksum(csPath, tarPath, "sprawl.tar.gz"); err == nil {
		t.Fatal("expected error when checksums.txt has no row for the asset")
	}
}

// -- atomicReplace ---------------------------------------------------------

func TestAtomicReplace_SwapsFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sprawl")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "stage")
	if err := os.WriteFile(src, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := atomicReplace(target, src); err != nil {
		t.Fatalf("atomicReplace: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW" {
		t.Fatalf("target = %q, want NEW", got)
	}
	// .old / .new should both be cleaned up on success.
	for _, p := range []string{target + ".old", target + ".new"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to be cleaned up, stat err = %v", p, err)
		}
	}
}

// -- end-to-end RunUpdate --------------------------------------------------

// fakeReleaseServer serves a /releases/latest endpoint, plus arbitrary
// asset paths backed by an in-memory map.
func fakeReleaseServer(t *testing.T, tag string, tarballName string, tarball []byte) *httptest.Server {
	t.Helper()
	checksums := fmt.Appendf(nil, "%s  %s\n", sha256Hex(tarball), tarballName)

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/releases/latest", repoOwner, repoName),
		func(w http.ResponseWriter, r *http.Request) {
			// Build asset URLs against the request host so the client can
			// reach them.
			base := "http://" + r.Host
			payload := map[string]any{
				"tag_name": tag,
				"assets": []map[string]any{
					{"name": tarballName, "browser_download_url": base + "/assets/" + tarballName},
					{"name": "checksums.txt", "browser_download_url": base + "/assets/checksums.txt"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(payload)
		})
	mux.HandleFunc("/assets/"+tarballName, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(tarball)
	})
	mux.HandleFunc("/assets/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Write(checksums)
	})
	return httptest.NewServer(mux)
}

func TestRunUpdate_DevBinaryRefuses(t *testing.T) {
	swapBuild(t, "sprawl_dev", "v0.1.0")
	var stdout, stderr bytes.Buffer
	if err := RunUpdate(context.Background(), &stdout, &stderr, strings.NewReader(""), true); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if !strings.Contains(stdout.String(), "sprawl_dev is built from source") {
		t.Fatalf("dev refusal missing: %q", stdout.String())
	}
}

func TestRunUpdate_LocalBuildRefuses(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0-1-gabc-dirty")
	var stdout, stderr bytes.Buffer
	if err := RunUpdate(context.Background(), &stdout, &stderr, strings.NewReader(""), true); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if !strings.Contains(stdout.String(), "local build") {
		t.Fatalf("local-build refusal missing: %q", stdout.String())
	}
}

func TestRunUpdate_AlreadyLatest(t *testing.T) {
	swapBuild(t, "sprawl", "v0.5.0")
	srv := fakeReleaseServer(t, "v0.5.0", "ignored", nil)
	t.Cleanup(srv.Close)
	swapBaseURL(t, srv.URL)

	var stdout, stderr bytes.Buffer
	if err := RunUpdate(context.Background(), &stdout, &stderr, strings.NewReader(""), true); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	if !strings.Contains(stdout.String(), "already the latest") {
		t.Fatalf("expected up-to-date message, got %q", stdout.String())
	}
}

func TestRunUpdate_SuccessReplacesBinary(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")

	// Stage a fake current binary in a temp dir.
	tmpExe := filepath.Join(t.TempDir(), "sprawl")
	if err := os.WriteFile(tmpExe, []byte("CURRENT"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevResolve := resolveExecutable
	resolveExecutable = func() (string, error) { return tmpExe, nil }
	t.Cleanup(func() { resolveExecutable = prevResolve })

	useTempConfig(t)

	tarballName := fmt.Sprintf("sprawl_0.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	tarball := makeTarball(t, "sprawl", []byte("NEW-BINARY"))
	srv := fakeReleaseServer(t, "v0.2.0", tarballName, tarball)
	t.Cleanup(srv.Close)
	swapBaseURL(t, srv.URL)

	var stdout, stderr bytes.Buffer
	if err := RunUpdate(context.Background(), &stdout, &stderr, strings.NewReader(""), true); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	got, err := os.ReadFile(tmpExe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW-BINARY" {
		t.Fatalf("binary not replaced; got %q", got)
	}
	if !strings.Contains(stdout.String(), "Updated sprawl 0.1.0 → 0.2.0") {
		t.Fatalf("missing success line: %q", stdout.String())
	}
}

func TestRunUpdate_ChecksumMismatchAborts(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")

	tmpExe := filepath.Join(t.TempDir(), "sprawl")
	original := []byte("CURRENT")
	if err := os.WriteFile(tmpExe, original, 0o755); err != nil {
		t.Fatal(err)
	}
	prevResolve := resolveExecutable
	resolveExecutable = func() (string, error) { return tmpExe, nil }
	t.Cleanup(func() { resolveExecutable = prevResolve })

	tarballName := fmt.Sprintf("sprawl_0.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	realTarball := makeTarball(t, "sprawl", []byte("NEW-BINARY"))

	// Build a server whose checksums.txt is for a *different* payload than
	// what the tarball endpoint actually returns.
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/releases/latest", repoOwner, repoName),
		func(w http.ResponseWriter, r *http.Request) {
			base := "http://" + r.Host
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.2.0",
				"assets": []map[string]any{
					{"name": tarballName, "browser_download_url": base + "/assets/tar"},
					{"name": "checksums.txt", "browser_download_url": base + "/assets/cs"},
				},
			})
		})
	mux.HandleFunc("/assets/tar", func(w http.ResponseWriter, r *http.Request) {
		w.Write(realTarball)
	})
	mux.HandleFunc("/assets/cs", func(w http.ResponseWriter, r *http.Request) {
		// Wrong sum: SHA256 of unrelated bytes.
		fmt.Fprintf(w, "%s  %s\n", sha256Hex([]byte("not the tarball")), tarballName)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	swapBaseURL(t, srv.URL)

	var stdout, stderr bytes.Buffer
	err := RunUpdate(context.Background(), &stdout, &stderr, strings.NewReader(""), true)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got %v", err)
	}
	got, _ := os.ReadFile(tmpExe)
	if !bytes.Equal(got, original) {
		t.Fatalf("original binary clobbered after checksum failure: got %q", got)
	}
}

func TestRunUpdate_PromptDecline(t *testing.T) {
	swapBuild(t, "sprawl", "v0.1.0")
	tmpExe := filepath.Join(t.TempDir(), "sprawl")
	original := []byte("CURRENT")
	os.WriteFile(tmpExe, original, 0o755)
	prevResolve := resolveExecutable
	resolveExecutable = func() (string, error) { return tmpExe, nil }
	t.Cleanup(func() { resolveExecutable = prevResolve })

	tarballName := fmt.Sprintf("sprawl_0.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	srv := fakeReleaseServer(t, "v0.2.0", tarballName, makeTarball(t, "sprawl", []byte("NEW")))
	t.Cleanup(srv.Close)
	swapBaseURL(t, srv.URL)

	var stdout, stderr bytes.Buffer
	if err := RunUpdate(context.Background(), &stdout, &stderr, strings.NewReader("n\n"), false); err != nil {
		t.Fatalf("RunUpdate: %v", err)
	}
	got, _ := os.ReadFile(tmpExe)
	if !bytes.Equal(got, original) {
		t.Fatalf("binary touched after declining prompt: %q", got)
	}
	if !strings.Contains(stderr.String(), "update cancelled") {
		t.Fatalf("missing cancel message: %q", stderr.String())
	}
}

func TestFindAsset(t *testing.T) {
	assets := []releaseAsset{
		{Name: "foo", DownloadURL: "u1"},
		{Name: "bar", DownloadURL: "u2"},
	}
	if got := findAsset(assets, "bar"); got != "u2" {
		t.Errorf("found %q, want u2", got)
	}
	if got := findAsset(assets, "missing"); got != "" {
		t.Errorf("missing should return empty, got %q", got)
	}
}
