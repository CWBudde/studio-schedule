package domain

import "testing"

// darkPrimaryForeground is the dark theme's text on the primary color
// (web/src/index.css).
var darkPrimaryForeground = mustOKLCh(0.2, 0.03, 345)

// TestDarkAccentContrast checks the guarantee that spares the dark variant a
// check of its own (Settings.validateAccent): whatever the accent, its
// variant reaches MinAccentContrast against the dark theme's text on it. The
// variant depends only on the accent's hue and (capped) chroma, which a grid
// over the RGB cube covers densely.
func TestDarkAccentContrast(t *testing.T) {
	t.Parallel()

	const step = 3

	lowest, at := 21.0, Color{}

	for r := 0; r <= 255; r += step {
		for g := 0; g <= 255; g += step {
			for b := 0; b <= 255; b += step {
				c := Color{R: uint8(r), G: uint8(g), B: uint8(b)}
				if got := ContrastRatio(DarkAccent(c), darkPrimaryForeground); got < lowest {
					lowest, at = got, c
				}
			}
		}
	}

	if lowest < MinAccentContrast {
		t.Fatalf("the dark variant of %s reaches only %.2f:1 against the text on it", at.Hex(), lowest)
	}

	t.Logf("lowest contrast of a dark variant: %.2f:1 (%s)", lowest, at.Hex())
}
