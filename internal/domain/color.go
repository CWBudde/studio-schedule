package domain

import (
	"fmt"
	"math"
	"strconv"
)

// Color is an sRGB color with 8 bits per channel, written "#rrggbb".
type Color struct{ R, G, B uint8 }

// ParseColor parses "#rrggbb" (either case). Short forms and names are not
// colors here: the accent is stored and compared in one spelling.
func ParseColor(s string) (Color, bool) {
	if len(s) != 7 || s[0] != '#' {
		return Color{}, false
	}

	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return Color{}, false
	}

	return Color{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}, true //nolint:gosec // 24 bits
}

// Hex returns the color as "#rrggbb" in lower case.
func (c Color) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// luminance is the relative luminance of WCAG 2.
func (c Color) luminance() float64 {
	return 0.2126*linear(c.R) + 0.7152*linear(c.G) + 0.0722*linear(c.B)
}

// ContrastRatio is the WCAG 2 contrast ratio of two colors, from 1 to 21.
func ContrastRatio(a, b Color) float64 {
	la, lb := a.luminance(), b.luminance()
	if la < lb {
		la, lb = lb, la
	}

	return (la + 0.05) / (lb + 0.05)
}

// The theme colors an accent must stand out against (web/src/index.css):
// the light theme's text on the primary color and its page background.
var (
	lightPrimaryForeground = mustOKLCh(0.985, 0.008, 75)
	lightBackground        = mustOKLCh(0.978, 0.008, 75)
)

// MinAccentContrast is the contrast the accent needs against the text on it
// and the page: WCAG 2 AA for normal text.
const MinAccentContrast = 4.5

// AccentContrast is the lower contrast of an accent against the light
// theme's text on the primary color and its page background, where the
// accent is also a text color (links, the current page).
func AccentContrast(c Color) float64 {
	return min(ContrastRatio(c, lightPrimaryForeground), ContrastRatio(c, lightBackground))
}

// The dark theme shows the accent lighter and calmer, as its built-in
// primary color: at this OKLab lightness and at most this chroma.
const (
	darkAccentLightness = 0.78
	darkAccentMaxChroma = 0.1
)

// DarkAccent derives the dark theme's variant of an accent: the same hue
// at a fixed lightness, with the chroma capped and reduced further until the
// color fits into sRGB. The fixed lightness is what keeps the dark theme's
// text on it readable: every variant reaches MinAccentContrast against it
// (TestDarkAccentContrast), so the variant needs no check of its own.
func DarkAccent(c Color) Color {
	_, chroma, hue := c.okLCh()
	chroma = min(chroma, darkAccentMaxChroma)

	for ; chroma > 0; chroma -= 0.002 {
		if out, ok := fromOKLCh(darkAccentLightness, chroma, hue); ok {
			return out
		}
	}

	out, _ := fromOKLCh(darkAccentLightness, 0, 0)

	return out
}

// okLCh converts to OKLCh (lightness 0–1, chroma, hue in degrees).
func (c Color) okLCh() (l, chroma, hue float64) {
	r, g, b := linear(c.R), linear(c.G), linear(c.B)

	lm := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	mm := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	sm := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)

	l = 0.2104542553*lm + 0.7936177850*mm - 0.0040720468*sm
	a := 1.9779984951*lm - 2.4285922050*mm + 0.4505937099*sm
	bb := 0.0259040371*lm + 0.7827717662*mm - 0.8086757660*sm

	hue = math.Atan2(bb, a) * 180 / math.Pi
	if hue < 0 {
		hue += 360
	}

	return l, math.Hypot(a, bb), hue
}

// fromOKLCh converts from OKLCh; ok is false if the color lies outside
// sRGB (then the channels are clipped).
func fromOKLCh(l, chroma, hue float64) (Color, bool) {
	a := chroma * math.Cos(hue*math.Pi/180)
	b := chroma * math.Sin(hue*math.Pi/180)

	lm := l + 0.3963377774*a + 0.2158037573*b
	mm := l - 0.1055613458*a - 0.0638541728*b
	sm := l - 0.0894841775*a - 1.2914855480*b
	lm, mm, sm = lm*lm*lm, mm*mm*mm, sm*sm*sm

	r := 4.0767416621*lm - 3.3077115913*mm + 0.2309699292*sm
	g := -1.2684380046*lm + 2.6097574011*mm - 0.3413193965*sm
	bl := -0.0041960863*lm - 0.7034186147*mm + 1.7076147010*sm

	const eps = 1e-4

	ok := true
	for _, v := range []float64{r, g, bl} {
		if v < -eps || v > 1+eps {
			ok = false
		}
	}

	return Color{R: gamma(r), G: gamma(g), B: gamma(bl)}, ok
}

func mustOKLCh(l, chroma, hue float64) Color {
	c, ok := fromOKLCh(l, chroma, hue)
	if !ok {
		panic(fmt.Sprintf("domain: oklch(%v %v %v) is outside sRGB", l, chroma, hue))
	}

	return c
}

// linear is the linear value (0–1) of an sRGB channel.
func linear(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}

	return math.Pow((c+0.055)/1.055, 2.4)
}

// gamma is the sRGB channel of a linear value, clipped to 0–255.
func gamma(v float64) uint8 {
	v = min(max(v, 0), 1)
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}

	return uint8(math.Round(v * 255))
}
