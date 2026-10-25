package domain_test

import (
	"math"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func TestParseColor(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{"#7a1f3d": "#7a1f3d", "#7A1F3D": "#7a1f3d", "#000000": "#000000"} {
		c, ok := domain.ParseColor(in)
		if !ok || c.Hex() != want {
			t.Errorf("ParseColor(%q) = %v, %v; want %s", in, c.Hex(), ok, want)
		}
	}

	for _, in := range []string{"", "7a1f3d", "#7a1f3", "#7a1f3d0", "#7a1g3d", "red", "#fff", " #7a1f3d"} {
		if _, ok := domain.ParseColor(in); ok {
			t.Errorf("ParseColor(%q) accepted", in)
		}
	}
}

func TestContrastRatio(t *testing.T) {
	t.Parallel()

	black, _ := domain.ParseColor("#000000")
	white, _ := domain.ParseColor("#ffffff")
	grey, _ := domain.ParseColor("#767676") // the classic AA grey on white

	for _, tc := range []struct {
		a, b domain.Color
		want float64
	}{
		{black, white, 21},
		{white, black, 21},
		{white, white, 1},
		{grey, white, 4.54},
	} {
		if got := domain.ContrastRatio(tc.a, tc.b); math.Abs(got-tc.want) > 0.01 {
			t.Errorf("ContrastRatio(%s, %s) = %.3f, want %.2f", tc.a.Hex(), tc.b.Hex(), got, tc.want)
		}
	}
}

// The vectors are shared with web/src/lib/color.test.ts: both sides must
// derive the same dark variant and judge the contrast alike.
func TestAccent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		accent, dark string
		contrast     float64
	}{
		{"#7a1f3d", "#ee9cae", 9.42},
		{"#1d4ed8", "#97b7f8", 6.28},
		{"#0f766e", "#72cac0", 5.13},
		{"#b45309", "#eca47e", 4.71},
		{"#ff0000", "#f19f92", 3.75},
		{"#808080", "#b7b7b7", 3.70},
		{"#000000", "#b7b7b7", 19.69},
	} {
		c, _ := domain.ParseColor(tc.accent)
		if got := domain.DarkAccent(c).Hex(); got != tc.dark {
			t.Errorf("DarkAccent(%s) = %s, want %s", tc.accent, got, tc.dark)
		}

		if got := domain.AccentContrast(c); math.Abs(got-tc.contrast) > 0.01 {
			t.Errorf("AccentContrast(%s) = %.3f, want %.2f", tc.accent, got, tc.contrast)
		}
	}
}
