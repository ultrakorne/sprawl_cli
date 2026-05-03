// Package config reads and writes the per-binary config.toml.
//
// The config directory is derived from the binary's AppName (set at build
// time via -ldflags), so `sprawl` and `sprawl_dev` never collide.
// Schema:
//   - token: device-flow result, written by `sprawl login`.
//   - skill_installs: bookkeeping for `sprawl skill install` so that
//     `sprawl update` can re-extract every recorded copy.
//
// The agent secret is never persisted — it comes from SPRAWL_AGENT_SECRET
// at invocation time.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// SkillInstall records one on-disk copy of the sprawl skill or the
// sprawl-bookkeeper agent. `Path` is the install identity — for a skill it
// points at the directory, for an agent at the .md file.
type SkillInstall struct {
	Kind    string `toml:"kind"`    // "skill" | "agent"
	Name    string `toml:"name"`    // e.g. "sprawl" or "sprawl-bookkeeper"
	Tool    string `toml:"tool"`    // "claude" | "opencode"
	Scope   string `toml:"scope"`   // "global" | "local"
	Path    string `toml:"path"`    // absolute target path
	Version string `toml:"version"` // version recorded at install time
}

type Config struct {
	Token         string         `toml:"token,omitempty"`
	SkillInstalls []SkillInstall `toml:"skill_installs,omitempty"`
}

// UpsertInstall replaces the existing record for inst.Path or appends a new
// one. Path is the identity — a skill dir and an agent file can never share
// it. Returns true if an existing record was replaced.
func (c *Config) UpsertInstall(inst SkillInstall) bool {
	for i, existing := range c.SkillInstalls {
		if existing.Path == inst.Path {
			c.SkillInstalls[i] = inst
			return true
		}
	}
	c.SkillInstalls = append(c.SkillInstalls, inst)
	return false
}

// RemoveInstall drops the record for path. Returns true if a record was
// removed.
func (c *Config) RemoveInstall(path string) bool {
	for i, existing := range c.SkillInstalls {
		if existing.Path == path {
			c.SkillInstalls = append(c.SkillInstalls[:i], c.SkillInstalls[i+1:]...)
			return true
		}
	}
	return false
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
