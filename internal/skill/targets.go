// Package skill installs and updates the sprawl skill + sprawl-bookkeeper
// agent into the destinations expected by Claude Code and OpenCode. Source
// is the repo's master branch on GitHub; install paths and recorded
// versions live in the per-binary config.toml so `sprawl update` can
// re-extract every recorded copy when a new version ships.
package skill

import (
	"fmt"
	"path/filepath"
)

// Choice is the user's selection coming out of the interactive prompts.
type Choice struct {
	What  []string // subset of {"skill", "agent"}
	Tools []string // subset of {"claude", "opencode"}
	Scope string   // "global" | "local"
}

// Target describes one concrete file or directory to install. Path is the
// install identity used in config bookkeeping — the same value goes into
// SkillInstall.Path so re-install and update can find the record.
type Target struct {
	Kind    string // "skill" | "agent"
	Name    string // "sprawl" | "sprawl-bookkeeper"
	Tool    string // "claude" | "opencode"
	Scope   string // "global" | "local"
	SrcPath string // repo-relative path inside the master tarball
	DstPath string // absolute destination on disk
}

// ResolveTargets expands a Choice into one Target per (what × tool) pair.
// home is the user's home directory; cwd is the working directory used for
// local-scope installs. Both must be absolute.
func ResolveTargets(c Choice, home, cwd string) []Target {
	var ts []Target
	for _, what := range c.What {
		for _, tool := range c.Tools {
			ts = append(ts, Target{
				Kind:    what,
				Name:    nameFor(what),
				Tool:    tool,
				Scope:   c.Scope,
				SrcPath: srcFor(what, tool),
				DstPath: dstFor(what, tool, c.Scope, home, cwd),
			})
		}
	}
	return ts
}

func nameFor(what string) string {
	if what == "skill" {
		return "sprawl"
	}
	return "sprawl-bookkeeper"
}

// srcFor returns the repo-relative path of the source. Skill content is
// shared between Claude and OpenCode; the agent file diverges per tool
// because the frontmatter shape differs.
func srcFor(what, tool string) string {
	switch what {
	case "skill":
		return ".claude/skills/sprawl"
	case "agent":
		if tool == "opencode" {
			return ".opencode/agents/sprawl-bookkeeper.md"
		}
		return ".claude/agents/sprawl-bookkeeper.md"
	}
	return ""
}

// dstFor returns the absolute destination for a (what, tool, scope) triple.
// Returns "" for unknown combinations — caller is expected to feed valid
// values from the prompts.
func dstFor(what, tool, scope, home, cwd string) string {
	base := home
	if scope == "local" {
		base = cwd
	}
	key := fmt.Sprintf("%s/%s/%s", what, tool, scope)
	switch key {
	case "skill/claude/global", "skill/claude/local":
		return filepath.Join(base, ".claude", "skills", "sprawl")
	case "skill/opencode/global":
		return filepath.Join(base, ".config", "opencode", "skills", "sprawl")
	case "skill/opencode/local":
		return filepath.Join(base, ".opencode", "skills", "sprawl")
	case "agent/claude/global", "agent/claude/local":
		return filepath.Join(base, ".claude", "agents", "sprawl-bookkeeper.md")
	case "agent/opencode/global":
		return filepath.Join(base, ".config", "opencode", "agents", "sprawl-bookkeeper.md")
	case "agent/opencode/local":
		return filepath.Join(base, ".opencode", "agents", "sprawl-bookkeeper.md")
	}
	return ""
}
