package fire

import (
	"math/rand/v2"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// Fire canvas and log sprite dimensions, in simulation pixels.
const (
	FireW = 28
	FireH = 36
	LogW  = 28
	LogH  = 12
)

// PixelFireEngine is a Doom-style pixel heat field: each frame it lifts the
// row below upward, nudges it sideways with the wind, and cools it by tier.
// The logic corresponds line-for-line with the macOS PixelFireEngine; only the
// output buffer format differs.
//
// The C# version emitted BGRA because that is Windows' 32bpp bitmap layout.
// go-gui wants straight-alpha NRGBA, so this engine emits R,G,B,A order
// directly and no channel swizzle is needed on the way out.
type PixelFireEngine struct {
	Width  int
	Height int

	heat []byte
	pix  []byte
	rng  *rand.Rand

	// Intensity is 0…1: the width and heat of the source at the base.
	Intensity float64
	Tier      core.FireTier
	Phase     core.FirePhase
	EmberHeat float64
	// FlameAccent is the user's chosen flame colour, normalised RGB.
	FlameAccent core.AccentRGB
	// AccentEpoch increments when the user changes the flame colour, which
	// triggers a ramp rebuild.
	AccentEpoch int

	cachedAccentEpoch int
	accentRamp        []RGBA
}

// NewPixelFireEngine allocates an engine and builds its initial ramp.
func NewPixelFireEngine(width, height int) *PixelFireEngine {
	e := &PixelFireEngine{
		Width:             width,
		Height:            height,
		heat:              make([]byte, width*height),
		pix:               make([]byte, width*height*4),
		rng:               rand.New(rand.NewPCG(0x5eed, 0x1ce1ce)),
		Intensity:         0.4,
		Tier:              core.TierCrackle,
		Phase:             core.PhaseFlame,
		FlameAccent:       core.DefaultFlameAccent,
		cachedAccentEpoch: -1,
	}
	e.RebuildColorCaches()
	return e
}

// Seed makes the simulation reproducible. Used by --render so a release's
// preview PNGs are stable; the live app never calls it.
func (e *PixelFireEngine) Seed(seed uint64) {
	e.rng = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
}

// Pix returns the RGBA8 buffer. Length is Width*Height*4.
func (e *PixelFireEngine) Pix() []byte { return e.pix }

// Reset clears the heat field.
func (e *PixelFireEngine) Reset() {
	for i := range e.heat {
		e.heat[i] = 0
	}
}

// ---------------------------------------------------------------------------
// Simulation
// ---------------------------------------------------------------------------

// Step advances the heat field by one frame.
func (e *PixelFireEngine) Step() {
	w, h := e.Width, e.Height

	// Rise: each cell takes heat from below, offset by wind, minus cooling.
	for y := 0; y < h-1; y++ {
		for x := 0; x < w; x++ {
			below := int(e.heat[(y+1)*w+x])
			wind := e.rng.IntN(3) - 1
			dstX := x + wind
			if dstX < 0 {
				dstX = 0
			} else if dstX > w-1 {
				dstX = w - 1
			}

			var cool int
			switch e.Tier {
			case core.TierHush:
				cool = 2 + e.rng.IntN(3) // 2..4
			case core.TierGlow, core.TierCrackle:
				cool = 1 + e.rng.IntN(3) // 1..3
			case core.TierRoar:
				cool = 1 + e.rng.IntN(2) // 1..2
			default:
				cool = e.rng.IntN(3) // 0..2
			}
			next := below - cool
			if next < 0 {
				next = 0
			}
			e.heat[y*w+dstX] = byte(next)
		}
	}

	bottom := (h - 1) * w
	for x := 0; x < w; x++ {
		e.heat[bottom+x] = 0
	}

	switch e.Phase {
	case core.PhaseUnlit, core.PhaseOut:
		// nothing to seed
	case core.PhaseEmber:
		e.seedEmbers(bottom)
	default:
		e.seedFlame(bottom)
	}

	e.applyHeightCap()
}

func (e *PixelFireEngine) seedFlame(bottom int) {
	w := e.Width
	// A tiny per-frame centre drift prevents the base from looking like a
	// rigid symmetric column while keeping the log contact stable.
	cx := w/2 + e.rng.IntN(3) - 1

	var half int
	switch e.Tier {
	case core.TierHush:
		half = 2
	case core.TierGlow:
		half = 3
	case core.TierCrackle:
		half = 5
	case core.TierRoar:
		half = 7
	default:
		half = 10
	}

	var basePeak int
	switch e.Tier {
	case core.TierHush:
		basePeak = 18
	case core.TierGlow:
		basePeak = 22
	case core.TierCrackle:
		basePeak = 26
	case core.TierRoar:
		basePeak = 30
	default:
		basePeak = 32
	}
	// Small breathing variation keeps successive frames from having the same
	// flat peak. The heat field still clamps to the same 0..32 palette range.
	peak := int(float64(basePeak) * (0.55 + e.Intensity*0.55) * (0.94 + e.rng.Float64()*0.12))
	if peak > 32 {
		peak = 32
	}

	for dx := -half; dx <= half; dx++ {
		x := cx + dx
		if x < 0 || x >= w {
			continue
		}
		edge := absInt(dx) == half
		v := peak - absInt(dx)*2
		if edge {
			v -= 4
		}
		v += e.rng.IntN(5) - 2
		// Roaring fires occasionally flare.
		if (e.Tier == core.TierBlaze || e.Tier == core.TierRoar) && e.rng.IntN(9) == 0 {
			v += 6
			if v > 32 {
				v = 32
			}
		}
		if v < 0 {
			v = 0
		}
		e.heat[bottom+x] = byte(v)
	}

	// Reinforce the second row so the fire base reads thicker.
	if e.Height >= 2 && half > 1 {
		row := (e.Height - 2) * w
		for dx := -(half - 1); dx <= half-1; dx++ {
			x := cx + dx
			if x < 0 || x >= w {
				continue
			}
			cur := int(e.heat[row+x])
			boost := peak/2 - absInt(dx)
			if boost < 0 {
				boost = 0
			}
			if cur > boost {
				e.heat[row+x] = byte(cur)
			} else {
				e.heat[row+x] = byte(boost)
			}
		}
	}

	// Add a few narrower secondary tongues above the main base. A single
	// symmetric heat mound reads like a torch; overlapping tongues with their
	// own drift and height are what make a campfire read as turbulent and alive.
	if e.Intensity > 0.08 && half > 2 {
		tongues := 1 + int(e.Intensity*3)
		maxRise := e.Height / 2
		if maxRise < 4 {
			maxRise = 4
		}
		for i := 0; i < tongues; i++ {
			center := cx + e.rng.IntN(2*half-1) - (half - 1)
			row := bottom - (2 + e.rng.IntN(maxRise))
			if row < 0 || row >= e.Height {
				continue
			}
			tongueHalf := 1 + e.rng.IntN(maxInt(2, half/2))
			tonguePeak := int(float64(peak) * (0.30 + e.rng.Float64()*0.30))
			for dx := -tongueHalf; dx <= tongueHalf; dx++ {
				x := center + dx
				if x < 0 || x >= w {
					continue
				}
				v := tonguePeak - absInt(dx)*3 + e.rng.IntN(5) - 2
				if v < 0 {
					v = 0
				}
				if v > int(e.heat[row*w+x]) {
					e.heat[row*w+x] = byte(v)
				}
			}
		}
	}
}

func (e *PixelFireEngine) seedEmbers(bottom int) {
	w := e.Width
	cx := w / 2
	n := 3 + int(e.EmberHeat*4)
	for i := 0; i < n; i++ {
		x := cx - n/2 + i
		if x < 0 || x >= w {
			continue
		}
		pulse := int(10+e.EmberHeat*12) + (e.rng.IntN(7) - 3)
		if e.rng.IntN(3) == 0 {
			if pulse < 0 {
				pulse = 0
			}
			if pulse > 255 {
				pulse = 255
			}
			e.heat[bottom+x] = byte(pulse)
		}
	}
}

// applyHeightCap limits how tall the fire may grow, fading the top two rows
// rather than cutting hard.
func (e *PixelFireEngine) applyHeightCap() {
	var maxRows int
	switch e.Phase {
	case core.PhaseUnlit, core.PhaseOut:
		maxRows = 0
	case core.PhaseEmber:
		maxRows = 4
	default:
		switch e.Tier {
		case core.TierHush:
			maxRows = 8
		case core.TierGlow:
			maxRows = 14
		case core.TierCrackle:
			maxRows = 22
		case core.TierRoar:
			maxRows = 30
		default:
			maxRows = e.Height
		}
	}

	cut := e.Height - maxRows
	if cut <= 0 {
		return
	}
	w := e.Width
	for y := 0; y < cut; y++ {
		dist := cut - y
		for x := 0; x < w; x++ {
			if dist > 2 {
				e.heat[y*w+x] = 0
			} else {
				e.heat[y*w+x] = byte(int(e.heat[y*w+x]) / (4 - dist))
			}
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ---------------------------------------------------------------------------
// Colouring
// ---------------------------------------------------------------------------

// RefreshPaletteIfNeeded rebuilds the ramp when the accent epoch moved.
func (e *PixelFireEngine) RefreshPaletteIfNeeded() {
	if e.accentRamp == nil || e.AccentEpoch != e.cachedAccentEpoch {
		e.RebuildColorCaches()
	}
}

// RebuildColorCaches regenerates the accent ramp.
func (e *PixelFireEngine) RebuildColorCaches() {
	e.cachedAccentEpoch = e.AccentEpoch
	e.accentRamp = FlamePaletteBuilder.Ramp(e.FlameAccent)
}

// Render colours the heat field into the RGBA buffer. The caller composites it.
func (e *PixelFireEngine) Render() {
	e.RefreshPaletteIfNeeded()
	count := e.Width * e.Height

	for i := 0; i < count; i++ {
		h := int(e.heat[i])
		if h > 32 {
			h = 32
		}
		o := i * 4
		if h == 0 {
			e.pix[o], e.pix[o+1], e.pix[o+2], e.pix[o+3] = 0, 0, 0, 0
			continue
		}
		c := e.accentRamp[h]
		e.pix[o] = c.R
		e.pix[o+1] = c.G
		e.pix[o+2] = c.B
		// Low heat is the translucent edge of the flame, not a solid pixel.
		// Keeping the hot core opaque preserves brightness while the fringe
		// blends into the air naturally after the renderer's interpolation.
		alpha := h * 22
		if alpha > int(c.A) {
			alpha = int(c.A)
		}
		e.pix[o+3] = byte(alpha)
	}
}

// ---------------------------------------------------------------------------
// Sprites
// ---------------------------------------------------------------------------

// Sprite is a small RGBA pixel image.
type Sprite struct {
	W, H int
	Pix  []RGBA
}

// At reads a pixel; out-of-range reads return Clear.
func (s Sprite) At(x, y int) RGBA {
	if y < 0 || y >= s.H || x < 0 || x >= s.W {
		return Clear
	}
	return s.Pix[y*s.W+x]
}

func newSprite(w, h int) Sprite {
	return Sprite{W: w, H: h, Pix: make([]RGBA, w*h)}
}

func (s Sprite) put(x, y int, c RGBA) {
	if y < 0 || y >= s.H || x < 0 || x >= s.W {
		return
	}
	s.Pix[y*s.W+x] = c
}

// stamp paints an inclusive horizontal run on one row.
func (s Sprite) stamp(row, x0, x1 int, c RGBA) {
	for x := x0; x <= x1; x++ {
		s.put(x, row, c)
	}
}

// CampfireSprites holds the static pixel art for the log pile and sparks.
type campfireSprites struct{}

// CampfireSprites is the exported singleton.
var CampfireSprites campfireSprites

// Log draws the log pile. The front log's top face is flat — that is where the
// flame sits.
func (campfireSprites) Log() Sprite {
	g := newSprite(LogW, LogH)
	g.stamp(8, 4, 23, Ash)
	g.stamp(9, 3, 24, Coal)
	g.stamp(10, 5, 22, Ash)
	g.stamp(11, 7, 20, Coal)
	g.put(8, 9, Ember)
	g.put(14, 9, Ember)
	g.put(19, 9, Ember)

	// back log, one layer lower
	g.stamp(6, 2, 25, LogDark)
	g.stamp(7, 2, 25, LogMid)
	g.put(2, 6, LogEnd)
	g.put(2, 7, LogEnd)
	g.put(25, 6, LogEnd)
	g.put(25, 7, LogLight)

	// front log — flat top, the flame rests on it
	g.stamp(4, 3, 24, LogLight)
	g.stamp(5, 3, 24, LogMid)
	g.put(3, 4, LogEnd)
	g.put(3, 5, LogEnd)
	g.put(24, 4, LogEnd)
	g.put(24, 5, LogLight)
	return g
}

// Spark is the 3x3 ember sprite.
func (campfireSprites) Spark() Sprite {
	g := newSprite(3, 3)
	g.put(1, 1, Spark)
	g.put(1, 0, Spark)
	g.put(0, 1, RGBA{255, 180, 40, 200})
	g.put(2, 1, RGBA{255, 180, 40, 180})
	return g
}
