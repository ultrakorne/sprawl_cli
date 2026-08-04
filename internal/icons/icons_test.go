package icons

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/ultrakorne/sprawl_cli/internal/client"
)

// TestGlyphsAreOneCell is the load-bearing test for the whole package: the state
// column is a constant width in both the CLI table and the TUI, which only works
// if every glyph in every set occupies exactly one cell.
func TestGlyphsAreOneCell(t *testing.T) {
	for _, set := range []Set{Nerd, Plain} {
		for _, glyph := range []string{set.Ready, set.Progress, set.Review} {
			if w := lipgloss.Width(glyph); w != Width {
				t.Errorf("glyph %q width = %d, want %d", glyph, w, Width)
			}
		}
	}
	if w := lipgloss.Width(Unknown); w != Width {
		t.Errorf("unknown-state marker width = %d, want %d", w, Width)
	}
}

func TestResolve_EnvOptOut(t *testing.T) {
	t.Setenv("SPRAWL_ICONS", "plain")
	if Resolve() != Plain {
		t.Fatal("SPRAWL_ICONS=plain must select the plain set")
	}
	// Case and surrounding whitespace are tolerated — this is a shell export.
	t.Setenv("SPRAWL_ICONS", "  PLAIN ")
	if Resolve() != Plain {
		t.Fatal("SPRAWL_ICONS is matched case-insensitively and trimmed")
	}
	t.Setenv("SPRAWL_ICONS", "")
	if Resolve() != Nerd {
		t.Fatal("unset SPRAWL_ICONS must select the nerd set")
	}
	// Any other value is not an error — it just isn't the opt-out.
	t.Setenv("SPRAWL_ICONS", "fancy")
	if Resolve() != Nerd {
		t.Fatal("an unrecognised SPRAWL_ICONS value falls back to the nerd set")
	}
}

func TestFor_EveryState(t *testing.T) {
	t.Setenv("SPRAWL_ICONS", "plain")
	for state, want := range map[string]string{
		"":                          "",
		client.StateReadyToPickup:   Plain.Ready,
		client.StateInProgress:      Plain.Progress,
		client.StateInReview:        Plain.Review,
		"a_state_from_the_future_1": Unknown,
	} {
		if got := For(state); got != want {
			t.Errorf("For(%q) = %q, want %q", state, got, want)
		}
	}
}
