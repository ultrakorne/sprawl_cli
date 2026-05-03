package skill

import "strings"

// ParseFrontmatterVersion returns the value of the `version:` key inside the
// leading `---` ... `---` YAML frontmatter block, or "" if either the block
// or the key is missing. Quotes around the value are stripped.
//
// We don't pull in a full YAML parser — frontmatter here is hand-edited by
// the maintainer and the version field is always a top-level scalar, so a
// line scan is enough and keeps the no-extra-deps stance.
func ParseFrontmatterVersion(content []byte) string {
	s := string(content)
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
