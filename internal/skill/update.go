package skill

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ultrakorne/sprawl_cli/internal/build"
	"github.com/ultrakorne/sprawl_cli/internal/config"
)

// Update walks every recorded SkillInstall, compares its stored version
// against the latest on master, and re-extracts the stale ones from a
// single tarball download. The config record's Version is rewritten to
// match the freshly-installed copy.
//
// Returns nil if there's nothing to do or every stale install was
// refreshed. Returns a joined error if some installs failed; the rest
// still get written and recorded.
func Update(ctx context.Context, out io.Writer) error {
	cfg, err := config.Load(build.AppName)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(cfg.SkillInstalls) == 0 {
		fmt.Fprintln(out, "No skill installs recorded; run `sprawl skill install` first.")
		return nil
	}

	rv := FetchRemoteVersions(ctx)

	type pending struct {
		idx    int
		remote string
	}
	var stale []pending
	for i, inst := range cfg.SkillInstalls {
		remote := rv.VersionFor(inst.Kind, inst.Tool)
		if remote == "" {
			fmt.Fprintf(out, "  ? %s — couldn't probe remote version, skipping\n", inst.Path)
			continue
		}
		if remote == inst.Version {
			fmt.Fprintf(out, "  = %s (v%s up to date)\n", inst.Path, inst.Version)
			continue
		}
		stale = append(stale, pending{idx: i, remote: remote})
	}
	if len(stale) == 0 {
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

	var errs []error
	for _, p := range stale {
		inst := cfg.SkillInstalls[p.idx]
		t := Target{
			Kind:    inst.Kind,
			Name:    inst.Name,
			Tool:    inst.Tool,
			Scope:   inst.Scope,
			SrcPath: srcFor(inst.Kind, inst.Tool),
			DstPath: inst.Path,
		}
		newVersion, werr := writeTarget(files, t)
		if werr != nil {
			fmt.Fprintf(out, "  ✗ %s: %v\n", inst.Path, werr)
			errs = append(errs, fmt.Errorf("%s: %w", inst.Path, werr))
			continue
		}
		// Use the freshly-parsed version when available; fall back to the
		// remote probe result so the record still moves forward if the
		// frontmatter parse fails for some reason.
		if newVersion == "" {
			newVersion = p.remote
		}
		cfg.SkillInstalls[p.idx].Version = newVersion
		fmt.Fprintf(out, "  ✓ %s (v%s → v%s)\n", inst.Path, inst.Version, newVersion)
	}

	if err := config.Save(build.AppName, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
