package core

// ---------------------------------------------------------------------------
// Data sources
// ---------------------------------------------------------------------------

// UsageSource identifies a local tool whose usage logs we read.
//
// The integer value IS the flame colour-band order, so new sources must be
// appended, never inserted — reordering silently recolours every existing
// source for users who upgrade.
//
// NOTE: there are 25 values below but only 23 are actually collected. Cursor
// and Kiro exist purely to keep the colour-band indices stable and have no
// adapter. Do not derive a source count from len(UsageSourcesAll); count the
// `sources` block of a real --dump instead (23 lines), then subtract one for
// WorkBuddyIntl, which is the same tool installed twice.
type UsageSource int

const (
	// ---- the original six from the macOS version ----
	ClaudeCode UsageSource = iota
	Codex
	Cursor
	Grok
	Pi
	Amp

	// ---- added for multi-tool aggregation ----
	WorkBuddy
	CodeBuddy
	Qoder
	QwenCode
	Kimi
	Copilot
	ZCode
	OpenCode
	GeminiCli
	Kiro
	Droid
	Cline
	RooCode
	KiloCode
	DeepSeekHarness
	CommandCode
	OpenClaw
	EveryCode

	// ---- regional variants of the same tool ----
	// WorkBuddy (CN) and WorkBuddy (INTL) are two independent installs with
	// different log roots (~/.workbuddy vs ~/.workbuddy-ai) and identical
	// formats. Counting them apart is what shows which side is burning
	// tokens. Must stay last: index == colour-band ordinal.
	WorkBuddyIntl
)

// UsageSourcesAll is the iteration order for reports and menus. The original
// six stay first so a user who only ever ran those sources sees byte-identical
// flame colouring after an upgrade.
var UsageSourcesAll = []UsageSource{
	ClaudeCode,
	Codex,
	Cursor,
	Grok,
	Pi,
	Amp,
	WorkBuddy,
	CodeBuddy,
	Qoder,
	QwenCode,
	Kimi,
	Copilot,
	ZCode,
	OpenCode,
	GeminiCli,
	Kiro,
	Droid,
	Cline,
	RooCode,
	KiloCode,
	DeepSeekHarness,
	CommandCode,
	OpenClaw,
	EveryCode,
	WorkBuddyIntl,
}

// rawNames are the persisted / colour-key identifiers. Index-aligned with
// UsageSourcesAll.
var rawNames = [...]string{
	"claude_code", "codex", "cursor", "grok", "pi", "amp",
	"workbuddy", "codebuddy", "qoder", "qwen", "kimi", "copilot",
	"zcode", "opencode", "gemini", "kiro", "droid", "cline",
	"roocode", "kilocode", "dsh", "command-code", "openclaw", "every-code",
	"workbuddy-intl",
}

// displayNames are stable, unlocalised English product names. A few sources
// override these via the L10n key "source.name.<raw>" when the regional
// variant needs disambiguating.
var displayNames = [...]string{
	"Claude Code", "Codex", "Cursor", "Grok", "Pi", "Amp",
	"WorkBuddy", "CodeBuddy", "Qoder", "Qwen Code", "Kimi", "GitHub Copilot",
	"ZCode", "OpenCode", "Gemini CLI", "Kiro", "Droid", "Cline",
	"Roo Code", "Kilo Code", "DeepSeek Harness", "Command Code", "OpenClaw", "Every Code",
	"WorkBuddy INTL",
}

func init() {
	// The three tables must stay the same length — a mismatch means someone
	// added a source without updating its companions.
	//
	// The lengths are compared one at a time rather than as one `a != n || b !=
	// n` expression, which go vet flags as a suspect `or` (it cannot tell this
	// apart from a copy-paste slip).
	n := len(UsageSourcesAll)
	if len(rawNames) != n {
		panic("core: rawNames is out of sync with UsageSource")
	}
	if len(displayNames) != n {
		panic("core: displayNames is out of sync with UsageSource")
	}
}

func sourceIndex(s UsageSource) int {
	if i := int(s); i >= 0 && i < len(rawNames) {
		return i
	}
	return 0
}

// Raw returns the persisted identifier, which doubles as the colour key.
func (s UsageSource) Raw() string { return rawNames[sourceIndex(s)] }

// UsageSourceFromRaw reverses Raw. The second result reports a hit.
func UsageSourceFromRaw(raw string) (UsageSource, bool) {
	for i, n := range rawNames {
		if n == raw {
			return UsageSourcesAll[i], true
		}
	}
	return 0, false
}

// DisplayName returns the localised label, falling back to the static English
// product name when no override key exists.
func (s UsageSource) DisplayName() string {
	key := "source.name." + rawNames[sourceIndex(s)]
	if L10nHas(key) {
		return T(key)
	}
	return displayNames[sourceIndex(s)]
}

// SourceConnectionState is the health of one source's log discovery.
type SourceConnectionState int

const (
	SourceOk SourceConnectionState = iota
	SourceNotFound
	SourceNoPermission
	SourceUnsupported
	SourceReadError
)

// LabelKey returns the L10n key naming this state.
func (s SourceConnectionState) LabelKey() string {
	switch s {
	case SourceOk:
		return "source.state.ok"
	case SourceNotFound:
		return "source.state.notFound"
	case SourceNoPermission:
		return "source.state.noPermission"
	case SourceUnsupported:
		return "source.state.unsupported"
	default:
		return "source.state.readError"
	}
}

// Label returns the localised state name.
func (s SourceConnectionState) Label() string { return T(s.LabelKey()) }

// RawName returns the untranslated state name.
//
// The --dump report is hardcoded English and prints this rather than Label(),
// matching the C# build, which printed the enum's ToString(). Keeping it
// untranslated is the point: the report gets pasted into issues, and a state
// column that changes language with the reporter's OS is harder to read, not
// easier.
func (s SourceConnectionState) RawName() string {
	switch s {
	case SourceOk:
		return "Ok"
	case SourceNotFound:
		return "NotFound"
	case SourceNoPermission:
		return "NoPermission"
	case SourceUnsupported:
		return "Unsupported"
	default:
		return "ReadError"
	}
}

// ---------------------------------------------------------------------------
// Usage records
// ---------------------------------------------------------------------------

// UsageBreakdown splits a usage record by token class. Nil means "not
// reported by this source", which is distinct from zero.
type UsageBreakdown struct {
	Input      *int
	Output     *int
	CacheRead  *int
	CacheWrite *int
}

// BillableTotal sums whichever classes were reported.
func (b UsageBreakdown) BillableTotal() int {
	return deref(b.Input) + deref(b.Output) + deref(b.CacheRead) + deref(b.CacheWrite)
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// UsageEvent is one token-consuming record read from a local log.
type UsageEvent struct {
	ID        string
	Source    UsageSource
	Timestamp Time // local time
	Tokens    int
	Breakdown UsageBreakdown
	// Model is the model the tokens were billed to, e.g. "gpt-6-astra". It is
	// optional and often empty: only some tools write a model next to the
	// token counters, and for the rest the field stays "" rather than being
	// guessed from the tool's name. Empty means "not stated", not "unknown
	// model" — the console reports the two the same way, because inventing a
	// value would be worse than admitting there is none.
	//
	// It is deliberately not part of ID: ids are already on disk, and folding
	// the model into them would make every existing row look like a new event
	// and double-count the history.
	Model       string
	FilePath    string
	IsEstimated bool
}

// Clone returns a deep copy. UsageBreakdown holds pointers, so a shallow copy
// would alias them.
func (e UsageEvent) Clone() UsageEvent {
	c := e
	c.Breakdown = UsageBreakdown{
		Input:      cloneInt(e.Breakdown.Input),
		Output:     cloneInt(e.Breakdown.Output),
		CacheRead:  cloneInt(e.Breakdown.CacheRead),
		CacheWrite: cloneInt(e.Breakdown.CacheWrite),
	}
	return c
}

func cloneInt(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// IntPtr is a convenience for building breakdowns and fixtures.
func IntPtr(v int) *int { return &v }

// SourceStatus is one row of the console's source list.
type SourceStatus struct {
	Source      UsageSource
	State       SourceConnectionState
	Detail      string
	LastReadAt  Time
	HasLastRead bool
	TodayTokens int
}

// HourlyUsage is one bar of the today timeline.
type HourlyUsage struct {
	Hour   int
	Tokens int
}

// ---------------------------------------------------------------------------
// Fire
// ---------------------------------------------------------------------------

// FirePhase is the coarse visual state of the campfire.
type FirePhase int

const (
	PhaseUnlit FirePhase = iota
	PhaseFlame
	PhaseEmber
	PhaseOut
)

// FireTier is the headline "how big is the fire" label.
type FireTier int

const (
	TierHush FireTier = iota
	TierGlow
	TierCrackle
	TierRoar
	TierBlaze
)

// LabelKey returns the L10n key naming this tier.
func (t FireTier) LabelKey() string {
	switch t {
	case TierHush:
		return "tier.hush"
	case TierGlow:
		return "tier.glow"
	case TierCrackle:
		return "tier.crackle"
	case TierRoar:
		return "tier.roar"
	default:
		return "tier.blaze"
	}
}

// Label returns the localised tier name.
func (t FireTier) Label() string { return T(t.LabelKey()) }

// TierFromIntensity maps live intensity to a tier. Fuel deliberately does not
// participate — matches the macOS version.
func TierFromIntensity(intensity float64) FireTier {
	switch {
	case intensity < 0.16:
		return TierHush
	case intensity < 0.34:
		return TierGlow
	case intensity < 0.56:
		return TierCrackle
	case intensity < 0.78:
		return TierRoar
	default:
		return TierBlaze
	}
}

// AccentRGB is a normalised RGB triple (each component 0…1).
type AccentRGB [3]float64

// DefaultFlameAccent is the natural campfire base colour: warm orange at the
// base, with the palette ramp carrying it through red and yellow to white-hot.
var DefaultFlameAccent = AccentRGB{1.0, 0.45, 0.12}

// FireSnapshot is an immutable-per-frame view of the fire for the renderer.
//
// AccentRGB is a value type on purpose: the C# version used a double[] and
// needed an explicit Clone() to avoid aliasing the accent across snapshots.
type FireSnapshot struct {
	Intensity  float64
	Fuel       float64
	EmberHeat  float64
	Phase      FirePhase
	SparkBurst float64
	Tier       FireTier
	// FlameAccent is the user's chosen flame colour. The flame, the glow and
	// the sparks all follow it. The fire is single-colour: there is no
	// per-source tinting (removed by user request).
	FlameAccent AccentRGB
}

// Extinguished returns the fully-out snapshot.
func Extinguished() FireSnapshot {
	return FireSnapshot{
		Phase:       PhaseUnlit,
		Tier:        TierHush,
		FlameAccent: DefaultFlameAccent,
	}
}

// FireTuning holds the simulation constants. Values match the macOS version
// unless noted.
type FireTuning struct {
	// IntensityWindowSeconds is the observation window for "how hard is it
	// burning right now".
	//
	// Deliberately shorter than the macOS version's 60s. The window only
	// affects transients, not steady state: a steady rate R yields the same
	// TPM under any window W (credited = R·W, normalised by 60/W), so the
	// anchor table is unaffected.
	//
	// Transients differ a lot, though. With a 60s window the batch of events
	// from a finished conversation keeps the fire lit for a further 60s — the
	// perceived "not live" feeling is mostly that failure to fall, not a
	// failure to rise. At 20s the fire tracks activity like a real campfire.
	IntensityWindowSeconds float64

	// RateWindowSeconds is the window behind the displayed tok/s figure. It
	// stays at 60s because it reports "average burn rate", not "how hard that
	// last burst was" — shorten it and a single record (capped at 15000)
	// divided by the window blows past the 320 display cap, pinning the
	// readout.
	RateWindowSeconds float64

	// BurstRiseSeconds is the rise time constant on a burst. Throw a log on a
	// campfire and it flares instantly.
	BurstRiseSeconds float64

	// BurstJumpThreshold is how far the target must jump to count as "a big
	// armful of wood" and earn BurstRiseSeconds.
	BurstJumpThreshold float64

	// TpmAnchors / TpmIntensity are the piecewise TPM → intensity curve:
	// ~0.8k embers · ~2.5k small · ~12k medium · ~45k large · ~180k roaring.
	TpmAnchors   [6]float64
	TpmIntensity [6]float64

	// EventCreditTokens caps how much a single log line may contribute.
	EventCreditTokens float64

	IntensityRiseSeconds float64
	IntensityFallSeconds float64
	// IntensityFallSecondsIdle is the fall tau once the input stream has gone
	// silent. The 14s default is too slow — it looks "stuck on".
	IntensityFallSecondsIdle float64
	// IdleFallStartSeconds is how long silence must last before we treat the
	// input as stopped.
	IdleFallStartSeconds float64
	// IdleFallRampSeconds ramps from the normal fall tau to the idle one.
	IdleFallRampSeconds float64
	// IdleFlameRateTau is the exponential fade tau for ComputeInstantRate
	// after silence.
	IdleFlameRateTau float64

	FuelTokenScale          float64
	FuelBurnPerSecondAtFull float64
	FuelBurnPerSecondAtIdle float64
	EmberDecayPerSecond     float64
	EmberFromFuelGain       float64
	MaxSparkBurst           float64
	SparkDecayPerSecond     float64

	// DailyBase* raise a slow "banked embers" floor from the day's total. It
	// never participates in the headline UI.
	DailyBaseStartTokens  float64
	DailyBaseHalfTokens   float64
	DailyBaseMaxIntensity float64
}

// DefaultFireTuning returns the shipped tuning.
func DefaultFireTuning() FireTuning {
	return FireTuning{
		IntensityWindowSeconds:   20,
		RateWindowSeconds:        60,
		BurstRiseSeconds:         0.7,
		BurstJumpThreshold:       0.08,
		TpmAnchors:               [6]float64{0, 800, 2500, 12000, 45000, 180000},
		TpmIntensity:             [6]float64{0, 0.10, 0.26, 0.48, 0.72, 1.0},
		EventCreditTokens:        45000,
		IntensityRiseSeconds:     2.5,
		IntensityFallSeconds:     14,
		IntensityFallSecondsIdle: 5.0,
		IdleFallStartSeconds:     3.0,
		IdleFallRampSeconds:      4.0,
		IdleFlameRateTau:         3.0,
		FuelTokenScale:           120000,
		FuelBurnPerSecondAtFull:  1.0 / 90,
		FuelBurnPerSecondAtIdle:  1.0 / 150,
		EmberDecayPerSecond:      1.0 / (2.5 * 60),
		EmberFromFuelGain:        0.4,
		MaxSparkBurst:            1.0,
		SparkDecayPerSecond:      1.4,
		DailyBaseStartTokens:     40000,
		DailyBaseHalfTokens:      600000,
		DailyBaseMaxIntensity:    0.22,
	}
}

// SoftCompress lightly compresses a token count for the fuel increment only.
// Intensity itself uses the raw rate.
func SoftCompress(tokens float64) float64 {
	if tokens <= 0 {
		return 0
	}
	return tokens / (1.0 + tokens/140000.0)
}
