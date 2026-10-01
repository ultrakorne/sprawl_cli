package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

func parseAssignee(value string) (*client.Assignee, error) {
	kind, rawID, ok := strings.Cut(value, ":")
	invalid := func() (*client.Assignee, error) {
		return nil, fmt.Errorf("invalid assignee %q: want user:<id> or agent_key:<id>, with a decimal id from 1 to 2147483647", value)
	}
	if !ok || (kind != "user" && kind != "agent_key") || rawID == "" {
		return invalid()
	}
	for _, ch := range rawID {
		if ch < '0' || ch > '9' {
			return invalid()
		}
	}
	id, err := strconv.ParseInt(rawID, 10, 32)
	if err != nil || id <= 0 {
		return invalid()
	}
	return &client.Assignee{Type: kind, ID: id}, nil
}

// mergeAssignment preserves omission and null, applies explicit flags first,
// then validates JSON input through the same parser and emits an integer ID.
func mergeAssignment(attrs map[string]any, value string, present, clear bool) error {
	if present && clear {
		return fmt.Errorf("--assignee and --unassign are mutually exclusive")
	}
	if clear {
		attrs["assignee"] = nil
		return nil
	}
	if present {
		a, err := parseAssignee(value)
		if err != nil {
			return err
		}
		attrs["assignee"] = a
		return nil
	}
	raw, exists := attrs["assignee"]
	if !exists || raw == nil {
		return nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("assignee must be a {type, id} object or null")
	}
	kind, ok := obj["type"].(string)
	if !ok {
		return fmt.Errorf("assignee.type must be user or agent_key")
	}
	var id string
	switch v := obj["id"].(type) {
	case string:
		id = v
	case float64:
		id = strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Errorf("assignee.id must be a decimal integer from 1 to 2147483647")
	}
	a, err := parseAssignee(kind + ":" + id)
	if err != nil {
		return err
	}
	attrs["assignee"] = a
	return nil
}

func newItemAssignCmd(opts *runtimeOpts, clear bool) *cobra.Command {
	use, short, count := "assign <id> <user:id|agent_key:id>", "Assign or reassign an item", 2
	if clear {
		use, short, count = "unassign <id>", "Clear an item's assignment", 1
	}
	cmd := &cobra.Command{
		Use: use, Short: short,
		Long: short + ". Discover eligible targets with `workspace actors` in the same workspace. " +
			"Use --workspace <id> for both discovery and writes in another workspace. " +
			"Assignment preserves completion, state, notes and PR number.",
		Args: textArgs(cobra.ExactArgs(count)),
		RunE: func(cmd *cobra.Command, args []string) error {
			value := ""
			if !clear {
				value = args[1]
			}
			attrs := map[string]any{}
			if err := mergeAssignment(attrs, value, !clear, clear); err != nil {
				return reportErr(cmd.OutOrStdout(), cmd.ErrOrStderr(), err, opts)
			}
			return runItemUpdate(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], attrs, opts)
		},
		SilenceErrors: true,
	}
	return cmd
}

func assigneeMap(a *client.Assignee) any {
	if a == nil {
		return nil
	}
	return map[string]any{"type": a.Type, "id": a.ID}
}

func assigneeText(a *client.Assignee) string {
	if a == nil {
		return "-"
	}
	return fmt.Sprintf("%s:%d", a.Type, a.ID)
}
