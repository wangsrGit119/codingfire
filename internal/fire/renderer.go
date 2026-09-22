package fire

import (
	"math"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// DpiScale is the process DPI scale. Pixel art must land 1:1 or it looks
// blurry, so the panel is sized in physical pixels. The UI layer sets this at
// startup; it stays 1.0 when the query fails.
var DpiScale = 1.0

// CampfireRenderer composites the heat field, the log pile and the sparks into
// one RGBA frame. Layout corresponds item for item with the macOS FireScene:
//
//	logNode   : centred on the scene origin
//	flameNode : bottom-anchored, sitting at flameBaseY - px
//	sparkNodes: count and rise speed driven by tier
//
// The C# version emitted premultiplied BGRA because that is what
// UpdateLayeredWindow demands. go-gui's in-memory images are straight-alpha
// NRGBA8, so this composites source-over in straight alpha instead and needs no
// premultiply step on the way out.
type CampfireRenderer struct {
	engine *PixelFireEngine
	logArt Sprite
	spark  Sprite

	width  int
	height int
	pix    []byte

	flameAlpha   float64
	lastStepTime float64
	lastTier     core.FireTier
	lastPhase    core.FirePhase
	firstFrame   bool
	dragging     bool

	// FlameTipY / FlameBaseY locate the flame inside the panel, for positioning
	// the hover card.
	FlameTipY  int
	FlameBaseY int
}

// NewCampfireRenderer returns a renderer with a fresh heat field.
func NewCampfireRenderer() *CampfireRenderer {
	return &CampfireRenderer{
		engine:     NewPixelFireEngine(FireW, FireH),
		logArt:     CampfireSprites.Log(),
		spark:      CampfireSprites.Spark(),
		firstFrame: true,
	}
}

// Width returns the current panel width in pixels.
func (r *CampfireRenderer) Width() int { return r.width }

// Height returns the current panel height in pixels.
func (r *CampfireRenderer) Height() int { return r.height }

// Pix returns the RGBA8 frame buffer.
func (r *CampfireRenderer) Pix() []byte { return r.pix }

// Engine exposes the heat field.
func (r *CampfireRenderer) Engine() *PixelFireEngine { return r.engine }

// Resize reallocates the panel.
func (r *CampfireRenderer) Resize(width, height int) {
	if width == r.width && height == r.height && r.pix != nil {
		return
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	r.width, r.height = width, height
	r.pix = make([]byte, width*height*4)
}

// ResetEngine clears the heat field.
func (r *CampfireRenderer) ResetEngine() { r.engine.Reset() }

// SetDragging marks whether the user is dragging the panel.
func (r *CampfireRenderer) SetDragging(dragging bool) { r.dragging = dragging }

// Render composites one frame. timeSeconds is a monotonic clock in seconds.
func (r *CampfireRenderer) Render(snap core.FireSnapshot, pixelScale float64, reduceMotion bool, timeSeconds float64) {
	if r.pix == nil {
		return
	}

	r.engine.Intensity = snap.Intensity
	r.engine.Tier = snap.Tier
	r.engine.Phase = snap.Phase
	r.engine.EmberHeat = snap.EmberHeat
	if snap.FlameAccent != (core.AccentRGB{}) && snap.FlameAccent != r.engine.FlameAccent {
		r.engine.FlameAccent = snap.FlameAccent
		r.engine.AccentEpoch++ // triggers a ramp rebuild
	}

	phaseChanged := snap.Phase != r.lastPhase
	tierChanged := snap.Tier != r.lastTier
	r.lastPhase = snap.Phase
	r.lastTier = snap.Tier

	// Snap the alpha on a phase/tier change, ease it otherwise, so switching
	// states does not flicker.
	var targetAlpha float64
	switch snap.Phase {
	case core.PhaseUnlit, core.PhaseOut:
		targetAlpha = 0
	case core.PhaseEmber:
		targetAlpha = 0.85
	default:
		targetAlpha = 1.0
	}
	if phaseChanged || tierChanged {
		r.flameAlpha = targetAlpha
	} else {
		r.flameAlpha += (targetAlpha - r.flameAlpha) * 0.35
	}

	if snap.Phase == core.PhaseUnlit || snap.Phase == core.PhaseOut {
		if r.flameAlpha < 0.02 {
			r.flameAlpha = 0
			r.engine.Reset()
		}
	}

	// Simulation stepping: match the UI's 10fps flame cadence.
	stepFPS := 10.0
	if reduceMotion {
		stepFPS = 6
	}
	if r.lastStepTime == 0 {
		r.lastStepTime = timeSeconds
	}
	if r.firstFrame || timeSeconds-r.lastStepTime >= 1.0/stepFPS {
		r.firstFrame = false
		r.lastStepTime = timeSeconds
		if snap.Phase == core.PhaseFlame || snap.Phase == core.PhaseEmber || r.flameAlpha > 0 {
			r.engine.Step()
			r.engine.Render()
		}
	}

	r.composeFrame(snap, pixelScale, reduceMotion, timeSeconds)
}

func (r *CampfireRenderer) composeFrame(snap core.FireSnapshot, px float64, reduceMotion bool, timeSeconds float64) {
	px *= DpiScale

	panelW, panelH := r.width, r.height

	originX := panelW / 2
	originY := int(math.Round(float64(panelH) * 0.72))

	logW := float64(LogW) * px
	logH := float64(LogH) * px
	flameW := float64(FireW) * px
	flameH := float64(FireH) * px
	flameBaseY := float64(LogH)*px*0.5 - 4*px

	// Side-to-side flame sway. Use continuous intensity instead of only the
	// discrete tier, then layer a faster smaller oscillation over it. That makes
	// the fire breathe within a tier instead of waiting for a threshold jump.
	var jitter float64
	if snap.Phase == core.PhaseFlame && !reduceMotion {
		amp := (0.25 + snap.Intensity*1.75) * px / 3.5
		jitter = math.Round(
			math.Sin(timeSeconds*2.2)*amp +
				math.Sin(timeSeconds*4.9+0.8)*amp*0.28)
	}

	// A subtle whole-flame flicker modulates the edge alpha. The heat field
	// still supplies the shape and colour; this only prevents a frame from
	// looking like a perfectly flat opaque cutout.
	flameFlicker := 1.0
	if snap.Phase == core.PhaseFlame && !reduceMotion {
		flameFlicker = 0.94 +
			0.035*math.Sin(timeSeconds*7.0) +
			0.025*math.Sin(timeSeconds*12.7+1.4)
	}

	// Start from a fully transparent frame.
	for i := range r.pix {
		r.pix[i] = 0
	}

	// ---- log pile ----
	logX := int(math.Round(float64(originX) - logW/2))
	logY := int(math.Round(float64(originY) - logH/2))
	r.blitSprite(logX, logY, int(math.Round(logW)), int(math.Round(logH)), r.logArt, 1.0)

	// ---- flame ----
	if r.flameAlpha > 0.01 {
		flameX := int(math.Round(float64(originX) - flameW/2 + jitter))
		flameY := int(math.Round(float64(originY) - (flameBaseY - px) - flameH))
		r.blitEngine(flameX, flameY, int(math.Round(flameW)), int(math.Round(flameH)), r.flameAlpha*flameFlicker)

		r.FlameBaseY = int(math.Round(float64(originY) - (flameBaseY - px)))
		r.FlameTipY = flameY
	}

	// ---- sparks ----
	r.renderSparks(snap, px, reduceMotion, timeSeconds, float64(originX), float64(originY), flameBaseY)
}

func (r *CampfireRenderer) renderSparks(snap core.FireSnapshot, px float64, reduceMotion bool, timeSeconds, originX, originY, flameBaseY float64) {
	const sparkCount = 12
	var active int
	switch snap.Phase {
	case core.PhaseFlame:
		var baseCount int
		switch snap.Tier {
		case core.TierHush:
			baseCount = 0
		case core.TierGlow:
			baseCount = 1
		case core.TierCrackle:
			baseCount = 3
		case core.TierRoar:
			baseCount = 6
		default:
			baseCount = 10
		}
		if reduceMotion {
			active = maxInt(0, baseCount/2)
		} else {
			active = minInt(sparkCount, baseCount+int(snap.SparkBurst*2))
		}
	case core.PhaseEmber:
		if snap.EmberHeat > 0.55 {
			active = 1
		}
	default:
		return
	}
	if active <= 0 {
		return
	}

	var riseMax float64
	switch snap.Tier {
	case core.TierHush:
		riseMax = 10 * px
	case core.TierGlow:
		riseMax = 14 * px
	case core.TierCrackle:
		riseMax = 20 * px
	case core.TierRoar:
		riseMax = 26 * px
	default:
		riseMax = 32 * px
	}

	sparkW := maxInt(1, int(math.Round(px)))
	sparkH := maxInt(2, int(math.Round(px*2)))

	panelW, panelH := r.width, r.height

	for i := 0; i < active; i++ {
		seed := float64(i)*1.7 + tierSeed(snap.Tier)
		speed := 12.0 + snap.Intensity*14.0
		t := timeSeconds*speed*0.07 + seed
		rise := math.Mod(t*9, math.Max(1.0, riseMax))
		sway := math.Round(math.Sin(t*2.1+seed) * px * 0.5)
		spread := float64(i-active/2) * px

		// The scene origin sits at panel (originX, originY) with +y upward.
		sceneX := sway + spread
		sceneY := flameBaseY + 4 + rise
		cx := int(math.Round(originX + sceneX))
		cy := int(math.Round(originY - sceneY))

		life := 1 - rise/math.Max(1.0, riseMax)
		alpha := math.Max(0, life*(0.5+snap.Intensity*0.5))
		if alpha <= 0.02 {
			continue
		}

		// Spark trail: the tail fades with rise speed.
		trailLen := math.Max(2, speed*0.08)
		trailAlpha := alpha * 0.25
		for tr := 1; tr <= 3; tr++ {
			fade := 1.0 - float64(tr)*0.32
			ty := cy + int(float64(tr)*trailLen*0.4)
			if ty >= panelH {
				break
			}
			r.blitSprite(cx-sparkW/2, ty-sparkH/2, sparkW, sparkH, r.spark, trailAlpha*fade)
		}

		r.blitSprite(cx-sparkW/2, cy-sparkH/2, sparkW, sparkH, r.spark, alpha)
		_ = panelW
	}
}

func tierSeed(t core.FireTier) float64 {
	switch t {
	case core.TierHush:
		return 4
	case core.TierGlow:
		return 4
	case core.TierCrackle:
		return 7
	case core.TierRoar:
		return 4
	default:
		return 5
	}
}

// blitEngine scales the heat field with bilinear sampling. The simulation stays
// intentionally small and fast, while interpolation removes the visible block
// edges when the flame is enlarged for the overlay window.
func (r *CampfireRenderer) blitEngine(dx, dy, dw, dh int, alphaMul float64) {
	src := r.engine.Pix()
	sw, sh := r.engine.Width, r.engine.Height
	if dw <= 0 || dh <= 0 {
		return
	}
	for y := 0; y < dh; y++ {
		ty := dy + y
		if ty < 0 || ty >= r.height {
			continue
		}
		for x := 0; x < dw; x++ {
			tx := dx + x
			if tx < 0 || tx >= r.width {
				continue
			}
			rr, gg, bb, a := sampleFlame(src, sw, sh,
				(float64(x)+0.5)*float64(sw)/float64(dw)-0.5,
				(float64(y)+0.5)*float64(sh)/float64(dh)-0.5)
			if a == 0 {
				continue
			}
			if alphaMul < 0.999 {
				a *= alphaMul
			}
			r.blendPixel(tx, ty, rr, gg, bb, a)
		}
	}
}

// sampleFlame applies a small 5-tap filter around the bilinear sample. It
// softens enlarged heat cells without adding another render pass.
func sampleFlame(src []byte, sw, sh int, x, y float64) (r, g, b, a float64) {
	points := [...]struct{ dx, dy, weight float64 }{
		{0, 0, 0.46}, {-0.42, 0, 0.135}, {0.42, 0, 0.135},
		{0, -0.42, 0.135}, {0, 0.42, 0.135},
	}
	for _, p := range points {
		rr, gg, bb, aa := sampleBilinear(src, sw, sh, x+p.dx, y+p.dy)
		r += rr * aa * p.weight
		g += gg * aa * p.weight
		b += bb * aa * p.weight
		a += aa * p.weight
	}
	if a > 0 {
		r /= a
		g /= a
		b /= a
	}
	return r, g, b, a
}

// sampleBilinear interpolates premultiplied colour and alpha to avoid dark
// fringes where transparent pixels surround the flame.
func sampleBilinear(src []byte, sw, sh int, x, y float64) (r, g, b, a float64) {
	if x < 0 {
		x = 0
	} else if x > float64(sw-1) {
		x = float64(sw - 1)
	}
	if y < 0 {
		y = 0
	} else if y > float64(sh-1) {
		y = float64(sh - 1)
	}
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	x1, y1 := minInt(x0+1, sw-1), minInt(y0+1, sh-1)
	fx, fy := x-float64(x0), y-float64(y0)
	points := [...]struct {
		x, y   int
		weight float64
	}{
		{x0, y0, (1 - fx) * (1 - fy)}, {x1, y0, fx * (1 - fy)},
		{x0, y1, (1 - fx) * fy}, {x1, y1, fx * fy},
	}
	for _, p := range points {
		o := (p.y*sw + p.x) * 4
		pa := float64(src[o+3]) / 255
		a += pa * p.weight
		r += float64(src[o]) / 255 * pa * p.weight
		g += float64(src[o+1]) / 255 * pa * p.weight
		b += float64(src[o+2]) / 255 * pa * p.weight
	}
	if a > 0 {
		r /= a
		g /= a
		b /= a
	}
	return r, g, b, a
}

// blitSprite scales a sprite into the panel with nearest-neighbour sampling,
// compositing source-over in straight alpha.
func (r *CampfireRenderer) blitSprite(dx, dy, dw, dh int, src Sprite, alphaMul float64) {
	if dw <= 0 || dh <= 0 {
		return
	}
	sw, sh := src.W, src.H

	for y := 0; y < dh; y++ {
		ty := dy + y
		if ty < 0 || ty >= r.height {
			continue
		}
		syy := y * sh / dh
		if syy >= sh {
			syy = sh - 1
		}

		for x := 0; x < dw; x++ {
			tx := dx + x
			if tx < 0 || tx >= r.width {
				continue
			}
			sxx := x * sw / dw
			if sxx >= sw {
				sxx = sw - 1
			}

			c := src.Pix[syy*sw+sxx]
			if c.A == 0 {
				continue
			}
			a := float64(c.A) / 255 * alphaMul
			if a <= 0 {
				continue
			}
			r.blendPixel(tx, ty, float64(c.R)/255, float64(c.G)/255, float64(c.B)/255, a)
		}
	}
}

// blendPixel composites a straight-alpha source over the frame, source-over.
func (r *CampfireRenderer) blendPixel(x, y int, sr, sg, sb, sa float64) {
	if sa <= 0 {
		return
	}
	if sa > 1 {
		sa = 1
	}
	o := (y*r.width + x) * 4

	dr := float64(r.pix[o]) / 255
	dg := float64(r.pix[o+1]) / 255
	db := float64(r.pix[o+2]) / 255
	da := float64(r.pix[o+3]) / 255

	outA := sa + da*(1-sa)
	if outA <= 0 {
		r.pix[o], r.pix[o+1], r.pix[o+2], r.pix[o+3] = 0, 0, 0, 0
		return
	}
	r.pix[o] = toByte255((sr*sa + dr*da*(1-sa)) / outA)
	r.pix[o+1] = toByte255((sg*sa + dg*da*(1-sa)) / outA)
	r.pix[o+2] = toByte255((sb*sa + db*da*(1-sa)) / outA)
	r.pix[o+3] = toByte255(outA)
}

func toByte255(v float64) byte {
	i := math.Round(v * 255)
	if i < 0 {
		return 0
	}
	if i > 255 {
		return 255
	}
	return byte(i)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
