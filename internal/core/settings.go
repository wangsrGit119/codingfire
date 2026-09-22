package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Flame size
// ---------------------------------------------------------------------------

// FlameSize selects the on-screen campfire scale.
type FlameSize int

const (
	SizeSmall FlameSize = iota
	SizeMedium
	SizeLarge
)

// FlameSizesAll is the menu order.
var FlameSizesAll = []FlameSize{SizeSmall, SizeMedium, SizeLarge}

// Raw returns the persisted identifier.
func (s FlameSize) Raw() string {
	switch s {
	case SizeSmall:
		return "small"
	case SizeLarge:
		return "large"
	default:
		return "medium"
	}
}

// FlameSizeFromRaw parses a persisted identifier, defaulting to medium.
func FlameSizeFromRaw(raw string) FlameSize {
	switch raw {
	case "small":
		return SizeSmall
	case "large":
		return SizeLarge
	default:
		return SizeMedium
	}
}

// Label returns the localised size name.
func (s FlameSize) Label() string {
	switch s {
	case SizeSmall:
		return T("size.small")
	case SizeLarge:
		return T("size.large")
	default:
		return T("size.medium")
	}
}

// PixelScale is the pixel magnification — what the user actually sees.
func (s FlameSize) PixelScale() float64 {
	switch s {
	case SizeSmall:
		return 2.0
	case SizeLarge:
		return 5.5
	default:
		return 3.5
	}
}

// ---------------------------------------------------------------------------
// Paths
// ---------------------------------------------------------------------------

// AppPaths holds the on-disk layout. Everything lives under
// %APPDATA%\CodingFire, which is the SAME directory the C# build uses. The
// only thing that does NOT live here is the auto-start entry, which has to go
// to HKCU\...\Run (see autostart_windows.go) and to the equivalent place on
// the other two platforms.
//
// Sharing the root is deliberate: the file formats are identical, so the two
// builds are one app with two implementations, and switching between them
// keeps the whole history and the user's settings.
//
// The reason the roots were originally split was that two cursors over one log
// tree made each build skip whatever the other had already read. That cannot
// happen once the root is shared — there is one cursor over one store, and a
// cursor that is behind merely re-reads a range the store already holds, whose
// duplicate event ids are dropped on load.
//
// Two writers in this directory would still be a mistake, which is why the
// single-instance mutex is shared with the C# build too (see main.go).
//
// Older Go builds used a separate CodingFireGo directory. Nothing reads or
// writes it any more.
type appPaths struct{}

// AppPaths is the exported singleton.
var AppPaths appPaths

// DataDir returns the data directory, creating it if needed.
//
// CODINGFIRE_DATA_DIR overrides it, for portable mode and for automation that
// must not touch real user data.
func (appPaths) DataDir() string {
	overridden := strings.TrimSpace(os.Getenv("CODINGFIRE_DATA_DIR"))

	var dir string
	if overridden != "" {
		if abs, err := filepath.Abs(overridden); err == nil {
			dir = abs
		} else {
			dir = overridden
		}
	} else {
		dir = filepath.Join(configDir(), "CodingFire")
	}
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// SettingsFile is the single-file JSON settings path.
func (p appPaths) SettingsFile() string { return filepath.Join(p.DataDir(), "settings.json") }

// UsageFile is the append-only event log.
func (p appPaths) UsageFile() string { return filepath.Join(p.DataDir(), "usage.ndjson") }

// CursorsFile records per-file read offsets so rescans are incremental.
func (p appPaths) CursorsFile() string { return filepath.Join(p.DataDir(), "cursors.json") }

// MetaFile records the last known daily totals.
func (p appPaths) MetaFile() string { return filepath.Join(p.DataDir(), "meta.json") }

// LogFile is the diagnostic log.
func (p appPaths) LogFile() string { return filepath.Join(p.DataDir(), "codingfire.log") }

// Home returns the user profile directory, which source adapters expand
// "~" against.
func (appPaths) Home() string {
	if h := os.Getenv("USERPROFILE"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return configDir()
}

// Shorten collapses an absolute path to a ~/… display form.
func (p appPaths) Shorten(path string) string {
	if path == "" {
		return "—"
	}
	home := p.Home()
	if home != "" && len(path) >= len(home) && strings.EqualFold(path[:len(home)], home) {
		return "~" + path[len(home):]
	}
	return path
}

// CleanupStaleTempFiles removes .tmp files left behind by a hard kill. A
// clean exit never leaves them; they are never read, only untidy.
func (p appPaths) CleanupStaleTempFiles() {
	matches, err := filepath.Glob(filepath.Join(p.DataDir(), "*.tmp"))
	if err != nil {
		return
	}
	for _, f := range matches {
		_ = os.Remove(f)
	}
}

// WriteAtomicFile writes via a .tmp file and then replaces the target, so a
// reader (or a crash) never sees a half-written file and there is no window
// where the target has been deleted but not yet rewritten.
func (p appPaths) WriteAtomicFile(path, content string) {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		LogWarn("write failed: " + path + " — " + err.Error())
		return
	}
	if err := replaceFile(tmp, path); err != nil {
		LogWarn("atomic replace failed: " + path + " — " + err.Error())
		_ = os.Remove(tmp)
	}
}

// configDir resolves %APPDATA%, falling back through the environment because
// some sandboxes do not populate os.UserConfigDir.
func configDir() string {
	if d, err := os.UserConfigDir(); err == nil && d != "" {
		return d
	}
	if d := os.Getenv("APPDATA"); d != "" {
		return d
	}
	if h := os.Getenv("USERPROFILE"); h != "" {
		return filepath.Join(h, "AppData", "Roaming")
	}
	return "."
}

// ---------------------------------------------------------------------------
// App metadata
// ---------------------------------------------------------------------------

// AppInfo carries the application's identity. Version comes from version.go,
// which is the single source of truth — never repeat the literal elsewhere.
type appInfo struct{}

// AppInfo is the exported singleton.
var AppInfo appInfo

// Name is the product name.
func (appInfo) Name() string { return "CodingFire" }

// Version returns the value declared in version.go, e.g. "1.0.0".
func (appInfo) Version() string { return Version }

// DisplayName returns "CodingFire <version>" for tray tooltips and window titles.
func (appInfo) DisplayName() string { return "CodingFire " + Version }

// ProjectUrl is the single source of truth for the project home page. Both the
// tray menu item and the console About tab read it — do not inline the URL in
// either place.
func (appInfo) ProjectUrl() string { return "https://github.com/wangsrGit119/codingfire" }

// PlatformName names the system this build runs on, for the About tab.
//
// It exists because the tab said "Windows" outright, which was true of the
// first build and has been wrong on two platforms ever since. A product name is
// not translated, so this is not an L10n key.
func (appInfo) PlatformName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

// Settings is the persisted user configuration.
type Settings struct {
	Size         FlameSize
	HasPosition  bool
	PanelX       int
	PanelY       int
	Language     AppLanguage
	FlameVisible bool
	ShowLiveRate bool

	// AutoStart defaults to ON, so the first run installs the login entry (the
	// HKCU Run value on Windows, an XDG autostart file on Linux, a LaunchAgent
	// on macOS). The tray menu can turn it off. AutoStart syncs the actual
	// on-disk state.
	AutoStart bool

	// FlameColor is the flame theme colour, normalised RGB. Default classic
	// orange. The flame is single-colour; SourceColors only drives the small
	// dots in the console and the hover card.
	FlameColor AccentRGB

	AudioEnabled bool
	Volume       float64

	// SourceColors maps a source's raw identifier to [r,g,b] (0…1).
	SourceColors map[string]AccentRGB
}

// DefaultSettings returns the shipped defaults.
func DefaultSettings() *Settings {
	return &Settings{
		Size:         SizeMedium,
		Language:     LangSystem,
		FlameVisible: true,
		ShowLiveRate: true,
		AutoStart:    true,
		FlameColor:   DefaultFlameAccent,
		Volume:       0.5,
		SourceColors: map[string]AccentRGB{},
	}
}

var settingsGate sync.Mutex

// LoadSettings reads settings.json, falling back to defaults on any error.
// A missing or corrupt file is not an error condition.
func LoadSettings() *Settings {
	s := DefaultSettings()
	data, err := os.ReadFile(AppPaths.SettingsFile())
	if err != nil {
		return s
	}
	root := Of(ParseJSON(string(data)))

	s.Size = FlameSizeFromRaw(root.Str("size"))
	hasPos, _ := root.Bool("hasPosition")
	s.HasPosition = hasPos && root.Has("x") && root.Has("y")
	if v, ok := root.Long("x"); ok {
		s.PanelX = int(v)
	}
	if v, ok := root.Long("y"); ok {
		s.PanelY = int(v)
	}
	s.Language = langFromRaw(root.Str("language"))
	s.FlameVisible = root.BoolOr("visible", true)
	s.ShowLiveRate = root.BoolOr("showLiveRate", true)
	// An older config file has no autoStart key → default true.
	s.AutoStart = root.BoolOr("autoStart", true)

	if fc := root.Arr("flameColor"); fc.Len() >= 3 {
		if c, ok := readRGB(fc); ok {
			s.FlameColor = c
		}
	}
	s.AudioEnabled = root.BoolOr("audio", false)
	if v, ok := root.Num("volume"); ok {
		s.Volume = v
	}

	if colors := root.Obj("colors"); colors.Len() > 0 {
		for _, src := range UsageSourcesAll {
			arr := colors.Arr(src.Raw())
			if arr.Len() != 3 {
				continue
			}
			if c, ok := readRGB(arr); ok {
				s.SourceColors[src.Raw()] = c
			}
		}
	}
	return s
}

// readRGB reads three clamped components from a JSON array.
func readRGB(arr JArr) (AccentRGB, bool) {
	var c AccentRGB
	for i := 0; i < 3; i++ {
		v, ok := arr.NumAt(i)
		if !ok {
			return c, false
		}
		c[i] = clamp01(v)
	}
	return c, true
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

// Save writes settings.json atomically. Errors are logged, not returned —
// a failed settings write must never take the app down.
func (s *Settings) Save() {
	settingsGate.Lock()
	defer settingsGate.Unlock()

	var b strings.Builder
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  \"size\": %s,\n", quote(s.Size.Raw()))
	fmt.Fprintf(&b, "  \"hasPosition\": %t,\n", s.HasPosition)
	fmt.Fprintf(&b, "  \"x\": %d,\n", s.PanelX)
	fmt.Fprintf(&b, "  \"y\": %d,\n", s.PanelY)
	fmt.Fprintf(&b, "  \"language\": %s,\n", quote(langRaw(s.Language)))
	fmt.Fprintf(&b, "  \"visible\": %t,\n", s.FlameVisible)
	fmt.Fprintf(&b, "  \"showLiveRate\": %t,\n", s.ShowLiveRate)
	fmt.Fprintf(&b, "  \"autoStart\": %t,\n", s.AutoStart)
	fmt.Fprintf(&b, "  \"flameColor\": [%s, %s, %s],\n",
		formatMax4(s.FlameColor[0]), formatMax4(s.FlameColor[1]), formatMax4(s.FlameColor[2]))
	fmt.Fprintf(&b, "  \"audio\": %t,\n", s.AudioEnabled)
	fmt.Fprintf(&b, "  \"volume\": %s,\n", formatMax3(s.Volume))
	b.WriteString("  \"colors\": {")

	// Iterate in UsageSource order rather than map order so the file is
	// stable across runs and diffs cleanly.
	first := true
	for _, src := range UsageSourcesAll {
		c, ok := s.SourceColors[src.Raw()]
		if !ok {
			continue
		}
		if !first {
			b.WriteString(",")
		}
		first = false
		fmt.Fprintf(&b, "\n    %s: [%s, %s, %s]",
			quote(src.Raw()), formatMax4(c[0]), formatMax4(c[1]), formatMax4(c[2]))
	}
	if !first {
		b.WriteString("\n  ")
	}
	b.WriteString("}\n")
	b.WriteString("}\n")

	AppPaths.WriteAtomicFile(AppPaths.SettingsFile(), b.String())
}

// SourcesWithCustomColor lists sources in colour-band order that have an
// explicit colour override.
func (s *Settings) SourcesWithCustomColor() []UsageSource {
	var out []UsageSource
	for _, src := range UsageSourcesAll {
		if _, ok := s.SourceColors[src.Raw()]; ok {
			out = append(out, src)
		}
	}
	return out
}

func langRaw(l AppLanguage) string {
	switch l {
	case LangEnglish:
		return "en"
	case LangChineseSimplified:
		return "zh-Hans"
	case LangJapanese:
		return "ja"
	case LangKorean:
		return "ko"
	default:
		return "system"
	}
}

func langFromRaw(raw string) AppLanguage {
	switch raw {
	case "en":
		return LangEnglish
	case "zh-Hans":
		return LangChineseSimplified
	case "ja":
		return LangJapanese
	case "ko":
		return LangKorean
	default:
		return LangSystem
	}
}

// QuoteString renders a JSON string literal. Exported for the store's
// line encoder.
func QuoteString(s string) string { return quote(s) }

// quote renders a JSON string literal.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, "\\u%04x", r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// formatMax4 renders with at most 4 decimals, trailing zeros trimmed —
// the Go equivalent of C#'s ToString("0.####").
func formatMax4(v float64) string { return formatTrimmed(v, 4) }

// formatMax3 is the same with at most 3 decimals.
func formatMax3(v float64) string { return formatTrimmed(v, 3) }

func formatTrimmed(v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	if strings.ContainsRune(s, '.') {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	if s == "-0" {
		return "0"
	}
	return s
}

// ---------------------------------------------------------------------------
// Logging
// ---------------------------------------------------------------------------

var (
	logGate     sync.Mutex
	logDisabled bool
)

// logMaxBytes rotates the log to a single .1 once it grows past this. The app
// is a long-running resident process, so an unbounded log would grow forever.
const logMaxBytes = 512 * 1024

// LogWarn appends a WARN line.
func LogWarn(message string) { logWrite("WARN", message) }

// LogInfo appends an INFO line.
func LogInfo(message string) { logWrite("INFO", message) }

func logWrite(level, message string) {
	if logDisabled {
		return
	}
	logGate.Lock()
	defer logGate.Unlock()

	rotateLogIfNeeded()
	line := time.Now().Format("2006-01-02 15:04:05") + " [" + level + "] " + message + "\r\n"
	f, err := os.OpenFile(AppPaths.LogFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logDisabled = true // unwritable disk: degrade silently, do not re-throw
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// rotateLogIfNeeded keeps the current log plus one .1, dropping older ones.
// The log is written rarely enough that the extra stat is not worth avoiding.
func rotateLogIfNeeded() {
	path := AppPaths.LogFile()
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < logMaxBytes {
		return
	}
	previous := path + ".1"
	_ = os.Remove(previous)
	_ = os.Rename(path, previous)
}
