package skill

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// option pairs a human-readable label with the value emitted on selection.
type option struct {
	label string
	value string
}

// promptMultiSelect asks the user to pick one or more entries by index.
// Empty input means "all" — by design, since both selectors here default to
// every entry. Returns the selected values in the original list order.
//
// Loops on bad input until either a valid line or io.EOF (returned as an
// error so the caller can abort cleanly).
func promptMultiSelect(in *bufio.Reader, out io.Writer, header string, opts []option) ([]string, error) {
	for {
		fmt.Fprintln(out, header)
		for i, o := range opts {
			fmt.Fprintf(out, "  %d) %s\n", i+1, o.label)
		}
		fmt.Fprint(out, "Selection (comma-separated, blank = all): ")

		line, err := readLine(in)
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			out := make([]string, len(opts))
			for i, o := range opts {
				out[i] = o.value
			}
			return out, nil
		}

		picked, ok := parseIndexes(line, len(opts))
		if !ok {
			fmt.Fprintln(out, "Invalid selection — enter numbers like 1 or 1,2.")
			continue
		}
		seen := make(map[int]bool, len(picked))
		var values []string
		for _, idx := range picked {
			if seen[idx] {
				continue
			}
			seen[idx] = true
			values = append(values, opts[idx].value)
		}
		return values, nil
	}
}

// promptSingleSelect requires exactly one index. Loops on bad input.
func promptSingleSelect(in *bufio.Reader, out io.Writer, header string, opts []option) (string, error) {
	for {
		fmt.Fprintln(out, header)
		for i, o := range opts {
			fmt.Fprintf(out, "  %d) %s\n", i+1, o.label)
		}
		fmt.Fprintf(out, "Choose [1-%d]: ", len(opts))

		line, err := readLine(in)
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(opts) {
			fmt.Fprintln(out, "Invalid selection — enter a number.")
			continue
		}
		return opts[n-1].value, nil
	}
}

// promptYesNo prints prompt and returns true on "y"/"yes" (case-insensitive).
// Empty input returns defaultYes. Anything else → false. EOF behaves as
// empty input so a closed stdin uses the default rather than crashing.
func promptYesNo(in *bufio.Reader, out io.Writer, prompt string, defaultYes bool) bool {
	fmt.Fprint(out, prompt)
	line, err := readLine(in)
	if err != nil {
		return defaultYes
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes
	}
	return line == "y" || line == "yes"
}

// readLine reads up to '\n' and returns the line without the terminator.
// io.EOF mid-line is treated as a successful read of whatever was buffered;
// EOF on an empty buffer returns io.EOF so callers can abort.
func readLine(in *bufio.Reader) (string, error) {
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if line == "" && err != nil {
		return "", io.EOF
	}
	return strings.TrimRight(line, "\n"), nil
}

// parseIndexes parses "1,3,2" into []int{0,2,1}. Returns false on any out-
// of-range or non-numeric token.
func parseIndexes(s string, max int) ([]int, bool) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > max {
			return nil, false
		}
		out = append(out, n-1)
	}
	return out, true
}

// promptChoice runs the three-stage interactive picker (what / tools /
// scope) and returns the resolved Choice. cwd is shown in the local-scope
// label so the user sees exactly where a local install will land.
func promptChoice(in *bufio.Reader, out io.Writer, cwd string) (Choice, error) {
	what, err := promptMultiSelect(in, out, "What to install?", []option{
		{label: "sprawl skill", value: "skill"},
		{label: "sprawl-bookkeeper agent", value: "agent"},
	})
	if err != nil {
		return Choice{}, err
	}
	if len(what) == 0 {
		return Choice{}, nil
	}
	fmt.Fprintln(out)

	tools, err := promptMultiSelect(in, out, "For which AI tools?", []option{
		{label: "Claude Code", value: "claude"},
		{label: "OpenCode", value: "opencode"},
	})
	if err != nil {
		return Choice{}, err
	}
	if len(tools) == 0 {
		return Choice{}, nil
	}
	fmt.Fprintln(out)

	scope, err := promptSingleSelect(in, out, "Scope?", []option{
		{label: "Global (your home directory)", value: "global"},
		{label: "Local — current folder: " + cwd, value: "local"},
	})
	if err != nil {
		return Choice{}, err
	}
	return Choice{What: what, Tools: tools, Scope: scope}, nil
}
