package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
)

// loadJSONFromSource reads a JSON object from stdin ("-") or a file path and
// returns it as an attrs map. Used by write commands to accept `--from-json`.
// The payload must be a top-level object; arrays and scalars are rejected.
func loadJSONFromSource(source string, stdin io.Reader) (map[string]any, error) {
	var reader io.Reader
	if source == "-" {
		reader = stdin
	} else {
		f, err := os.Open(source)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", source, err)
		}
		defer f.Close()
		reader = f
	}
	var raw any
	dec := json.NewDecoder(reader)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode json from %s: %w", source, err)
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("decode json from %s: expected a JSON object at top level", source)
	}
	return obj, nil
}

// mergeStringFlag sets attrs[key]=value if the flag was non-empty. Flags
// override values already present in the attrs map (e.g. from --from-json).
func mergeStringFlag(attrs map[string]any, key, value string) {
	if value == "" {
		return
	}
	attrs[key] = value
}

// mergeProjectID parses the --project-id flag as an integer and merges it.
// An empty flag is a no-op. Non-numeric input errors out so the user gets a
// clean message rather than a 422 invalid_project_id from the server.
func mergeProjectID(attrs map[string]any, value string) error {
	if value == "" {
		return nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("--project-id must be an integer, got %q", value)
	}
	attrs["project_id"] = n
	return nil
}

// requireAttrs fails if the final attrs map is empty after merging flags and
// --from-json. Callers use it to avoid sending empty PATCH bodies that the
// server would reject or silently accept.
func requireAttrs(attrs map[string]any, subject string) error {
	if len(attrs) == 0 {
		return fmt.Errorf("%s requires at least one field (use flags or --from-json)", subject)
	}
	return nil
}
