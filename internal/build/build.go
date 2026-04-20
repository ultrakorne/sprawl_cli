// Package build holds values injected by the linker at build time.
//
// Two binaries ship from this repo: `sprawl` (prod) and `sprawl_dev` (dev).
// They share this codebase and differ only in the values set here via
// `-ldflags "-X"`. See Makefile for the exact flags.
package build

var (
	// APIURL is the base URL for the task_manager API. Overridden at build
	// time per the plan; never read from config.
	APIURL = "http://localhost:4000"

	// AppName selects the XDG config directory:
	//   prod → ~/.config/sprawl/config.toml
	//   dev  → ~/.config/sprawl_dev/config.toml
	AppName = "sprawl_dev"

	// Version, Commit, Date are set by goreleaser; zero values mean a local build.
	Version = "dev"
	Commit  = ""
	Date    = ""
)
