package skill

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ultrakorne/sprawl_cli/internal/build"
)

// baseURL is the GitHub API root. Tests override it to point at an
// httptest.Server.
var baseURL = "https://api.github.com"

const (
	repoOwner = "ultrakorne"
	repoName  = "sprawl_cli"

	downloadTimeout = 30 * time.Second
)

// fetchMasterTarball downloads the gzipped tarball of the repo's master
// branch from GitHub. The default HTTP client follows the 302 to codeload
// transparently.
func fetchMasterTarball(ctx context.Context) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/tarball/master", baseURL, repoOwner, repoName)
	reqCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sprawl-cli/"+build.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github tarball: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// extractTarball returns a map from repo-relative path to file content.
// Directories and other special tar entries are skipped — install only
// needs file bytes; destination dirs are created on write.
//
// GitHub wraps the repo in a top-level prefix dir like
// `ultrakorne-sprawl_cli-<sha>/`; we strip whatever the first path segment
// is from each entry so callers see clean repo-relative paths.
func extractTarball(gz []byte) (map[string][]byte, error) {
	gzr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	files := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		parts := strings.SplitN(hdr.Name, "/", 2)
		if len(parts) != 2 || parts[1] == "" {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		files[parts[1]] = data
	}
	return files, nil
}
