package skill

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// Uninstall removes every recorded skill / agent install from disk and
// drops the corresponding rows from config.toml. There's no per-target
// selection by design — install bookkeeping already names every place a
// copy landed, so the simple thing is to clear them all.
//
// Per-target failures are reported but do not abort the rest of the run.
// A failed removal keeps its config row so the user can retry; successful
// removals are dropped from the config regardless.
func Uninstall(ctx context.Context, in io.Reader, out io.Writer) error {
	cfg, err := config.Load(build.AppName)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(cfg.SkillInstalls) == 0 {
		fmt.Fprintln(out, "Nothing installed.")
		return nil
	}

	fmt.Fprintln(out, "About to uninstall:")
	for _, inst := range cfg.SkillInstalls {
		fmt.Fprintf(out, "  - %s (%s, %s) ← %s\n", inst.Name, inst.Tool, inst.Scope, inst.Path)
	}
	fmt.Fprintln(out)

	proceed, err := promptConfirmFunc(in, out, "Uninstall all?")
	if err != nil {
		if errors.Is(err, errPromptCancelled) {
			fmt.Fprintln(out, "Aborted.")
			return nil
		}
		return fmt.Errorf("confirm: %w", err)
	}
	if !proceed {
		fmt.Fprintln(out, "Aborted.")
		return nil
	}

	total := len(cfg.SkillInstalls)
	var failed int
	// Iterate over a snapshot so RemoveInstall can safely mutate
	// cfg.SkillInstalls during the loop.
	for _, inst := range append([]config.SkillInstall(nil), cfg.SkillInstalls...) {
		if err := os.RemoveAll(inst.Path); err != nil {
			fmt.Fprintf(out, "  ✗ %s: %v\n", inst.Path, err)
			failed++
			continue
		}
		cfg.RemoveInstall(inst.Path)
		fmt.Fprintf(out, "  ✓ %s\n", inst.Path)
	}

	if err := config.Save(build.AppName, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d target(s) failed", failed, total)
	}
	fmt.Fprintln(out, "Done.")
	return nil
}
