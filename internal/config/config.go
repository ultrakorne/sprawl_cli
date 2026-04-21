// Package config reads and writes the per-binary config.toml.
//
// The config directory is derived from the binary's AppName (set at build
// time via -ldflags), so `sprawl` and `sprawl_dev` never collide.
// Schema is intentionally minimal: only `token` is stored. The agent
// secret is never persisted — it comes from SPRAWL_AGENT_SECRET at
// invocation time.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Token string `toml:"token,omitempty"`
}

// Dir returns the config directory for appName, honouring XDG_CONFIG_HOME
// and falling back to ~/.config.
func Dir(appName string) (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", appName), nil
}

// Path is Dir + "config.toml".
func Path(appName string) (string, error) {
	dir, err := Dir(appName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Load returns the config file contents. Missing file → zero Config + nil error.
func Load(appName string) (*Config, error) {
	p, err := Path(appName)
	if err != nil {
		return nil, err
	}
	var c Config
	_, err = toml.DecodeFile(p, &c)
	if errors.Is(err, fs.ErrNotExist) {
		return &c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	return &c, nil
}

// Save writes the config atomically with mode 0600, creating the directory
// at 0700 if missing. Atomic rename means an interrupted write never leaves
// the file truncated.
func Save(appName string, c *Config) error {
	dir, err := Dir(appName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	final := filepath.Join(dir, "config.toml")

	tmp, err := os.CreateTemp(dir, ".config.toml-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once Rename succeeds

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := toml.NewEncoder(tmp).Encode(c); err != nil {
		tmp.Close()
		return fmt.Errorf("encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		return fmt.Errorf("rename to %s: %w", final, err)
	}
	return nil
}
