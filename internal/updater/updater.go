// Package updater handles the prod sprawl binary's once-per-day version
// notice and the explicit `sprawl update` flow. The dev binary (sprawl_dev,
// AppName != "sprawl") and any non-release Version (e.g. `git describe`
// output for dirty/post-tag commits) skip every code path here.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/config"
	"github.com/ultrakorne/sprawl_cli/internal/skill"
)

// baseURL is the GitHub API root. Tests override this to point at an
// httptest.Server. There is no user-visible flag.
var baseURL = "https://api.github.com"

// fetchRemoteSkillVersions is the seam MaybeNotify uses to probe skill /
// agent frontmatter on master. Pinned as a var so updater tests can swap
// it for a stub without depending on the skill package's internal hosts.
var fetchRemoteSkillVersions = skill.FetchRemoteVersions

const (
	repoOwner = "ultrakorne"
	repoName  = "sprawl_cli"

	notifyTimeout = 2 * time.Second
	cacheWindow   = 24 * time.Hour

	cacheFileName = "update_check.json"
)

// IsReleaseVersion reports whether v is a clean release tag (vX.Y.Z with no
// pre-release or build suffix). False for "dev", "", or `git describe`
// output like "v0.1.0-1-gabc123-dirty" — we use it to gate every
// auto-update behaviour so local builds stay quiet.
//
// Accepts both "v0.2.0" and "0.2.0" forms: goreleaser strips the leading "v"
// from `{{.Version}}` by default, so binaries shipped before the goreleaser
// config was tightened bake in an un-prefixed version. Without normalisation
// those releases would mis-classify as local builds and `sprawl update`
// would refuse to run.
func IsReleaseVersion(v string) bool {
	if v == "" || v == "dev" {
		return false
	}
	c := canonicalVersion(v)
	if !semver.IsValid(c) {
		return false
	}
	return semver.Prerelease(c) == "" && semver.Build(c) == ""
}

// canonicalVersion returns v with a leading "v" if it would otherwise be
// valid semver. Non-semver inputs are returned unchanged so the caller's
// own validation can reject them.
func canonicalVersion(v string) string {
	if v == "" {
		return v
	}
	if strings.HasPrefix(v, "v") {
		return v
	}
	if semver.IsValid("v" + v) {
		return "v" + v
	}
	return v
}

type updateCache struct {
	CheckedAt                  time.Time `json:"checked_at"`
	LatestVersion              string    `json:"latest_version"`
	LatestSkillVersion         string    `json:"latest_skill_version,omitempty"`
	LatestClaudeAgentVersion   string    `json:"latest_claude_agent_version,omitempty"`
	LatestOpenCodeAgentVersion string    `json:"latest_opencode_agent_version,omitempty"`
	LatestCodexAgentVersion    string    `json:"latest_codex_agent_version,omitempty"`
}

// MaybeNotify is invoked from the root command's PersistentPreRunE. It
// always returns nil; any failure (network, disk, parse) is silent so the
// banner can never block or noise up the user's actual command. The CLI
// release tag and the master-branch skill/agent version markers are
// probed together and cached for 24h.
func MaybeNotify(ctx context.Context, stderr io.Writer) error {
	if !shouldCheck() {
		return nil
	}

	cachePath, err := cacheFilePath()
	if err != nil {
		return nil
	}

	cached, cachedOK := readCache(cachePath)
	if cachedOK && time.Since(cached.CheckedAt) < cacheWindow {
		printBanners(stderr, cached)
		return nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	latest := fetchLatestQuiet(ctx)
	rv := fetchRemoteSkillVersions(probeCtx)

	// Always rewrite the cache so we back off for the next 24h, even on
	// failure. Preserve any prior values so a transient network hiccup
	// doesn't lose previously-known versions.
	next := updateCache{
		CheckedAt:                  time.Now().UTC(),
		LatestVersion:              latest,
		LatestSkillVersion:         rv.Skill,
		LatestClaudeAgentVersion:   rv.ClaudeAgent,
		LatestOpenCodeAgentVersion: rv.OpenCodeAgent,
		LatestCodexAgentVersion:    rv.CodexAgent,
	}
	if cachedOK {
		if next.LatestVersion == "" {
			next.LatestVersion = cached.LatestVersion
		}
		if next.LatestSkillVersion == "" {
			next.LatestSkillVersion = cached.LatestSkillVersion
		}
		if next.LatestClaudeAgentVersion == "" {
			next.LatestClaudeAgentVersion = cached.LatestClaudeAgentVersion
		}
		if next.LatestOpenCodeAgentVersion == "" {
			next.LatestOpenCodeAgentVersion = cached.LatestOpenCodeAgentVersion
		}
		if next.LatestCodexAgentVersion == "" {
			next.LatestCodexAgentVersion = cached.LatestCodexAgentVersion
		}
	}
	_ = writeCache(cachePath, next)

	printBanners(stderr, next)
	return nil
}

// printBanners emits the CLI banner (when newer) and a single skill/agent
// banner if any recorded install is older than the corresponding remote.
func printBanners(stderr io.Writer, c updateCache) {
	if c.LatestVersion != "" {
		printBannerIfNewer(stderr, c.LatestVersion)
	}
	cfg, err := config.Load(build.AppName)
	if err != nil || len(cfg.SkillInstalls) == 0 {
		return
	}
	skillStale := hasStaleInstall(cfg.SkillInstalls, "skill", "", c.LatestSkillVersion)
	agentStale := hasStaleInstall(cfg.SkillInstalls, "agent", "claude", c.LatestClaudeAgentVersion) ||
		hasStaleInstall(cfg.SkillInstalls, "agent", "opencode", c.LatestOpenCodeAgentVersion) ||
		hasStaleInstall(cfg.SkillInstalls, "agent", "codex", c.LatestCodexAgentVersion)

	switch {
	case skillStale && agentStale:
		printSkillBanner(stderr, "sprawl skill + sprawl-bookkeeper agent updates available — run `sprawl update`.")
	case skillStale:
		printSkillBanner(stderr, "sprawl skill update available — run `sprawl update`.")
	case agentStale:
		printSkillBanner(stderr, "sprawl-bookkeeper agent update available — run `sprawl update`.")
	}
}

// hasStaleInstall reports whether any install of the given (kind, tool)
// has a recorded version that differs from remote. Empty remote means
// "couldn't probe" → false (silent rather than nag without certainty).
// Empty tool matches any tool (used for skills, which don't differ by tool).
func hasStaleInstall(installs []config.SkillInstall, kind, tool, remote string) bool {
	if remote == "" {
		return false
	}
	for _, inst := range installs {
		if inst.Kind != kind {
			continue
		}
		if tool != "" && inst.Tool != tool {
			continue
		}
		if inst.Version != remote {
			return true
		}
	}
	return false
}

func printSkillBanner(stderr io.Writer, msg string) {
	line := msg + "\n"
	if useColor(stderr) {
		fmt.Fprintf(stderr, "\x1b[33m%s\x1b[0m", line)
		return
	}
	fmt.Fprint(stderr, line)
}

func shouldCheck() bool {
	if build.AppName != "sprawl" {
		return false
	}
	if !IsReleaseVersion(build.Version) {
		return false
	}
	if os.Getenv("SPRAWL_NO_UPDATE_CHECK") == "1" {
		return false
	}
	return true
}

// fetchLatestQuiet returns the latest release tag, or "" on any error.
// Errors are intentionally swallowed — see MaybeNotify's contract.
func fetchLatestQuiet(ctx context.Context) string {
	tag, err := fetchLatestTag(ctx, notifyTimeout)
	if err != nil {
		return ""
	}
	return tag
}

func fetchLatestTag(ctx context.Context, timeout time.Duration) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", baseURL, repoOwner, repoName)
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sprawl-cli/"+build.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases: status %d", resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if !semver.IsValid(payload.TagName) {
		return "", fmt.Errorf("github releases: tag %q is not semver", payload.TagName)
	}
	return payload.TagName, nil
}

// printBannerIfNewer compares latest against build.Version and prints the
// notice when latest > current.
func printBannerIfNewer(stderr io.Writer, latest string) {
	if latest == "" || !semver.IsValid(latest) {
		return
	}
	if semver.Compare(latest, canonicalVersion(build.Version)) <= 0 {
		return
	}
	cur := strings.TrimPrefix(build.Version, "v")
	next := strings.TrimPrefix(latest, "v")
	msg := fmt.Sprintf("sprawl %s available (current: %s). Run `sprawl update`.\n", next, cur)
	if useColor(stderr) {
		fmt.Fprintf(stderr, "\x1b[33m%s\x1b[0m", msg)
		return
	}
	fmt.Fprint(stderr, msg)
}

// useColor reports whether stderr is a tty AND NO_COLOR is unset. We avoid
// pulling golang.org/x/term to keep the no-extra-deps pattern; instead we
// type-assert to *os.File and inspect the file mode.
func useColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func cacheFilePath() (string, error) {
	dir, err := config.Dir(build.AppName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cacheFileName), nil
}

func readCache(path string) (updateCache, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return updateCache{}, false
	}
	var c updateCache
	if err := json.Unmarshal(data, &c); err != nil {
		return updateCache{}, false
	}
	return c, true
}

// writeCache mirrors config.Save's atomic temp+rename pattern (see
// internal/config/config.go:66-98). We don't reuse that helper because it's
// typed for *config.Config and TOML; keeping the cache JSON keeps it visibly
// distinct from user-facing config.
func writeCache(path string, c updateCache) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update_check-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once Rename succeeds

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	enc := json.NewEncoder(tmp)
	if err := enc.Encode(&c); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// removeCache best-effort deletes the notify cache. Called after a
// successful update so the next invocation doesn't print a stale "newer
// version available" banner.
func removeCache() {
	path, err := cacheFilePath()
	if err != nil {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		// best-effort; nothing to do
		return
	}
}
