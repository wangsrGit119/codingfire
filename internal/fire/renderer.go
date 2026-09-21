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

	// Simulation stepping: 6fps under reduce-motion, else 12fps.
	stepFPS := 12.0
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

	// ---- outer glow (soft radial warm additive, under the flame) ----
	if r.flameAlpha > 0.01 {
		r.renderFireGlow(snap, px, timeSeconds, float64(originX), float64(originY), flameBaseY, flameH)
	}

	// ---- flame ----
	if r.flameAlpha > 0.01 {
		flameX := int(math.Round(float64(originX) - flameW/2 + jitter))
		flameY := int(math.Round(float64(originY) - (flameBaseY - px) - flameH))
		r.blitEngine(flameX, flameY, int(math.Round(flameW)), int(math.Round(flameH)), r.flameAlpha*flameFlicker)

		r.FlameBaseY = int(math.Round(float64(originY) - (flameBaseY - px)))
		r.FlameTipY = flameY
	}

	// ---- ember breathing glow on top of the logs ----
	r.renderEmberGlow(logX, logY, int(math.Round(logW)), int(math.Round(logH)), snap, timeSeconds)

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

// renderFireGlow draws the outer radial glow around the fire, additively,
// before the flame itself.
func (r *CampfireRenderer) renderFireGlow(snap core.FireSnapshot, px, timeSeconds, originX, originY, flameBaseY, flameH float64) {
	// Glow centre: at the base of the flame, slightly above the logs.
	cx := originX
	cy := originY - flameBaseY

	baseRadius := flameH*0.55 + 4*px
	intensityBoost := 1.0 + snap.Intensity*0.2
	pulse := 0.9 + 0.1*math.Sin(timeSeconds*1.3)
	maxR := baseRadius * intensityBoost * pulse

	ar, ag, ab := accentComponents(snap)

	// Sharp Gaussian falloff: concentrated near the centre.
	maxRadiusI := int(math.Ceil(maxR))
	maxR2 := maxR * maxR
	for dy := -maxRadiusI; dy <= maxRadiusI; dy++ {
		for dx := -maxRadiusI; dx <= maxRadiusI; dx++ {
			dist2 := float64(dx*dx + dy*dy)
			if dist2 > maxR2 {
				continue
			}
			falloff := math.Exp(-dist2 / maxR2 * 5.0)

			// Keep the same compact Gaussian shape as the C# renderer, but
			// lift the alpha slightly for the straight-alpha NRGBA surface.
			// The old 0.06/0.06 values were tuned for C#'s premultiplied
			// bitmap and became almost invisible after go-gui composited the
			// transparent GL surface over the desktop.
			glowAlpha := falloff * (0.09 + snap.Intensity*0.09) * r.flameAlpha

			tx := int(math.Round(cx + float64(dx)))
			ty := int(math.Round(cy + float64(dy)))
			if tx < 0 || tx >= r.width || ty < 0 || ty >= r.height {
				continue
			}
			r.addPixel(tx, ty, ar, ag, ab, glowAlpha)
		}
	}
}

// renderEmberGlow draws a pulsing glow over the log pile, additively.
func (r *CampfireRenderer) renderEmberGlow(logX, logY, logW, logH int, snap core.FireSnapshot, timeSeconds float64) {
	var heat float64
	switch snap.Phase {
	case core.PhaseFlame:
		heat = math.Max(snap.Intensity, snap.SparkBurst*0.5)
	case core.PhaseEmber:
		heat = snap.EmberHeat
	}
	if heat < 0.05 {
		return
	}

	pulse := 0.8 + 0.2*math.Sin(timeSeconds*2.1)
	glowStrength := heat * pulse * 0.15

	cx := float64(logX) + float64(logW)*0.5
	cy := float64(logY) + float64(logH)*0.8
	radius := float64(logW) * 0.35

	ar, ag, ab := accentComponents(snap)

	rI := int(math.Ceil(radius))
	r2 := radius * radius
	for dy := -rI; dy <= rI; dy++ {
		for dx := -rI; dx <= rI; dx++ {
			d2 := float64(dx*dx + dy*dy)
			if d2 > r2 {
				continue
			}
			falloff := math.Exp(-d2 / r2 * 1.8)
			a := falloff * glowStrength
			if a < 0.02 {
				continue
			}
			tx := int(math.Round(cx + float64(dx)))
			ty := int(math.Round(cy + float64(dy)))
			if tx < 0 || tx >= r.width || ty < 0 || ty >= r.height {
				continue
			}
			r.addPixel(tx, ty, ar, ag, ab, a)
		}
	}
}

func accentComponents(snap core.FireSnapshot) (float64, float64, float64) {
	if snap.FlameAccent == (core.AccentRGB{}) {
		return 0.95, 0.55, 0.20
	}
	return snap.FlameAccent[0], snap.FlameAccent[1], snap.FlameAccent[2]
}

// blitEngine scales the heat-field buffer into the panel with nearest-neighbour
// sampling.
func (r *CampfireRenderer) blitEngine(dx, dy, dw, dh int, alphaMul float64) {
	src := r.engine.Pix()
	sw, sh := r.engine.Width, r.engine.Height
	if dw <= 0 || dh <= 0 {
		return
	}
	sx := float64(sw) / float64(dw)
	sy := float64(sh) / float64(dh)

	for y := 0; y < dh; y++ {
		ty := dy + y
		if ty < 0 || ty >= r.height {
			continue
		}
		syy := int(float64(y) * sy)
		if syy >= sh {
			syy = sh - 1
		}
		srcRow := syy * sw * 4

		for x := 0; x < dw; x++ {
			tx := dx + x
			if tx < 0 || tx >= r.width {
				continue
			}
			sxx := int(float64(x) * sx)
			if sxx >= sw {
				sxx = sw - 1
			}

			so := srcRow + sxx*4
			a := float64(src[so+3]) / 255
			if a == 0 {
				continue
			}
			if alphaMul < 0.999 {
				a *= alphaMul
			}
			r.blendPixel(tx, ty, float64(src[so])/255, float64(src[so+1])/255, float64(src[so+2])/255, a)
		}
	}
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

// addPixel adds a colour into the frame, matching the C# renderer's additive
// blend on its premultiplied BGRA bitmap.
//
// The Go frame is straight-alpha NRGBA, though. The old port copied the C#
// channel addition directly into straight RGB, leaving glow pixels with dark
// RGB values such as (38,22,8,42). When go-gui (or --render) composited that
// pixel, the alpha was applied a second time and the ambient halo nearly
// disappeared. Convert the destination to premultiplied form, add the glow,
// then convert back to straight alpha exactly once.
func (r *CampfireRenderer) addPixel(x, y int, cr, cg, cb, a float64) {
	if a <= 0 {
		return
	}
	if a > 1 {
		a = 1
	}
	o := (y*r.width + x) * 4

	da := float64(r.pix[o+3]) / 255
	sa := a
	outA := da + sa
	if outA > 1 {
		outA = 1
	}
	if outA <= 0 {
		return
	}

	// Existing straight-alpha destination -> premultiplied channels, then
	// additive source contribution. Clamp premultiplied values before the
	// unpremultiply so an intense glow cannot wrap or produce invalid RGB.
	pr := math.Min(1, float64(r.pix[o])/255*da+cr*sa)
	pg := math.Min(1, float64(r.pix[o+1])/255*da+cg*sa)
	pb := math.Min(1, float64(r.pix[o+2])/255*da+cb*sa)
	r.pix[o] = toByte255(pr / outA)
	r.pix[o+1] = toByte255(pg / outA)
	r.pix[o+2] = toByte255(pb / outA)
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
