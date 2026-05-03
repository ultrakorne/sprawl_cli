package skill

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// Install runs the interactive install flow: prompt for selection, fetch
// the master tarball, write each target, and record paths + versions in
// config so `sprawl update` can refresh them later.
//
// cwd is used for local-scope installs (shown in the prompt and used as
// the destination root). out is the user-facing stream; in is read for
// prompt input. Errors from individual writes don't abort the rest of the
// plan — every failure is reported per-target so the user can see exactly
// what landed.
func Install(ctx context.Context, cwd string, in io.Reader, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	r := bufio.NewReader(in)

	choice, err := promptChoice(r, out, cwd)
	if err != nil {
		return err
	}
	if len(choice.What) == 0 || len(choice.Tools) == 0 {
		fmt.Fprintln(out, "Nothing selected.")
		return nil
	}

	targets := ResolveTargets(choice, home, cwd)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "About to install:")
	for _, t := range targets {
		fmt.Fprintf(out, "  - %s (%s, %s) → %s\n", t.Name, t.Tool, t.Scope, t.DstPath)
	}
	fmt.Fprintln(out)
	if !promptYesNo(r, out, "Proceed? [Y/n]: ", true) {
		fmt.Fprintln(out, "Aborted.")
		return nil
	}

	fmt.Fprintln(out, "Fetching latest from GitHub master…")
	gz, err := fetchMasterTarball(ctx)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	files, err := extractTarball(gz)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	cfg, err := config.Load(build.AppName)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	var failed int
	for _, t := range targets {
		version, err := writeTarget(files, t)
		if err != nil {
			fmt.Fprintf(out, "  ✗ %s: %v\n", t.DstPath, err)
			failed++
			continue
		}
		cfg.UpsertInstall(config.SkillInstall{
			Kind:    t.Kind,
			Name:    t.Name,
			Tool:    t.Tool,
			Scope:   t.Scope,
			Path:    t.DstPath,
			Version: version,
		})
		fmt.Fprintf(out, "  ✓ %s (v%s)\n", t.DstPath, version)
	}

	if err := config.Save(build.AppName, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d target(s) failed", failed, len(targets))
	}
	fmt.Fprintln(out, "Done.")
	return nil
}
