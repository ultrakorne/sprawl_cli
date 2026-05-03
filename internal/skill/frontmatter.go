package skill

import "strings"

// ParseFrontmatterVersion returns the value of the `version:` marker from
// installable artefacts. YAML-frontmatter files use a leading `---` ...
// `---` block; Codex TOML agent files use a leading `# version:` comment so
// the agent config itself stays valid Codex TOML. Quotes around the value are
// stripped.
//
// We don't pull in a full YAML parser — frontmatter here is hand-edited by
// the maintainer and the version field is always a top-level scalar, so a
// line scan is enough and keeps the no-extra-deps stance.
func ParseFrontmatterVersion(content []byte) string {
	s := string(content)
	if strings.HasPrefix(s, "# version:") {
		line, _, _ := strings.Cut(s, "\n")
		v := strings.TrimSpace(strings.TrimPrefix(line, "# version:"))
		return strings.Trim(v, `"'`)
	}
	if !strings.HasPrefix(s, "---\n") {
		return ""
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "version:") {
			continue
		}
		v := strings.TrimSpace(line[len("version:"):])
		v = strings.Trim(v, `"'`)
		return v
	}
	return ""
}
