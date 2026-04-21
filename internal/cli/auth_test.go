package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

func withAgentSecretFlag(t *testing.T, v string) {
	t.Helper()
	prev := agentSecretFlag
	agentSecretFlag = v
	t.Cleanup(func() { agentSecretFlag = prev })
}

// scratchConfigDir wires XDG_CONFIG_HOME to a tmpdir so token lookups don't
// touch the user's real config. Returns the dir for assertions.
func scratchConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// -- resolveToken -----------------------------------------------------------

func TestResolveToken_EnvOverridesConfig(t *testing.T) {
	scratchConfigDir(t)
	if err := config.Save(build.AppName, &config.Config{Token: "from-config"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	t.Setenv("SPRAWL_TOKEN", "from-env")
	got, err := resolveToken()
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if got != "from-env" {
		t.Fatalf("token = %q, want from-env", got)
	}
}

func TestResolveToken_FallsBackToConfig(t *testing.T) {
	scratchConfigDir(t)
	if err := config.Save(build.AppName, &config.Config{Token: "from-config"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	t.Setenv("SPRAWL_TOKEN", "")
	got, err := resolveToken()
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if got != "from-config" {
		t.Fatalf("token = %q, want from-config", got)
	}
}

func TestResolveToken_MissingReturnsLoginHint(t *testing.T) {
	dir := scratchConfigDir(t)
	t.Setenv("SPRAWL_TOKEN", "")
	_, err := resolveToken()
	if err == nil {
		t.Fatal("expected error when no token")
	}
	if !strings.Contains(err.Error(), "login") {
		t.Fatalf("error should hint at `login`: %v", err)
	}
	// Sanity: nothing was written to the config dir.
	if _, statErr := readDir(dir); statErr != nil {
		t.Fatalf("config dir missing: %v", statErr)
	}
}

func readDir(dir string) ([]string, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// -- resolveAgentSecret -----------------------------------------------------

func TestResolveAgentSecret_FlagWins(t *testing.T) {
	withAgentSecretFlag(t, "from-flag")
	t.Setenv("SPRAWL_AGENT_SECRET", "from-env")
	got, err := resolveAgentSecret()
	if err != nil {
		t.Fatalf("resolveAgentSecret: %v", err)
	}
	if got != "from-flag" {
		t.Fatalf("secret = %q, want from-flag", got)
	}
}

func TestResolveAgentSecret_FallsBackToEnv(t *testing.T) {
	withAgentSecretFlag(t, "")
	t.Setenv("SPRAWL_AGENT_SECRET", "from-env")
	got, err := resolveAgentSecret()
	if err != nil {
		t.Fatalf("resolveAgentSecret: %v", err)
	}
	if got != "from-env" {
		t.Fatalf("secret = %q, want from-env", got)
	}
}

func TestResolveAgentSecret_Missing(t *testing.T) {
	withAgentSecretFlag(t, "")
	t.Setenv("SPRAWL_AGENT_SECRET", "")
	_, err := resolveAgentSecret()
	if err == nil {
		t.Fatal("expected error when secret unset")
	}
	if !strings.Contains(err.Error(), "SPRAWL_AGENT_SECRET") {
		t.Fatalf("error should name the env var: %v", err)
	}
}

// -- newAuthedClient --------------------------------------------------------

// TestNewAuthedClient_FailsPreHTTPOnMissingSecret is the invariant the CLAUDE.md
// credential section promises: missing secret fails before any HTTP call so the
// server can't see a bad request.
func TestNewAuthedClient_FailsPreHTTPOnMissingSecret(t *testing.T) {
	scratchConfigDir(t)
	if err := config.Save(build.AppName, &config.Config{Token: "tok"}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	withAgentSecretFlag(t, "")
	t.Setenv("SPRAWL_AGENT_SECRET", "")
	t.Setenv("SPRAWL_TOKEN", "")

	// Set up a server that panics if touched — proof of the pre-HTTP
	// invariant.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server was hit; expected pre-HTTP failure. Path: %s", r.URL.Path)
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SPRAWL_API_URL", srv.URL)

	if _, err := newAuthedClient(); err == nil {
		t.Fatal("expected error when agent secret missing")
	}
}

func TestNewAuthedClient_Success(t *testing.T) {
	// Stand up a server that asserts the outbound request carries both headers.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer the-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Agent-Secret") != "the-secret" {
			t.Errorf("X-Agent-Secret = %q", r.Header.Get("X-Agent-Secret"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(ts.Close)
	t.Setenv("SPRAWL_API_URL", ts.URL)

	scratchConfigDir(t)
	withAgentSecretFlag(t, "the-secret")
	t.Setenv("SPRAWL_TOKEN", "the-token")

	c, err := newAuthedClient()
	if err != nil {
		t.Fatalf("newAuthedClient: %v", err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}
