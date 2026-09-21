// Package fire mirrors the C# CodingFire.Fire namespace: the pixel heat-field
// simulation, its palette, and the campfire state machine.
package fire

import (
	"math"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// RGBA is a four-component colour, same component order as the Swift version.
type RGBA struct {
	R, G, B, A byte
}

// Clear is fully transparent.
var Clear = RGBA{0, 0, 0, 0}

// PixelPalette is the fixed pixel-art palette. Values correspond one-for-one
// with PixelPalette in the macOS version's PixelCampfireAtlas.swift.
var (
	// Fire maps heat 0…32 to a colour; index 0 is fully transparent.
	Fire = buildFire()

	LogDark  = RGBA{62, 34, 14, 255}
	LogMid   = RGBA{110, 68, 28, 255}
	LogLight = RGBA{148, 98, 44, 255}
	LogEnd   = RGBA{186, 148, 88, 255}
	Ash      = RGBA{78, 74, 70, 255}
	Coal     = RGBA{28, 24, 20, 255}
	Ember    = RGBA{220, 48, 8, 255}
	Spark    = RGBA{255, 236, 120, 255}
)

// buildFire lays out the 33-entry ramp: deep red → red → orange → yellow →
// white-hot, padded to 33 entries.
func buildFire() []RGBA {
	t := make([]RGBA, 0, 33)
	t = append(t, Clear)

	// deep red
	for i := 1; i <= 4; i++ {
		t = append(t, RGBA{b(80 + i*20), b(8 + i*2), 0, 255})
	}
	// red
	for i := 0; i <= 5; i++ {
		t = append(t, RGBA{b(180 + i*10), b(20 + i*8), 0, 255})
	}
	// orange
	for i := 0; i <= 6; i++ {
		t = append(t, RGBA{255, b(70 + i*14), b(i * 4), 255})
	}
	// yellow
	for i := 0; i <= 6; i++ {
		t = append(t, RGBA{255, b(170 + i*8), b(20 + i*12), 255})
	}
	// white-hot
	for i := 0; i <= 5; i++ {
		t = append(t, RGBA{255, b(230 + i*4), b(140 + i*18), 255})
	}
	// pad out to 33 levels
	for len(t) < 33 {
		t = append(t, RGBA{255, 252, 230, 255})
	}
	return t
}

func b(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// ---------------------------------------------------------------------------
// Per-source accents
// ---------------------------------------------------------------------------

// sourceFlameColors assigns each tool a flame theme colour. Colours are spread
// around the hue wheel by the golden angle, so adding a source automatically
// gets a non-colliding colour instead of someone hand-picking a 22nd value.
//
// NOTE: this no longer tints the fire — the flame is single-colour, following
// Settings.FlameColor. These accents now only drive the small source dots in
// the console and the leading dot on each hover-card row.
type sourceFlameColors struct {
	settings *core.Settings
	builtIn  map[core.UsageSource]core.AccentRGB
}

// SourceFlameColors is the exported singleton.
var SourceFlameColors = newSourceFlameColors()

// goldenAngle spreads successive hues evenly around the wheel.
const goldenAngle = 2.3999632297286533

func newSourceFlameColors() *sourceFlameColors {
	m := make(map[core.UsageSource]core.AccentRGB, len(core.UsageSourcesAll))
	for i, s := range core.UsageSourcesAll {
		m[s] = VibrantColor(i)
	}
	return &sourceFlameColors{builtIn: m}
}

// VibrantColor derives a saturated, bright, deterministic colour from an
// index. Same index always yields the same colour.
func VibrantColor(index int) core.AccentRGB {
	// Offset the start angle so index 0 does not land in the red zone (red
	// reads too much like a classic flame). +3 ≈ 129°.
	hue := math.Mod(float64(index+3)*goldenAngle, 2*math.Pi)
	// Saturation wanders in 0.72–0.95 so not every dot is equally bright.
	sat := 0.72 + 0.23*math.Sin(hue*3.7+1.3)*0.5 + 0.5
	// Value wanders in 0.62–0.88.
	val := 0.62 + 0.26*math.Sin(hue*2.9+0.7)*0.5 + 0.5
	return HsvToRgb(hue, sat, val)
}

// HsvToRgb converts a hue in radians plus saturation/value to normalised RGB.
func HsvToRgb(h, s, v float64) core.AccentRGB {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/(math.Pi/3), 2)-1))
	m := v - c

	var r1, g1, b1 float64
	sector := int(h/(math.Pi/3)) % 6
	switch sector {
	case 0:
		r1, g1, b1 = c, x, 0
	case 1:
		r1, g1, b1 = x, c, 0
	case 2:
		r1, g1, b1 = 0, c, x
	case 3:
		r1, g1, b1 = 0, x, c
	case 4:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	return core.AccentRGB{clamp01(r1 + m), clamp01(g1 + m), clamp01(b1 + m)}
}

// Attach binds the settings object so user edits take effect immediately.
func (s *sourceFlameColors) Attach(settings *core.Settings) { s.settings = settings }

// Accent returns the effective colour for a source: the user's override when
// present, else the generated built-in.
func (s *sourceFlameColors) Accent(source core.UsageSource) core.AccentRGB {
	key := source.Raw()
	if s.settings != nil {
		if custom, ok := s.settings.SourceColors[key]; ok {
			return custom
		}
	}
	if builtin, ok := s.builtIn[source]; ok {
		return builtin
	}
	return core.AccentRGB{1.0, 0.45, 0.12}
}

// SetAccent stores a user override for one source.
func (s *sourceFlameColors) SetAccent(source core.UsageSource, r, g, bl float64) {
	if s.settings == nil {
		return
	}
	s.settings.SourceColors[source.Raw()] = core.AccentRGB{clamp01(r), clamp01(g), clamp01(bl)}
}

// ResetAll drops every user override.
func (s *sourceFlameColors) ResetAll() {
	if s.settings == nil {
		return
	}
	s.settings.SourceColors = map[string]core.AccentRGB{}
}

// IsCustom reports whether a source has a user override.
func (s *sourceFlameColors) IsCustom(source core.UsageSource) bool {
	if s.settings == nil {
		return false
	}
	_, ok := s.settings.SourceColors[source.Raw()]
	return ok
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ---------------------------------------------------------------------------
// Accent ramps
// ---------------------------------------------------------------------------

// FlamePaletteBuilder builds the heat ramp from a flame accent colour.
type flamePaletteBuilder struct{}

// FlamePaletteBuilder is the exported singleton.
var FlamePaletteBuilder flamePaletteBuilder

// Classic is the classic Doom ramp, used when no accent is set.
func (flamePaletteBuilder) Classic() []RGBA { return Fire }

// Ramp keeps the flame's brightness curve while letting the accent colour stay
// recognisable all the way up.
//
// The key difference from the older ramp: the mid→hot band no longer forces a
// convergence to orange, so the theme colour stays legible from the base to
// just below the tip. Only the top 3 levels (h≥30) go white-hot, which is what
// keeps the whole thing reading as "one fire".
func (flamePaletteBuilder) Ramp(accent core.AccentRGB) []RGBA {
	table := make([]RGBA, 0, 33)
	table = append(table, Clear)
	ar, ag, ab := accent[0], accent[1], accent[2]

	for h := 1; h <= 32; h++ {
		u := float64(h) / 32.0
		var r, g, bl float64

		switch {
		case u < 0.22:
			k := u / 0.22
			dark := 0.22 + k*0.24
			r, g, bl = ar*dark, ag*dark, ab*dark

		case u < 0.50:
			k := (u - 0.22) / 0.28
			v := 0.46 + (0.78-0.46)*k
			r, g, bl = ar*v, ag*v, ab*v

		case u < 0.78:
			k := (u - 0.50) / 0.28
			v := 0.78 + (0.98-0.78)*k
			r = math.Max(ar*v, ar*0.35)
			g = math.Max(ag*v, ag*0.35)
			bl = math.Max(ab*v, ab*0.35)

		case u < 0.94:
			k := (u - 0.78) / 0.16
			srcR := math.Min(1, ar*0.95)
			srcG := math.Min(1, ag*0.95)
			srcB := math.Min(1, ab*0.95)
			r = srcR + (1.0-srcR)*k
			g = srcG + (0.98-srcG)*k
			bl = srcB + (0.94-srcB)*k

		default:
			k := (u - 0.94) / 0.06
			r = 1.0 + (1.0-1.0)*k
			g = 0.97 + (0.99-0.97)*k
			bl = 0.88 + (0.96-0.88)*k
		}
		table = append(table, toRGBA(r, g, bl))
	}
	return table
}

func toRGBA(r, g, b float64) RGBA {
	return RGBA{clamp255(r * 255), clamp255(g * 255), clamp255(b * 255), 255}
}

// clamp255 rounds half away from zero, matching the C# version exactly.
// Go's math.Round already rounds halves away from zero.
func clamp255(v float64) byte {
	i := math.Round(v)
	if i < 0 {
		return 0
	}
	if i > 255 {
		return 255
	}
	return byte(i)
}
