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

func TestSaveLoad_SkillInstallsRoundTrip(t *testing.T) {
	useScratchXDG(t)
	want := &Config{
		Token: "tok",
		SkillInstalls: []SkillInstall{
			{Kind: "skill", Name: "sprawl", Tool: "claude", Scope: "global", Path: "/home/x/.claude/skills/sprawl", Version: "0.1.0"},
			{Kind: "agent", Name: "sprawl-bookkeeper", Tool: "opencode", Scope: "local", Path: "/work/proj/.opencode/agents/sprawl-bookkeeper.md", Version: "0.1.0"},
		},
	}
	if err := Save("sprawl", want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load("sprawl")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Token != want.Token {
		t.Fatalf("Token = %q, want %q", got.Token, want.Token)
	}
	if len(got.SkillInstalls) != len(want.SkillInstalls) {
		t.Fatalf("SkillInstalls len = %d, want %d", len(got.SkillInstalls), len(want.SkillInstalls))
	}
	for i := range want.SkillInstalls {
		if got.SkillInstalls[i] != want.SkillInstalls[i] {
			t.Fatalf("SkillInstalls[%d] = %+v, want %+v", i, got.SkillInstalls[i], want.SkillInstalls[i])
		}
	}
}

func TestLoad_OldConfigWithoutSkillInstalls(t *testing.T) {
	// Older configs predate the [[skill_installs]] section. Loading must
	// succeed and yield an empty slice, not an error or a non-nil sentinel.
	dir := useScratchXDG(t)
	if err := os.MkdirAll(filepath.Join(dir, "sprawl"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(dir, "sprawl", "config.toml")
	if err := os.WriteFile(p, []byte("token = \"old\"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load("sprawl")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Token != "old" {
		t.Fatalf("Token = %q, want %q", got.Token, "old")
	}
	if len(got.SkillInstalls) != 0 {
		t.Fatalf("SkillInstalls = %+v, want empty", got.SkillInstalls)
	}
}

func TestUpsertInstall_AppendsNewRecord(t *testing.T) {
	c := &Config{}
	replaced := c.UpsertInstall(SkillInstall{Kind: "skill", Name: "sprawl", Tool: "claude", Scope: "global", Path: "/a", Version: "0.1.0"})
	if replaced {
		t.Fatalf("UpsertInstall on empty config should append, got replaced=true")
	}
	if len(c.SkillInstalls) != 1 {
		t.Fatalf("len = %d, want 1", len(c.SkillInstalls))
	}
}

func TestUpsertInstall_ReplacesByPath(t *testing.T) {
	c := &Config{SkillInstalls: []SkillInstall{
		{Kind: "skill", Name: "sprawl", Tool: "claude", Scope: "global", Path: "/a", Version: "0.1.0"},
		{Kind: "skill", Name: "sprawl", Tool: "opencode", Scope: "global", Path: "/b", Version: "0.1.0"},
	}}
	replaced := c.UpsertInstall(SkillInstall{Kind: "skill", Name: "sprawl", Tool: "claude", Scope: "global", Path: "/a", Version: "0.2.0"})
	if !replaced {
		t.Fatalf("UpsertInstall on existing path should replace, got replaced=false")
	}
	if len(c.SkillInstalls) != 2 {
		t.Fatalf("len = %d, want 2 (replace, not append)", len(c.SkillInstalls))
	}
	if c.SkillInstalls[0].Version != "0.2.0" {
		t.Fatalf("version = %q, want 0.2.0", c.SkillInstalls[0].Version)
	}
	if c.SkillInstalls[1].Path != "/b" {
		t.Fatalf("second record disturbed: %+v", c.SkillInstalls[1])
	}
}

func TestRemoveInstall(t *testing.T) {
	c := &Config{SkillInstalls: []SkillInstall{
		{Path: "/a"}, {Path: "/b"}, {Path: "/c"},
	}}
	if !c.RemoveInstall("/b") {
		t.Fatalf("RemoveInstall(/b) = false, want true")
	}
	if len(c.SkillInstalls) != 2 || c.SkillInstalls[0].Path != "/a" || c.SkillInstalls[1].Path != "/c" {
		t.Fatalf("after remove: %+v", c.SkillInstalls)
	}
	if c.RemoveInstall("/nope") {
		t.Fatalf("RemoveInstall(/nope) = true on absent path")
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
