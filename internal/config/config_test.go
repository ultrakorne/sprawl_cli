package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// useScratchXDG points XDG_CONFIG_HOME at a temp dir for the duration of the test.
// AppName is still the thing that drives the subdir, so pass distinct names per
// test when that matters (it rarely does — the temp dir is per-test anyway).
func useScratchXDG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestDir_HonoursXDG(t *testing.T) {
	root := useScratchXDG(t)
	got, err := Dir("sprawl_dev")
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	want := filepath.Join(root, "sprawl_dev")
	if got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}
}

func TestDir_FallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	// Preferred by os.UserHomeDir on unix. Not all platforms, but the repo's
	// target is linux.
	t.Setenv("HOME", home)
	got, err := Dir("sprawl")
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	want := filepath.Join(home, ".config", "sprawl")
	if got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}
}

func TestPath_IsDirPlusConfigToml(t *testing.T) {
	useScratchXDG(t)
	p, err := Path("sprawl")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if !strings.HasSuffix(p, filepath.Join("sprawl", "config.toml")) {
		t.Fatalf("Path = %q, expected to end with sprawl/config.toml", p)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	useScratchXDG(t)
	cfg, err := Load("sprawl")
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if cfg.Token != "" {
		t.Fatalf("expected empty token on missing file, got %q", cfg.Token)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	useScratchXDG(t)
	want := &Config{Token: "deadbeef-token"}
	if err := Save("sprawl_dev", want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load("sprawl_dev")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Token != want.Token {
		t.Fatalf("Token = %q, want %q", got.Token, want.Token)
	}
}

func TestSave_FileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions only")
	}
	useScratchXDG(t)
	if err := Save("sprawl", &Config{Token: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	p, _ := Path("sprawl")
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 0600", got)
	}
}

func TestSave_DirMode0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions only")
	}
	useScratchXDG(t)
	if err := Save("sprawl", &Config{Token: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	dir, _ := Dir("sprawl")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", got)
	}
}

func TestSave_NoTempFileLeftBehind(t *testing.T) {
	// Atomic-write contract: after a successful Save, only config.toml should
	// exist in the config dir (no .config.toml-XXXX tmp files).
	useScratchXDG(t)
	if err := Save("sprawl", &Config{Token: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	dir, _ := Dir("sprawl")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("config dir should contain only config.toml, got %v", names)
	}
}

func TestSave_OverwritePreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions only")
	}
	useScratchXDG(t)
	if err := Save("sprawl", &Config{Token: "first"}); err != nil {
		t.Fatalf("Save #1: %v", err)
	}
	if err := Save("sprawl", &Config{Token: "second"}); err != nil {
		t.Fatalf("Save #2: %v", err)
	}
	p, _ := Path("sprawl")
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode after overwrite = %o, want 0600", got)
	}
	got, err := Load("sprawl")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Token != "second" {
		t.Fatalf("Token = %q, want %q", got.Token, "second")
	}
}
