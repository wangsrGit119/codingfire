package fire

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// FirePreviewStyle is a frozen fire appearance used by the console preview and
// by --render.
type FirePreviewStyle int

const (
	PreviewOut FirePreviewStyle = iota
	PreviewEmber
	PreviewHush
	PreviewGlow
	PreviewCrackle
	PreviewRoar
	PreviewBlaze
)

// FirePreviewsAll is the preview order shown in the console.
var FirePreviewsAll = []FirePreviewStyle{
	PreviewOut, PreviewEmber, PreviewHush,
	PreviewGlow, PreviewCrackle, PreviewRoar,
	PreviewBlaze,
}

// Label returns the localised preview name.
func (s FirePreviewStyle) Label() string {
	switch s {
	case PreviewOut:
		return core.T("phase.out")
	case PreviewEmber:
		return core.T("phase.ember")
	case PreviewHush:
		return core.T("tier.hush")
	case PreviewGlow:
		return core.T("tier.glow")
	case PreviewCrackle:
		return core.T("tier.crackle")
	case PreviewRoar:
		return core.T("tier.roar")
	default:
		return core.T("tier.blaze")
	}
}

// Raw returns the untranslated, lowercase style name.
//
// This is the file-name stem --render uses, so it is part of the tool's output
// contract and must not be localised: the C# build named its files from the
// enum's ToString().ToLowerInvariant(), and changing the names would silently
// break anyone diffing preview directories between the two builds.
func (s FirePreviewStyle) Raw() string {
	switch s {
	case PreviewOut:
		return "out"
	case PreviewEmber:
		return "ember"
	case PreviewHush:
		return "hush"
	case PreviewGlow:
		return "glow"
	case PreviewCrackle:
		return "crackle"
	case PreviewRoar:
		return "roar"
	default:
		return "blaze"
	}
}

// Snapshot returns the frozen appearance for this style.
func (s FirePreviewStyle) Snapshot() core.FireSnapshot {
	accent := core.DefaultFlameAccent
	switch s {
	case PreviewOut:
		return core.FireSnapshot{Phase: core.PhaseOut, Tier: core.TierHush, FlameAccent: accent}
	case PreviewEmber:
		return core.FireSnapshot{EmberHeat: 0.85, Phase: core.PhaseEmber, SparkBurst: 0.15, Tier: core.TierHush, FlameAccent: accent}
	case PreviewHush:
		return core.FireSnapshot{Intensity: 0.10, Fuel: 0.18, EmberHeat: 0.35, Phase: core.PhaseFlame, SparkBurst: 0.1, Tier: core.TierHush, FlameAccent: accent}
	case PreviewGlow:
		return core.FireSnapshot{Intensity: 0.22, Fuel: 0.35, EmberHeat: 0.45, Phase: core.PhaseFlame, SparkBurst: 0.25, Tier: core.TierGlow, FlameAccent: accent}
	case PreviewCrackle:
		return core.FireSnapshot{Intensity: 0.45, Fuel: 0.55, EmberHeat: 0.55, Phase: core.PhaseFlame, SparkBurst: 0.45, Tier: core.TierCrackle, FlameAccent: accent}
	case PreviewRoar:
		return core.FireSnapshot{Intensity: 0.72, Fuel: 0.78, EmberHeat: 0.7, Phase: core.PhaseFlame, SparkBurst: 0.75, Tier: core.TierRoar, FlameAccent: accent}
	default:
		return core.FireSnapshot{Intensity: 1.0, Fuel: 1.0, EmberHeat: 0.9, Phase: core.PhaseFlame, SparkBurst: 1.0, Tier: core.TierBlaze, FlameAccent: accent}
	}
}

// inflow is one batch of tokens that arrived at a point in time.
type inflow struct {
	at     core.Time
	tokens float64
	source *core.UsageSource
}

// FireStateMachine tracks three independent quantities:
//
//	intensity  how hard it is burning right now — rises fast, falls fast
//	fuel       burnable reserve banked from recent usage; sets how long real
//	           flame can last
//	emberHeat  residual heat after the flame dies, decaying far slower
//
// The day's total only ever raises a slow "banked embers" floor. It never
// becomes headline UI — same as the macOS version.
type FireStateMachine struct {
	tuning  core.FireTuning
	inflows []inflow
	rng     *rand.Rand

	// now is a seam for tests and for --render, which drives time by hand.
	now func() core.Time

	lastTick      core.Time
	burnIntensity float64

	// slowIntensity follows only the 60s rate window. The visible fire
	// (burnIntensity) gets pushed up by the burst window, so reading tok/s
	// back off it would pin the display on every log thrown on the fire.
	// Reading it back off this instead gives "average burn rate".
	slowIntensity float64

	smoothedTokensPerSecond float64
	rateJitter              float64
	// rateNoisePhase is randomised at construction, matching the macOS
	// version — otherwise every launch replays the identical jitter waveform.
	rateNoisePhase float64
	// lastInflowAt is the last token event time. After ~3s of silence the fire
	// decays faster and RateFromFlame fades, so the displayed tok/s drops
	// promptly.
	lastInflowAt *core.Time

	LiveSnapshot      core.FireSnapshot
	TodayTokens       int
	TodayBySource     map[core.UsageSource]int
	TokensPerSecond   float64
	ColorPaletteEpoch int

	PreviewStyle  *FirePreviewStyle
	CustomPreview *core.FireSnapshot
}

// RateEventCreditTokens caps a single record for the live rate, so an absurd
// tok/s cannot appear.
const rateEventCreditTokens = 15000

// RateDisplayCapTps is the displayed ceiling.
const rateDisplayCapTps = 320

// NewFireStateMachine returns an idle machine.
func NewFireStateMachine() *FireStateMachine {
	m := &FireStateMachine{
		tuning:        core.DefaultFireTuning(),
		rng:           rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x5eed1ce)),
		now:           time.Now,
		LiveSnapshot:  core.Extinguished(),
		TodayBySource: map[core.UsageSource]int{},
	}
	m.lastTick = m.now()
	// The macOS version randomised this at the declaration site; field
	// initialisers could not reach _rng, so it moved into the constructor.
	m.rateNoisePhase = m.rng.Float64() * 2 * math.Pi
	return m
}

// SetClock overrides the time source. Pass nil to restore time.Now.
func (m *FireStateMachine) SetClock(fn func() core.Time) {
	if fn == nil {
		fn = time.Now
	}
	m.now = fn
	m.lastTick = fn()
}

// Tuning exposes the constants (read-only use).
func (m *FireStateMachine) Tuning() *core.FireTuning { return &m.tuning }

// IsPreviewing reports whether a frozen appearance is being shown.
func (m *FireStateMachine) IsPreviewing() bool {
	return m.PreviewStyle != nil || m.CustomPreview != nil
}

// Snapshot returns the appearance the renderer should draw.
func (m *FireStateMachine) Snapshot() core.FireSnapshot {
	if m.CustomPreview != nil {
		return *m.CustomPreview
	}
	if m.PreviewStyle != nil {
		return m.PreviewStyle.Snapshot()
	}
	return m.LiveSnapshot
}

// ApplySleepGap advances the simulation across a suspend/resume gap, capped at
// 6 hours so a laptop that slept overnight does not burn the whole reserve in
// one step.
func (m *FireStateMachine) ApplySleepGap(seconds float64) {
	if seconds <= 0 {
		return
	}
	m.Advance(math.Min(seconds, 6*60*60))
}

// ResetToUnlit clears all state.
func (m *FireStateMachine) ResetToUnlit() {
	m.inflows = m.inflows[:0]
	m.burnIntensity = 0
	m.slowIntensity = 0
	m.smoothedTokensPerSecond = 0
	m.rateJitter = 0
	m.TokensPerSecond = 0
	m.LiveSnapshot = core.Extinguished()
	m.lastInflowAt = nil
	m.lastTick = m.now()
}

// ShowPreview freezes the fire at a named style.
func (m *FireStateMachine) ShowPreview(style FirePreviewStyle) {
	m.CustomPreview = nil
	m.PreviewStyle = &style
}

// ShowCustomPreview freezes the fire at an arbitrary intensity.
func (m *FireStateMachine) ShowCustomPreview(intensity float64) {
	i := math.Min(1, math.Max(0, intensity))
	m.PreviewStyle = nil

	var tier core.FireTier
	switch {
	case i < 0.18:
		tier = core.TierHush
	case i < 0.35:
		tier = core.TierGlow
	case i < 0.58:
		tier = core.TierCrackle
	case i < 0.82:
		tier = core.TierRoar
	default:
		tier = core.TierBlaze
	}

	var phase core.FirePhase
	switch {
	case i < 0.02:
		phase = core.PhaseOut
	case i < 0.08:
		phase = core.PhaseEmber
	default:
		phase = core.PhaseFlame
	}

	snap := core.FireSnapshot{
		Intensity:   i,
		Fuel:        i,
		Phase:       phase,
		SparkBurst:  i,
		Tier:        tier,
		FlameAccent: m.LiveSnapshot.FlameAccent,
	}
	if phase == core.PhaseEmber || phase == core.PhaseOut {
		snap.Intensity = 0
	}
	if phase == core.PhaseEmber {
		snap.EmberHeat = 0.85
	} else {
		snap.EmberHeat = math.Max(0.2, i*0.9)
	}
	m.CustomPreview = &snap
}

// ReturnToLive drops any preview.
func (m *FireStateMachine) ReturnToLive() {
	m.PreviewStyle = nil
	m.CustomPreview = nil
}

// UpdateTodayTokens feeds the day's running totals in.
func (m *FireStateMachine) UpdateTodayTokens(tokens int, bySource map[core.UsageSource]int) {
	m.TodayTokens = tokens
	if m.TodayTokens < 0 {
		m.TodayTokens = 0
	}
	if bySource == nil {
		bySource = map[core.UsageSource]int{}
	}
	m.TodayBySource = bySource
	m.LiveSnapshot = m.Compose(m.LiveSnapshot)
}

// NotifyColorsChanged bumps the palette epoch so the renderer rebuilds its
// ramp, then recomposes.
func (m *FireStateMachine) NotifyColorsChanged() {
	m.ColorPaletteEpoch++
	m.LiveSnapshot = m.Compose(m.LiveSnapshot)
}

// BumpPaletteEpoch signals a ramp rebuild without touching the snapshot.
func (m *FireStateMachine) BumpPaletteEpoch() { m.ColorPaletteEpoch++ }

// Ingest records a batch of tokens. animate=false suppresses the spark burst
// (used when replaying history at startup).
func (m *FireStateMachine) Ingest(tokens float64, source *core.UsageSource, at core.Time, animate bool) {
	if tokens <= 0 {
		return
	}

	m.inflows = append(m.inflows, inflow{at: at, tokens: tokens, source: source})
	m.PruneInflows(at)
	t := at
	m.lastInflowAt = &t
	m.UpdateTokensPerSecond(at, 0.35)

	compressed := core.SoftCompress(tokens)
	fuelGain := math.Min(0.55, compressed/m.tuning.FuelTokenScale)

	next := m.LiveSnapshot
	next.Fuel = math.Min(1.0, next.Fuel+fuelGain)
	next.EmberHeat = math.Min(1.0, math.Max(
		next.EmberHeat,
		next.Fuel*m.tuning.EmberFromFuelGain+m.InstantaneousRatePush(tokens)*0.25))

	if animate && m.PreviewStyle == nil {
		burst := math.Min(1.0, m.InstantaneousRatePush(tokens))
		next.SparkBurst = math.Min(m.tuning.MaxSparkBurst, next.SparkBurst+0.2+burst*0.9)
	}

	// Slow lane: the steady burn level that anchors the tok/s readout.
	m.slowIntensity = m.approach(m.slowIntensity, m.slowTargetIntensity(at), m.tuning.IntensityRiseSeconds, 0.35)

	// Push the visible fire toward "how hard right now"; the actual fall is
	// left to Advance. A target that jumps high (a big armful of wood) uses
	// the faster rise constant so the flame leaps immediately.
	target := m.targetIntensity(at)
	m.burnIntensity = math.Max(m.burnIntensity,
		m.approach(m.burnIntensity, target, m.tuning.BurstRiseSeconds, 0.35))
	next.Intensity = m.burnIntensity
	next.Phase = core.PhaseFlame
	m.LiveSnapshot = m.Compose(next)
}

// Tick advances the simulation, driven by the UI's 20 Hz timer.
func (m *FireStateMachine) Tick() {
	now := m.now()
	dt := now.Sub(m.lastTick).Seconds()
	m.lastTick = now
	if dt <= 0 {
		return
	}
	m.Advance(math.Min(dt, 1.0))
}

func (m *FireStateMachine) Advance(dt float64) {
	now := m.now()
	m.PruneInflows(now)
	m.UpdateTokensPerSecond(now, dt)

	slowTarget := m.slowTargetIntensity(now)
	target := m.targetIntensity(now)
	fuel := m.LiveSnapshot.Fuel
	ember := m.LiveSnapshot.EmberHeat
	spark := m.LiveSnapshot.SparkBurst

	// No new inflow for a while? Make the fire actually die down. The default
	// 14s fall tau is too slow — it looks "stuck on".
	fallTau := m.tuning.IntensityFallSeconds
	if m.lastInflowAt != nil {
		silence := now.Sub(*m.lastInflowAt).Seconds()
		if silence > m.tuning.IdleFallStartSeconds {
			t := math.Min(1.0, (silence-m.tuning.IdleFallStartSeconds)/
				math.Max(0.1, m.tuning.IdleFallRampSeconds))
			fallTau = m.tuning.IntensityFallSeconds +
				(m.tuning.IntensityFallSecondsIdle-m.tuning.IntensityFallSeconds)*t
		}
	} else if m.burnIntensity > 0.02 {
		// First launch with only historical data: also fall fast.
		fallTau = m.tuning.IntensityFallSecondsIdle
	}

	// The slow lane follows the 60s window using the original rise constant.
	// It only serves the tok/s readout.
	m.slowIntensity = m.approach(m.slowIntensity, slowTarget, m.tuning.IntensityRiseSeconds, dt)

	// Visible fire: the target includes the 20s burst term. A target that
	// jumps high uses the faster rise constant so the flame leaps; the fall
	// still uses the slow fallTau above, giving "leaps fast, collapses slowly"
	// — which is the shape of a real campfire.
	riseTau := m.tuning.IntensityRiseSeconds
	if target-m.burnIntensity > m.tuning.BurstJumpThreshold {
		riseTau = m.tuning.BurstRiseSeconds
	}
	tau := fallTau
	if target > m.burnIntensity {
		tau = riseTau
	}
	alpha := 1.0 - math.Exp(-dt/math.Max(0.05, tau))
	m.burnIntensity += (target - m.burnIntensity) * alpha
	m.burnIntensity = math.Min(1.0, math.Max(0, m.burnIntensity))

	// Fuel is only the reserve behind the embers; it never pushes the fire up.
	if fuel > 0 {
		burnRate := m.tuning.FuelBurnPerSecondAtIdle +
			(m.tuning.FuelBurnPerSecondAtFull-m.tuning.FuelBurnPerSecondAtIdle)*
				math.Pow(math.Max(m.burnIntensity, target), 1.1)
		fuel = math.Max(0, fuel-burnRate*dt)
		ember = math.Max(ember, fuel*0.55+m.burnIntensity*0.25)
	} else {
		ember = math.Max(0, ember-m.tuning.EmberDecayPerSecond*dt)
	}

	spark = math.Max(0, spark-m.tuning.SparkDecayPerSecond*dt)

	m.LiveSnapshot = m.Compose(core.FireSnapshot{
		Intensity:   m.burnIntensity,
		Fuel:        fuel,
		EmberHeat:   ember,
		Phase:       core.PhaseFlame,
		SparkBurst:  spark,
		Tier:        core.TierHush,
		FlameAccent: m.LiveSnapshot.FlameAccent,
	})
}

// ---------------------------------------------------------------------------
// Rate estimation
// ---------------------------------------------------------------------------

// RateFromInflows is the measured rate inside the rate window (tokens/sec).
// Each record is capped at rateEventCreditTokens and only records landing
// inside the window count.
func (m *FireStateMachine) RateFromInflows(now core.Time) float64 {
	window := math.Max(1.0, m.tuning.RateWindowSeconds)
	cutoff := now.Add(-time.Duration(window * float64(time.Second)))
	var credited float64
	for _, in := range m.inflows {
		// A late event (an old timestamp flushed when a tool exits) should
		// still make the fire leap, but it must not fake a "burning at this
		// rate right now" reading — hence the window filter.
		if in.at.Before(cutoff) {
			continue
		}
		credited += math.Min(in.tokens, rateEventCreditTokens)
	}
	return credited / window
}

// RateFromFlame converts the current fire back into tokens/sec so the shown
// rate agrees with the visible fire.
//
// It uses the slow lane, not the visible fire: the visible fire carries the
// burst term, and reading it back would pin the display on every log thrown on
// (a single 45k record reads back as 3000 tok/s), making the number useless.
func (m *FireStateMachine) RateFromFlame() float64 {
	return m.tokensPerSecondMatchingFlame(m.slowIntensity)
}

// ComputeInstantRate is the candidate rate before smoothing and jitter:
// min(display cap, max(measured, flame-derived)).
//
// Split out so it can be asserted directly — once jitter is layered on, the
// displayed value is not reproducible.
func (m *FireStateMachine) ComputeInstantRate(now core.Time) float64 {
	inflowRate := m.RateFromInflows(now)
	flameRate := m.RateFromFlame()

	// When the inflow stream goes stale, RateFromFlame keeps the number high
	// even though no tokens are arriving. Fade it out exponentially.
	if m.lastInflowAt != nil {
		silence := now.Sub(*m.lastInflowAt).Seconds()
		if silence > m.tuning.IdleFallStartSeconds {
			fade := math.Exp(-(silence - m.tuning.IdleFallStartSeconds) /
				math.Max(0.1, m.tuning.IdleFlameRateTau))
			flameRate *= fade
		}
	} else {
		flameRate = 0
	}

	return math.Min(rateDisplayCapTps, math.Max(inflowRate, flameRate))
}

func (m *FireStateMachine) UpdateTokensPerSecond(now core.Time, dt float64) {
	instant := m.ComputeInstantRate(now)

	tau := 2.8
	if instant > m.smoothedTokensPerSecond {
		tau = 0.9
	}
	alpha := 1.0 - math.Exp(-dt/math.Max(0.05, tau))
	m.smoothedTokensPerSecond += (instant - m.smoothedTokensPerSecond) * alpha

	if m.burnIntensity < 0.04 {
		m.smoothedTokensPerSecond = 0
		m.rateJitter = 0
		m.TokensPerSecond = 0
		return
	}

	// Multi-frequency low-amplitude jitter (±8–14%) keeps the hover number
	// feeling alive.
	m.rateNoisePhase += dt
	baseRate := math.Max(0.5, m.smoothedTokensPerSecond)
	amp := math.Max(0.6, baseRate*(0.08+0.04*m.burnIntensity))
	wander := math.Sin(m.rateNoisePhase*1.65)*amp*0.50 +
		math.Sin(m.rateNoisePhase*0.41+1.3)*amp*0.32 +
		math.Sin(m.rateNoisePhase*3.1+0.4)*amp*0.12 +
		(m.rng.Float64()*2-1)*amp*0.18

	jitterAlpha := 1.0 - math.Exp(-dt/0.35)
	m.rateJitter += (wander - m.rateJitter) * jitterAlpha

	displayed := math.Min(rateDisplayCapTps, math.Max(0.2, m.smoothedTokensPerSecond+m.rateJitter))
	rounded := math.Round(displayed*10) / 10
	if math.Abs(rounded-m.TokensPerSecond) >= 0.05 || (rounded == 0) != (m.TokensPerSecond == 0) {
		m.TokensPerSecond = rounded
	}
}

// tokensPerSecondMatchingFlame inverts the flame's TPM anchors so the shown
// rate tracks the visible fire.
func (m *FireStateMachine) tokensPerSecondMatchingFlame(intensity float64) float64 {
	if intensity <= 0.04 {
		return 0
	}
	return m.tpmForIntensity(intensity) / 60.0
}

func (m *FireStateMachine) tpmForIntensity(intensity float64) float64 {
	tpm := m.tuning.TpmAnchors
	val := m.tuning.TpmIntensity
	if intensity <= val[0] {
		return tpm[0]
	}
	if intensity >= val[len(val)-1] {
		return tpm[len(tpm)-1]
	}
	for i := 0; i < len(val)-1; i++ {
		if intensity <= val[i+1] {
			span := math.Max(0.0001, val[i+1]-val[i])
			t := (intensity - val[i]) / span
			return tpm[i] + (tpm[i+1]-tpm[i])*t
		}
	}
	return tpm[len(tpm)-1]
}

// targetIntensity is the fire level under the intensity window: how hard the
// flame should be burning right now. Shorter than the rate window, so the fire
// leaps when activity starts and drops when it stops.
func (m *FireStateMachine) targetIntensity(now core.Time) float64 {
	return m.intensityFromCredited(
		m.creditedTokensIn(now, m.tuning.IntensityWindowSeconds),
		m.tuning.IntensityWindowSeconds)
}

// slowTargetIntensity is the fire level under the rate window: the average
// burn rate. Only the tok/s readout uses it.
func (m *FireStateMachine) slowTargetIntensity(now core.Time) float64 {
	return m.intensityFromCredited(
		m.creditedTokensIn(now, m.tuning.RateWindowSeconds),
		m.tuning.RateWindowSeconds)
}

// creditedTokensIn sums the credited score of every inflow in the window,
// capping each at EventCreditTokens.
func (m *FireStateMachine) creditedTokensIn(now core.Time, window float64) float64 {
	w := math.Max(1.0, window)
	cutoff := now.Add(-time.Duration(w * float64(time.Second)))
	var credited float64
	for _, in := range m.inflows {
		if in.at.Before(cutoff) {
			continue
		}
		credited += math.Min(in.tokens, m.tuning.EventCreditTokens)
	}
	return credited
}

func (m *FireStateMachine) intensityFromCredited(credited, window float64) float64 {
	tokensPerMinute := credited * (60.0 / math.Max(1.0, window))
	return m.intensityFromTpm(tokensPerMinute)
}

// approach moves current toward target by (1 - e^(-dt/tau)) over dt seconds.
func (m *FireStateMachine) approach(current, target, tau, dt float64) float64 {
	alpha := 1.0 - math.Exp(-dt/math.Max(0.05, tau))
	v := current + (target-current)*alpha
	return math.Min(1.0, math.Max(0, v))
}

// intensityFromTpm maps tokens/minute to intensity. The curve is deliberately
// uneven: medium is easy, large takes more, roaring is hard.
func (m *FireStateMachine) intensityFromTpm(tpm float64) float64 {
	anchors := m.tuning.TpmAnchors
	val := m.tuning.TpmIntensity
	if tpm <= anchors[0] {
		return val[0]
	}
	if tpm >= anchors[len(anchors)-1] {
		return val[len(val)-1]
	}
	for i := 0; i < len(anchors)-1; i++ {
		if tpm <= anchors[i+1] {
			span := math.Max(1.0, anchors[i+1]-anchors[i])
			t := (tpm - anchors[i]) / span
			s := t * t * (3 - 2*t) // smoothstep, so tier changes do not step
			return val[i] + (val[i+1]-val[i])*s
		}
	}
	return val[len(val)-1]
}

func (m *FireStateMachine) credit(tokens float64) float64 {
	return math.Min(tokens, m.tuning.EventCreditTokens)
}

func (m *FireStateMachine) InstantaneousRatePush(tokens float64) float64 {
	return m.intensityFromTpm(m.credit(tokens) * (60.0 / math.Max(1.0, m.tuning.IntensityWindowSeconds)))
}

// dailyBaseIntensity is the silent banked-ember floor: it approaches "barely
// lit" asymptotically and never announces itself as a mode.
func (m *FireStateMachine) dailyBaseIntensity() float64 {
	t := float64(m.TodayTokens)
	if t < m.tuning.DailyBaseStartTokens {
		return 0
	}
	x := (t - m.tuning.DailyBaseStartTokens) / m.tuning.DailyBaseHalfTokens
	shaped := 1.0 - math.Exp(-x)
	return m.tuning.DailyBaseMaxIntensity * shaped
}

// Compose folds the derived quantities (tier, phase, banked embers) into a
// snapshot.
func (m *FireStateMachine) Compose(snap core.FireSnapshot) core.FireSnapshot {
	next := snap
	baseIntensity := m.dailyBaseIntensity()
	// Shown value = max(live fire, banked embers); the floor never feeds back
	// into burnIntensity.
	shown := math.Max(m.burnIntensity, baseIntensity)

	next.Intensity = shown
	next.Tier = core.TierFromIntensity(shown)

	switch {
	case shown >= 0.035:
		next.Phase = core.PhaseFlame
		next.EmberHeat = math.Max(next.EmberHeat, shown*0.4)
	case next.EmberHeat > 0.04 || next.Fuel > 0.03:
		next.Phase = core.PhaseEmber
		next.Intensity = 0
		next.Tier = core.TierHush
	case next.Phase == core.PhaseUnlit:
		next.Phase = core.PhaseUnlit
		next.Intensity = 0
	default:
		next.Phase = core.PhaseOut
		next.Intensity = 0
		next.Fuel = 0
		next.EmberHeat = 0
		next.SparkBurst = 0
	}
	return next
}

// retentionSeconds is the longest of the two windows. Inflows must be kept
// beyond the *longest* one: the intensity window is shorter than the rate
// window, so pruning on it would drop records the rate readout still needs.
func (m *FireStateMachine) retentionSeconds() float64 {
	return math.Max(m.tuning.IntensityWindowSeconds, m.tuning.RateWindowSeconds)
}

// PruneInflows drops inflows older than the retention window.
func (m *FireStateMachine) PruneInflows(now core.Time) {
	cutoff := now.Add(-time.Duration(m.retentionSeconds() * float64(time.Second)))
	kept := m.inflows[:0]
	for _, in := range m.inflows {
		if !in.at.Before(cutoff) {
			kept = append(kept, in)
		}
	}
	m.inflows = kept
	if len(m.inflows) == 0 {
		m.lastInflowAt = nil
	}
}
