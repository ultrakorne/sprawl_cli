package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/client"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// resolveToken returns the bearer token from SPRAWL_TOKEN or config.toml,
// in that order. Empty → "not logged in" error.
func resolveToken() (string, error) {
	if t := os.Getenv("SPRAWL_TOKEN"); t != "" {
		return t, nil
	}
	cfg, err := config.Load(build.AppName)
	if err != nil {
		return "", err
	}
	if cfg.Token == "" {
		return "", fmt.Errorf("not logged in, run `%s login`", build.AppName)
	}
	return cfg.Token, nil
}

// resolveAgentSecret returns the agent secret from --agent-secret or
// $SPRAWL_AGENT_SECRET, in that order. Empty → error.
func resolveAgentSecret() (string, error) {
	if agentSecretFlag != "" {
		return agentSecretFlag, nil
	}
	if v := os.Getenv("SPRAWL_AGENT_SECRET"); v != "" {
		return v, nil
	}
	return "", errors.New("agent secret not set — export SPRAWL_AGENT_SECRET or pass --agent-secret")
}

// newAuthedClient resolves both credentials (failing pre-HTTP if the agent
// secret is missing) and returns a client ready for /api/v1/* calls.
func newAuthedClient() (*client.Client, error) {
	token, err := resolveToken()
	if err != nil {
		return nil, err
	}
	secret, err := resolveAgentSecret()
	if err != nil {
		return nil, err
	}
	return client.NewAuthed(token, secret), nil
}
