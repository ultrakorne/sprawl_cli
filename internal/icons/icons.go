// Package icons is the glyph vocabulary for item states.
//
// It lives in its own package because two surfaces render an item's state and
// they must not drift: the CLI's shared item table (internal/cli/item_render.go)
// and the interactive TUI (internal/tui). Both resolve the same set through the
// same SPRAWL_ICONS opt-out, so a state looks identical whichever one you're in.
package icons

import (
	"os"
	"strings"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// Set is one complete glyph vocabulary. Two exist because the good one needs a
// font the user may not have.
type Set struct{ Ready, Progress, Review string }

// Nerd is the Nerd Font Material Design Icons set — the same icon family the
// web app's state pills use: a raised hand, a crossed hammer + wrench, an eye.
// They are monochrome glyphs in the Private Use Area, so unlike emoji they carry
// no colour of their own and inherit the terminal's foreground exactly like a
// letter does — which is what lets callers tint them from the ANSI palette.
var Nerd = Set{
	Ready:    "\U000F0E47", // md-hand_back_right
	Progress: "\U000F1323", // md-hammer_wrench
	Review:   "\U000F0208", // md-eye
}

// Plain is the fallback for terminals whose font has no Nerd Font glyphs, where
// the set above renders as tofu boxes. Opt in with SPRAWL_ICONS=plain.
// Geometric shapes, also uncoloured.
var Plain = Set{Ready: "▸", Progress: "◐", Review: "◉"}

// Unknown marks a state added server-side after this build: a marker rather than
// nothing, so a row doesn't lie about being stateless.
const Unknown = "•"

// Width is the display width every glyph — including Unknown — must occupy. The
// state column is a constant width in both the CLI table and the TUI, which only
// works if that holds; a two-cell glyph shears every row carrying it. Guarded by
// TestGlyphsAreOneCell here and mirrored in both consumers.
const Width = 1

// Resolve picks the active set. Nerd Font glyphs are the default — this is a
// developer's terminal tool and a patched font is the norm — with an env escape
// hatch rather than a config field, matching how every other one-off override in
// this CLI works.
func Resolve() Set {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SPRAWL_ICONS")), "plain") {
		return Plain
	}
	return Nerd
}

// For is the glyph for an item state, or "" when it has none.
func (s Set) For(state string) string {
	switch state {
	case "":
		return ""
	case client.StateReadyToPickup:
		return s.Ready
	case client.StateInProgress:
		return s.Progress
	case client.StateInReview:
		return s.Review
	default:
		return Unknown
	}
}

// For resolves the active set and returns the glyph for state. The env lookup is
// per call rather than cached in a package var so that a test (or a shell that
// exports SPRAWL_ICONS mid-session) is honoured — callers render a bounded number
// of rows, so the cost is noise.
func For(state string) string { return Resolve().For(state) }
