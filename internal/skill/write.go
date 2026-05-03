package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writeTarget materialises one Target on disk using files from the extracted
// tarball. Returns the version string parsed from the freshly-written
// frontmatter, or "" if the marker file is missing.
//
// For skills (kind="skill"), the destination directory is wiped and
// repopulated so removed source files don't linger from a prior install.
// For agents (kind="agent"), a single file is written.
func writeTarget(files map[string][]byte, t Target) (string, error) {
	switch t.Kind {
	case "skill":
		return writeSkillDir(files, t.SrcPath, t.DstPath)
	case "agent":
		return writeAgentFile(files, t.SrcPath, t.DstPath)
	}
	return "", fmt.Errorf("unknown target kind %q", t.Kind)
}

func writeSkillDir(files map[string][]byte, srcPrefix, dstDir string) (string, error) {
	prefix := strings.TrimSuffix(srcPrefix, "/") + "/"
	matched := make(map[string][]byte)
	for path, data := range files {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rel := path[len(prefix):]
		if rel == "" {
			continue
		}
		matched[rel] = data
	}
	if len(matched) == 0 {
		return "", fmt.Errorf("source %s not found in tarball", srcPrefix)
	}

	if err := os.RemoveAll(dstDir); err != nil {
		return "", fmt.Errorf("clean %s: %w", dstDir, err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dstDir, err)
	}
	for rel, data := range matched {
		dst := filepath.Join(dstDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", dst, err)
		}
	}
	// Version lives in SKILL.md frontmatter (the file the host loader reads).
	return ParseFrontmatterVersion(matched["SKILL.md"]), nil
}

func writeAgentFile(files map[string][]byte, srcPath, dstPath string) (string, error) {
	data, ok := files[srcPath]
	if !ok {
		return "", fmt.Errorf("source %s not found in tarball", srcPath)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(dstPath), err)
	}
	if err := os.WriteFile(dstPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", dstPath, err)
	}
	return ParseFrontmatterVersion(data), nil
}
