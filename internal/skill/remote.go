package skill

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ultrakorne/sprawl_cli/internal/build"
)

// rawBaseURL points at the host that serves raw repo files. Tests override
// it to an httptest.Server.
var rawBaseURL = "https://raw.githubusercontent.com"

const probeTimeout = 5 * time.Second

// RemoteVersions captures the latest versions of every installable artefact
// on the master branch. Empty strings mean "couldn't determine" — callers
// should treat that as "no comparison possible" and skip rather than warn.
type RemoteVersions struct {
	Skill         string
	ClaudeAgent   string
	OpenCodeAgent string
	CodexAgent    string
}

// FetchRemoteVersions fetches the version markers in parallel and parses the
// `version:` field out of each. Returns whatever it could probe; errors on
// individual files are folded into empty strings so a slow or missing source
// doesn't fail the whole call.
func FetchRemoteVersions(ctx context.Context) RemoteVersions {
	type result struct {
		key string
		v   string
	}
	files := map[string]string{
		"skill":         ".claude/skills/sprawl/SKILL.md",
		"claudeAgent":   ".claude/agents/sprawl-bookkeeper.md",
		"opencodeAgent": ".opencode/agents/sprawl-bookkeeper.md",
		"codexAgent":    codexAgentAssetPath,
	}
	ch := make(chan result, len(files))
	for key, path := range files {
		go func(key, path string) {
			ch <- result{key: key, v: fetchRawVersion(ctx, path)}
		}(key, path)
	}
	out := RemoteVersions{}
	for i := 0; i < len(files); i++ {
		r := <-ch
		switch r.key {
		case "skill":
			out.Skill = r.v
		case "claudeAgent":
			out.ClaudeAgent = r.v
		case "opencodeAgent":
			out.OpenCodeAgent = r.v
		case "codexAgent":
			out.CodexAgent = r.v
		}
	}
	return out
}

// fetchRawVersion downloads a single file from raw.githubusercontent.com
// and returns its frontmatter version. Empty string on any failure.
func fetchRawVersion(ctx context.Context, repoPath string) string {
	url := fmt.Sprintf("%s/%s/%s/master/%s", rawBaseURL, repoOwner, repoName, repoPath)
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "sprawl-cli/"+build.Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return ParseFrontmatterVersion(body)
}

// VersionFor returns the relevant remote version for an install record.
// Skill installs map to the SKILL.md version; agent installs map to the
// per-tool agent file. Empty if the remote probe yielded nothing usable.
func (rv RemoteVersions) VersionFor(kind, tool string) string {
	switch kind {
	case "skill":
		return rv.Skill
	case "agent":
		switch tool {
		case "opencode":
			return rv.OpenCodeAgent
		case "codex":
			return rv.CodexAgent
		}
		return rv.ClaudeAgent
	}
	return ""
}
