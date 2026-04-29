package updater

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/ultrakorne/sprawl_cli/internal/build"
)

// downloadTimeout is the per-request timeout for tarball + checksums.txt
// fetches. Longer than the metadata fetch because these are larger payloads.
const downloadTimeout = 60 * time.Second

type releaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

type releasePayload struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

// RunUpdate is the body of the `sprawl update` cobra command. It returns
// nil for friendly no-op exits (dev binary, local build, already-latest,
// user-cancelled prompt) and an error for anything that should exit 1.
func RunUpdate(ctx context.Context, stdout, stderr io.Writer, stdin io.Reader, autoYes bool) error {
	if build.AppName != "sprawl" {
		fmt.Fprintln(stdout, "sprawl_dev is built from source; use `make build-dev`.")
		return nil
	}
	if !IsReleaseVersion(build.Version) {
		fmt.Fprintf(stdout, "local build (version=%s); install a release before running update.\n", fallback(build.Version, "(unset)"))
		return nil
	}

	rel, err := fetchRelease(ctx)
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}
	if !semver.IsValid(rel.TagName) {
		return fmt.Errorf("release tag %q is not semver", rel.TagName)
	}
	if semver.Compare(rel.TagName, build.Version) <= 0 {
		fmt.Fprintf(stdout, "sprawl %s is already the latest.\n", strings.TrimPrefix(build.Version, "v"))
		return nil
	}

	tarballName := fmt.Sprintf("sprawl_%s_%s_%s.tar.gz", strings.TrimPrefix(rel.TagName, "v"), runtime.GOOS, runtime.GOARCH)
	tarballURL := findAsset(rel.Assets, tarballName)
	if tarballURL == "" {
		return fmt.Errorf("no release asset matches %s", tarballName)
	}
	checksumsURL := findAsset(rel.Assets, "checksums.txt")
	if checksumsURL == "" {
		return fmt.Errorf("release is missing checksums.txt")
	}

	if !autoYes {
		fmt.Fprintf(stderr, "Update sprawl %s → %s? [y/N] ",
			strings.TrimPrefix(build.Version, "v"),
			strings.TrimPrefix(rel.TagName, "v"))
		if !confirm(stdin) {
			fmt.Fprintln(stderr, "update cancelled")
			return nil
		}
	}

	tmpDir, err := os.MkdirTemp("", "sprawl-update-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, tarballName)
	if err := downloadFile(ctx, tarballURL, tarballPath); err != nil {
		return fmt.Errorf("download %s: %w", tarballName, err)
	}
	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := downloadFile(ctx, checksumsURL, checksumsPath); err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}

	if err := verifyChecksum(checksumsPath, tarballPath, tarballName); err != nil {
		return err
	}

	binPath := filepath.Join(tmpDir, "sprawl")
	if err := extractBinary(tarballPath, "sprawl", binPath); err != nil {
		return err
	}

	target, err := resolveExecutable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	if err := atomicReplace(target, binPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	removeCache()
	fmt.Fprintf(stdout, "Updated sprawl %s → %s.\n",
		strings.TrimPrefix(build.Version, "v"),
		strings.TrimPrefix(rel.TagName, "v"))
	return nil
}

// resolveExecutable is a package var so tests can stub it without writing
// over the real /proc/self/exe lookup. We fail loudly on EvalSymlinks
// errors rather than fall back to the unresolved path: on macOS
// os.Executable() can return a symlink (e.g. /usr/local/bin/sprawl ->
// /opt/homebrew/Cellar/sprawl/<v>/bin/sprawl), and silently renaming the
// symlink would leave the package-manager-owned target stale while
// reporting success.
var resolveExecutable = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func fetchRelease(ctx context.Context) (*releasePayload, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", baseURL, repoOwner, repoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sprawl-cli/"+build.Version)

	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var rel releasePayload
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func findAsset(assets []releaseAsset, name string) string {
	for _, a := range assets {
		if a.Name == name {
			return a.DownloadURL
		}
	}
	return ""
}

func downloadFile(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "sprawl-cli/"+build.Version)

	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

// verifyChecksum reads checksums.txt (goreleaser format: "<hex>  <name>"),
// finds the row for tarballName, and compares it against the SHA256 of
// tarballPath.
func verifyChecksum(checksumsPath, tarballPath, tarballName string) error {
	want, err := readChecksum(checksumsPath, tarballName)
	if err != nil {
		return err
	}

	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s (got %s, want %s)", tarballName, got, want)
	}
	return nil
}

func readChecksum(path, name string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		// goreleaser sometimes prefixes with "*" (binary-mode flag); strip it.
		fname := strings.TrimPrefix(parts[1], "*")
		if fname == name {
			return parts[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

// extractBinary pulls a single regular-file entry named `entryName` from a
// gzip'd tarball at `tarballPath` and writes it to `dst` with mode 0755.
func extractBinary(tarballPath, entryName, dst string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gunzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		// Reject anything that looks like a path-traversal attempt.
		clean := filepath.Clean(hdr.Name)
		if clean != entryName {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("tar entry %s is not a regular file", entryName)
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("tarball missing %s entry", entryName)
}

// atomicReplace swaps the file at `target` with the contents of `newPath`.
// The classic POSIX self-update dance: copy bytes next to the target,
// rename the running binary aside, rename the new bytes into place, then
// best-effort remove the side-aside copy. If the new-bytes rename fails
// after the side-aside, restore the original so we never leave the user
// without a binary.
func atomicReplace(target, newPath string) error {
	dir := filepath.Dir(target)
	tmpNext := target + ".new"
	tmpOld := target + ".old"

	if err := copyFile(newPath, tmpNext, 0o755); err != nil {
		return fmt.Errorf("stage new binary in %s: %w", dir, err)
	}
	cleanup := tmpNext
	defer func() {
		if cleanup != "" {
			os.Remove(cleanup)
		}
	}()

	if err := os.Rename(target, tmpOld); err != nil {
		return fmt.Errorf("rename current binary aside: %w", err)
	}
	if err := os.Rename(tmpNext, target); err != nil {
		// rollback: restore the original binary so the user isn't stranded.
		if rbErr := os.Rename(tmpOld, target); rbErr != nil {
			return fmt.Errorf("install new binary: %w (rollback also failed: %v)", err, rbErr)
		}
		return fmt.Errorf("install new binary: %w (rolled back)", err)
	}
	cleanup = "" // tmpNext was renamed into place; nothing to remove
	_ = os.Remove(tmpOld)
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// confirm returns true when the user types y/yes (case-insensitive). EOF
// or any other input is treated as "no".
func confirm(stdin io.Reader) bool {
	if stdin == nil {
		return false
	}
	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
