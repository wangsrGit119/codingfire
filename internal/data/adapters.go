//
//  adapters.go — CodingFire (Go)
//
//  Port of src/Data/Adapters.cs: the shared adapter scaffolding (PathUtil,
//  the LogAdapter base, token and timestamp helpers) plus the original five
//  log sources — Claude Code / Codex / Grok / Pi / Amp — and the
//  source→adapter map the monitor iterates.
//
//  Every adapter is strictly read-only: it reads local files and nothing else.
//  Nothing here executes a query, writes into a tool's directory, or touches
//  the network. The SQLite sources in adapters2.go read database pages
//  directly rather than going through a SQL engine.
//
//  Conventions kept from the C#:
//   - only billable token classes are counted; cache read/write stay split out
//   - cumulative fields are converted to increments by the monitor, so the
//     IsCumulative() set must match the C# exactly
//   - event ids are deterministic, because the store deduplicates on them
//   - a malformed line or a missing directory yields no events, never a panic
//

package data

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// ---------------------------------------------------------------------------
// Path helpers (port of PathUtil)
// ---------------------------------------------------------------------------

// pathEnv reads an environment variable, treating empty/whitespace as unset —
// the C# PathUtil.Env contract.
func pathEnv(name string) (string, bool) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return "", false
	}
	return v, true
}

// expandTilde resolves a leading "~" against the user profile.
func expandTilde(p string) string {
	if p == "" || !strings.HasPrefix(p, "~") {
		return p
	}
	rest := strings.TrimLeft(p, "~")
	rest = strings.TrimLeft(rest, `/\`)
	return combinePath(core.AppPaths.Home(), rest)
}

// combinePath folds path segments like PathUtil.Combine: empty segments are
// skipped, and an all-empty list yields "".
func combinePath(parts ...string) string {
	acc := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if acc == "" {
			acc = p
			continue
		}
		acc = filepath.Join(acc, p)
	}
	return acc
}

// stableHash reproduces PathUtil.StableHash: FNV-1a over the string's UTF-16
// code units, rendered as eight lowercase hex digits. It hashes UTF-16 units
// rather than runes so a line containing non-ASCII text hashes the same way the
// C# build hashes it.
func stableHash(s string) string {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	mix := func(u uint32) {
		h ^= u
		h *= prime
	}
	for _, r := range s {
		if r > 0xFFFF {
			r -= 0x10000
			mix(uint32(0xD800 + (r >> 10)))
			mix(uint32(0xDC00 + (r & 0x3FF)))
			continue
		}
		mix(uint32(r))
	}
	return fmt.Sprintf("%08x", h)
}

// appDataDir is %APPDATA% (Roaming), where the VS Code-family hosts, Amp and
// the Qoder IDE keep their per-user state.
//
// The fallbacks matter: .NET resolves SpecialFolder.ApplicationData through the
// shell, so it still answers when the environment variable is missing, and
// os.UserConfigDir alone would not. The last resort mirrors core.configDir.
func appDataDir() string {
	if d := os.Getenv("APPDATA"); d != "" {
		return d
	}
	if d, err := os.UserConfigDir(); err == nil && d != "" {
		return d
	}
	if h := os.Getenv("USERPROFILE"); h != "" {
		return filepath.Join(h, "AppData", "Roaming")
	}
	return ""
}

// dirName mirrors Path.GetDirectoryName: "" when the path carries no directory
// component (Go would say ".").
func dirName(p string) string {
	if p == "" {
		return ""
	}
	d := filepath.Dir(p)
	if d == "." && !strings.ContainsAny(p, `/\`) {
		return ""
	}
	return d
}

// nameWithoutExt is Path.GetFileNameWithoutExtension.
func nameWithoutExt(p string) string {
	b := filepath.Base(p)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// fileExists mirrors File.Exists, which is false for a directory.
func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// ---------------------------------------------------------------------------
// Timestamps
// ---------------------------------------------------------------------------

// isoZonedLayouts carry an explicit offset or "Z".
var isoZonedLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999Z0700",
	"2006-01-02T15:04:05Z0700",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05.999999999Z0700",
	"2006-01-02 15:04:05Z0700",
}

// isoLocalLayouts carry no zone; the value is local wall-clock time, matching
// the C# parser's Unspecified-kind branch.
var isoLocalLayouts = []string{
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseISO8601 accepts "2026-09-15T10:00:00.123Z", "+00:00", and a missing
// zone, and always returns local time.
func parseISO8601(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range isoZonedLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Local(), true
		}
	}
	for _, layout := range isoLocalLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseEpochAny reads epoch seconds, or milliseconds when the value is larger
// than 1e12. Non-positive and non-finite values are rejected.
func parseEpochAny(v float64) (time.Time, bool) {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return time.Time{}, false
	}
	sec := v
	if v > 1e12 {
		sec = v / 1000
	}
	whole := math.Floor(sec)
	nanos := int64((sec - whole) * 1e9)
	return time.Unix(int64(whole), nanos).Local(), true
}

// epochOrISO reads a timestamp that may be epoch seconds/millis or an ISO
// string — the shape DSH and OpenClaw write.
func epochOrISO(raw any) (time.Time, bool) {
	switch v := raw.(type) {
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return parseEpochAny(f)
		}
	case float64:
		return parseEpochAny(v)
	case int64:
		return parseEpochAny(float64(v))
	case string:
		return parseISO8601(v)
	}
	return time.Time{}, false
}

// formatSeconds3 renders with at most three decimals, trailing zeros trimmed —
// the Go equivalent of C#'s ToString("0.###"), used inside event ids.
func formatSeconds3(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
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
// Token helpers
// ---------------------------------------------------------------------------

// nonNeg is the port of LogAdapter.NonNeg: null, booleans, NaN, infinities and
// negatives all become 0, and the result is clamped to int32 max.
func nonNeg(v any) int {
	switch n := v.(type) {
	case nil, bool:
		return 0
	case int:
		return clampNonNegInt(int64(n))
	case int64:
		return clampNonNegInt(n)
	case float64:
		return nonNegFloat(n)
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return nonNegFloat(f)
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return nonNegFloat(f)
		}
	}
	return 0
}

func clampNonNegInt(v int64) int {
	if v < 0 {
		return 0
	}
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int(v)
}

func nonNegFloat(d float64) int {
	if math.IsNaN(d) || math.IsInf(d, 0) || d < 0 {
		return 0
	}
	if d >= math.MaxInt32 {
		return math.MaxInt32
	}
	return int(math.Floor(d))
}

// intVal is `x.Int(key) ?? 0`.
func intVal(o core.JObj, key string) int {
	v, _ := o.Int(key)
	return v
}

// itoa64 renders a 64-bit integer. A millisecond epoch does not fit in a
// 32-bit int, so it must never go through itoa.
func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

// posPtr reports a class only when it is non-zero, mirroring Token4.Breakdown.
func posPtr(v int) *int {
	if v > 0 {
		return core.IntPtr(v)
	}
	return nil
}

// optPtr reports a class whenever the source actually carried the field —
// nil and 0 mean different things to the console.
func optPtr(v int, present bool) *int {
	if !present {
		return nil
	}
	return core.IntPtr(v)
}

// token4 is the four-class counter from Adapters2.cs. The C# version lives in
// the "newer adapters" file, but it is shared toolkit, so it lives here.
type token4 struct{ input, output, cacheRead, cacheWrite int }

func (t token4) total() int { return t.input + t.output + t.cacheRead + t.cacheWrite }

func (t token4) breakdown() core.UsageBreakdown {
	return core.UsageBreakdown{
		Input:      posPtr(t.input),
		Output:     posPtr(t.output),
		CacheRead:  posPtr(t.cacheRead),
		CacheWrite: posPtr(t.cacheWrite),
	}
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

// parseJObj decodes one line and insists on a top-level object, exactly like
// `Json.Parse(text) as JObj`.
func parseJObj(line string) (core.JObj, bool) {
	m, ok := core.ParseJSON(strings.TrimSpace(line)).(map[string]any)
	if !ok {
		return core.JObj{}, false
	}
	return core.Of(m), true
}

// objField reports the nested object at key. It returns false when the value is
// missing, null or of another type, which is what the C# `x == JObj.Empty`
// reference comparison actually tested (Obj() returns the shared Empty
// instance only when the value is not an object).
func objField(o core.JObj, key string) (core.JObj, bool) {
	if m, ok := o.Raw(key).(map[string]any); ok {
		return core.Of(m), true
	}
	return core.JObj{}, false
}

// firstStr is a C# `a ?? b ?? c` chain: the first key holding a JSON string
// wins, and an empty string still counts as present.
func firstStr(o core.JObj, keys ...string) (string, bool) {
	for _, k := range keys {
		if s, ok := o.StrOk(k); ok {
			return s, true
		}
	}
	return "", false
}

// firstNonEmpty is a `string.IsNullOrEmpty` chain: the first non-empty string
// wins, without trimming.
func firstNonEmpty(o core.JObj, keys ...string) (string, bool) {
	for _, k := range keys {
		if s, ok := o.StrOk(k); ok && s != "" {
			return s, true
		}
	}
	return "", false
}

// trimmedStr is the C# `Trimmed(x.Str(k))` pattern: whitespace-only counts as
// absent.
func trimmedStr(o core.JObj, key string) (string, bool) {
	s, ok := o.StrOk(key)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s, true
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

// ---------------------------------------------------------------------------
// File helpers
// ---------------------------------------------------------------------------

// walkMaxDirs caps a single walk. Log trees are a handful of levels deep, so
// this only ever trips on a directory cycle (a junction pointing at one of its
// own ancestors), which would otherwise recurse forever.
const walkMaxDirs = 100000

// walkFiles is the port of LogAdapter.Walk: a LIFO directory walk that skips
// dot-entries and unreadable directories, yielding files whose name ends with
// the given extension (case-insensitive).
func walkFiles(root, extension string) []string {
	var out []string
	ext := strings.ToLower(extension)
	stack := []string{root}
	for dirs := 0; len(stack) > 0; dirs++ {
		if dirs > walkMaxDirs {
			core.LogWarn("walk aborted: too many directories under " + root)
			break
		}
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			p := filepath.Join(dir, name)
			if e.IsDir() {
				stack = append(stack, p)
				continue
			}
			if strings.HasSuffix(strings.ToLower(name), ext) {
				out = append(out, p)
			}
		}
	}
	return out
}

// modifiedSince reports whether the file was written at or after since.
func modifiedSince(path string, since time.Time) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !fi.ModTime().Before(since)
}

// readAllTextShared reads a file another process may be writing, tolerating a
// UTF-8 BOM. Files larger than 64 MB are rejected: they are not our format.
func readAllTextShared(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil || fi.Size() <= 0 || fi.Size() > 64*1024*1024 {
		return "", false
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return "", false
	}
	buf = bytes.TrimPrefix(buf, []byte{0xEF, 0xBB, 0xBF})
	return string(buf), true
}

// splitRoots splits an env-var path list. ':' is deliberately not a separator,
// because it is a drive letter on Windows.
func splitRoots(value string) []string {
	if value == "" {
		return nil
	}
	var out []string
	for _, p := range strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == ',' }) {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// fileChanged reports whether size or mtime moved since the previous call,
// updating the caller's remembered stamp — the port of AdapterIo.Changed.
func fileChanged(path string, lastLen, lastTicks *int64) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	l, t := fi.Size(), fi.ModTime().UnixNano()
	if l == *lastLen && t == *lastTicks {
		return false
	}
	*lastLen, *lastTicks = l, t
	return true
}

type fileStamp struct{ size, modNano int64 }

// fileStampCache skips re-parsing a whole-file source whose size and mtime have
// not moved. Line-based sources have a byte cursor and do not need it; the
// session archives here can be several megabytes.
type fileStampCache struct {
	gate sync.Mutex
	m    map[string]fileStamp
}

func (c *fileStampCache) changed(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	s := fileStamp{size: fi.Size(), modNano: fi.ModTime().UnixNano()}

	c.gate.Lock()
	defer c.gate.Unlock()
	if c.m == nil {
		c.m = map[string]fileStamp{}
	}
	if prev, ok := c.m[path]; ok && prev == s {
		return false
	}
	c.m[path] = s
	return true
}

// ---------------------------------------------------------------------------
// LogAdapter base
// ---------------------------------------------------------------------------

// baseAdapter carries the default behaviour every adapter inherits: the primary
// root, the connection probe, and the "this source has no whole-file format /
// no database" answers. Adapters override the parts that apply to them.
type baseAdapter struct{ root string }

// WatchRoots returns the data root. Sources that keep usage outside the log
// tree (SQLite databases) override this to add the database file, otherwise
// their changes would only be noticed by the fallback heartbeat.
func (b baseAdapter) WatchRoots() []string {
	if b.root == "" {
		return nil
	}
	return []string{b.root}
}

// CheckConnection probes whether the root exists and is listable.
func (b baseAdapter) CheckConnection() (core.SourceConnectionState, string) {
	if b.root == "" {
		return core.SourceNotFound, "—"
	}
	detail := core.AppPaths.Shorten(b.root)
	if !isDir(b.root) {
		return core.SourceNotFound, detail
	}
	if _, err := os.ReadDir(b.root); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return core.SourceNoPermission, detail
		}
		return core.SourceReadError, detail
	}
	return core.SourceOk, detail
}

// ParseThread returns nil: this source is read line by line. Returning an empty
// non-nil slice would claim the file and suppress the fallback reader.
func (b baseAdapter) ParseThread(string) []core.UsageEvent { return nil }

// ParseLine returns ok=false: this source has no line format.
func (b baseAdapter) ParseLine(string, string) (core.UsageEvent, bool) {
	return core.UsageEvent{}, false
}

// ParseDatabase returns nil: this source keeps no SQLite database.
func (b baseAdapter) ParseDatabase() []core.UsageEvent { return nil }

// IsCumulative reports that a record is written once and never grows.
func (b baseAdapter) IsCumulative() bool { return false }

// discoverJSONL is the shared "walk the root, keep recent .jsonl files" body
// used by most of the line-based sources.
func discoverJSONL(root string, since time.Time) []string {
	if !isDir(root) {
		return nil
	}
	var out []string
	for _, f := range walkFiles(root, ".jsonl") {
		if modifiedSince(f, since) {
			out = append(out, f)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Claude Code — ~/.claude/projects/**/*.jsonl where type == "assistant"
// ---------------------------------------------------------------------------

type claudeCodeAdapter struct{ baseAdapter }

func newClaudeCodeAdapter() *claudeCodeAdapter {
	root := ""
	if cfg, ok := pathEnv("CLAUDE_CONFIG_DIR"); ok {
		root = combinePath(expandTilde(cfg), "projects")
	} else {
		root = combinePath(core.AppPaths.Home(), ".claude", "projects")
	}
	return &claudeCodeAdapter{baseAdapter{root: root}}
}

// IsCumulative: the same assistant message reappears as output streams, each
// time carrying a larger usage. The monitor keeps the largest total seen and
// counts only the difference.
func (a *claudeCodeAdapter) IsCumulative() bool { return true }

func (a *claudeCodeAdapter) DiscoverLogFiles(since time.Time) []string {
	return discoverJSONL(a.root, since)
}

func (a *claudeCodeAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok || obj.Str("type") != "assistant" {
		return core.UsageEvent{}, false
	}

	msg := obj.Obj("message")
	usage := msg.Obj("usage")
	if !usage.Has("input_tokens") && !usage.Has("output_tokens") &&
		!usage.Has("cache_read_input_tokens") && !usage.Has("cache_creation_input_tokens") {
		return core.UsageEvent{}, false
	}

	input, hasInput := usage.Int("input_tokens")
	output, hasOutput := usage.Int("output_tokens")
	cacheWrite, hasCacheWrite := usage.Int("cache_creation_input_tokens")
	cacheRead, hasCacheRead := usage.Int("cache_read_input_tokens")

	// Anthropic reports input / cache_read / cache_creation as three parallel
	// buckets, so the four classes simply add up.
	total := input + output + cacheWrite + cacheRead
	if total <= 0 {
		return core.UsageEvent{}, false
	}

	var id string
	if v, ok := trimmedStr(msg, "id"); ok {
		id = "claude:" + v
	} else if v, ok := trimmedStr(obj, "uuid"); ok {
		id = "claude-uuid:" + v
	} else {
		id = "claude:" + filepath.Base(filePath) + ":" + stableHash(line)
	}

	ts, ok := parseISO8601(obj.Str("timestamp"))
	if !ok {
		ts = time.Now()
	}

	return core.UsageEvent{
		ID:        id,
		Source:    core.ClaudeCode,
		Timestamp: ts,
		Tokens:    total,
		Breakdown: core.UsageBreakdown{
			Input:      optPtr(input, hasInput),
			Output:     optPtr(output, hasOutput),
			CacheRead:  optPtr(cacheRead, hasCacheRead),
			CacheWrite: optPtr(cacheWrite, hasCacheWrite),
		},
		FilePath: filePath,
	}, true
}

// ---------------------------------------------------------------------------
// Codex — ~/.codex/sessions/**/*.jsonl where type == "token_usage_record"
// ---------------------------------------------------------------------------

type codexAdapter struct{ baseAdapter }

func newCodexAdapter() *codexAdapter {
	root := ""
	if home, ok := pathEnv("CODEX_HOME"); ok {
		root = combinePath(expandTilde(home), "sessions")
	} else {
		root = combinePath(core.AppPaths.Home(), ".codex", "sessions")
	}
	return &codexAdapter{baseAdapter{root: root}}
}

func (a *codexAdapter) DiscoverLogFiles(since time.Time) []string {
	return discoverJSONL(a.root, since)
}

func (a *codexAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok || obj.Str("type") != "token_usage_record" {
		return core.UsageEvent{}, false
	}

	payload := obj.Obj("payload")
	usage := payload.Obj("usage")

	input, hasInput := usage.Int("input_tokens")
	output, _ := usage.Int("output_tokens")
	// reasoning_output_tokens is a subset of output and is not added again.
	cacheRead, hasCacheRead := usage.Int("cached_input_tokens")
	cacheWrite, hasCacheWrite := usage.Int("cache_write_input_tokens")

	// OpenAI counts cached tokens inside input_tokens, so the total is
	// input + output.
	total := input + output
	if listed, ok := usage.Int("total_tokens"); ok && listed > 0 {
		total = listed
	}
	if total <= 0 {
		return core.UsageEvent{}, false
	}

	var id string
	if responseID, ok := firstNonEmpty(payload, "response_id"); ok {
		id = "codex:" + responseID
	} else if ordinal, ok := obj.Int("ordinal"); ok {
		id = "codex:" + payload.StrOr("session_id", "unknown") + ":" + itoa(ordinal)
	} else {
		id = "codex:" + filepath.Base(filePath) + ":" + stableHash(line)
	}

	// The console shows cache separately, so input is displayed net of the
	// cache hits it already contains.
	var nonCacheInput *int
	if hasInput {
		v := input
		if hasCacheRead {
			v = max(0, input-cacheRead)
		}
		nonCacheInput = core.IntPtr(v)
	}

	ts, ok := parseISO8601(obj.Str("timestamp"))
	if !ok {
		ts = time.Now()
	}

	return core.UsageEvent{
		ID:        id,
		Source:    core.Codex,
		Timestamp: ts,
		Tokens:    total,
		Breakdown: core.UsageBreakdown{
			Input:      nonCacheInput,
			Output:     optPtr(output, true),
			CacheRead:  optPtr(cacheRead, hasCacheRead),
			CacheWrite: optPtr(cacheWrite, hasCacheWrite),
		},
		FilePath: filePath,
	}, true
}

// ---------------------------------------------------------------------------
// Grok — ~/.grok/sessions/**/updates.jsonl (turn_completed)
//        plus the older ~/.grok/logs/unified.jsonl (shell.turn.inference_done)
// ---------------------------------------------------------------------------

type grokAdapter struct {
	baseAdapter
	sessions string
	unified  string
}

func newGrokAdapter() *grokAdapter {
	home := ""
	if env, ok := pathEnv("GROK_HOME"); ok {
		home = expandTilde(env)
	} else {
		home = combinePath(core.AppPaths.Home(), ".grok")
	}
	return &grokAdapter{
		baseAdapter: baseAdapter{root: home},
		sessions:    combinePath(home, "sessions"),
		unified:     combinePath(home, "logs", "unified.jsonl"),
	}
}

func (a *grokAdapter) DiscoverLogFiles(since time.Time) []string {
	var out []string
	if isDir(a.sessions) {
		for _, f := range walkFiles(a.sessions, ".jsonl") {
			if !strings.EqualFold(filepath.Base(f), "updates.jsonl") {
				continue
			}
			if modifiedSince(f, since) {
				out = append(out, f)
			}
		}
	}
	// The legacy log is small and is read in full every pass; its ids
	// deduplicate, so the mtime filter is deliberately not applied.
	if fileExists(a.unified) {
		out = append(out, a.unified)
	}
	return out
}

func (a *grokAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	raw := strings.TrimSpace(line)
	obj, ok := parseJObj(raw)
	if !ok {
		return core.UsageEvent{}, false
	}
	switch {
	case obj.Str("sessionUpdate") == "turn_completed":
		return a.parseTurnCompleted(obj, filePath, raw)
	case obj.Str("msg") == "shell.turn.inference_done":
		return a.parseUnified(obj, filePath)
	}
	return core.UsageEvent{}, false
}

func (a *grokAdapter) parseTurnCompleted(obj core.JObj, filePath, raw string) (core.UsageEvent, bool) {
	usage := obj
	if obj.Has("usage") {
		usage = obj.Obj("usage")
	}

	inputTotal := intVal(usage, "inputTokens")
	output := intVal(usage, "outputTokens")
	cacheRead := intVal(usage, "cachedReadTokens")
	cacheWrite := intVal(usage, "cacheCreationTokens")

	total := inputTotal + output
	if listed, ok := usage.Int("totalTokens"); ok && listed > 0 {
		total = listed
	}
	if total <= 0 {
		return core.UsageEvent{}, false
	}

	turnID, ok := firstStr(obj, "turnId", "promptId")
	if !ok {
		turnID = filepath.Base(filePath) + ":" + stableHash(raw)
	}

	ts, ok := parseISO8601(obj.Str("timestamp"))
	if !ok {
		ts, ok = parseISO8601(obj.Str("ts"))
	}
	if !ok {
		ts = time.Now()
	}

	return core.UsageEvent{
		ID:        "grok:turn:" + turnID,
		Source:    core.Grok,
		Timestamp: ts,
		Tokens:    total,
		Breakdown: core.UsageBreakdown{
			Input:      core.IntPtr(max(0, inputTotal-cacheRead)),
			Output:     core.IntPtr(output),
			CacheRead:  posPtr(cacheRead),
			CacheWrite: posPtr(cacheWrite),
		},
		FilePath: filePath,
	}, true
}

func (a *grokAdapter) parseUnified(obj core.JObj, filePath string) (core.UsageEvent, bool) {
	ctx := obj.Obj("ctx")
	promptTotal := max(0, intVal(ctx, "prompt_tokens"))
	cached := min(promptTotal, max(0, intVal(ctx, "cached_prompt_tokens")))
	output := max(0, intVal(ctx, "completion_tokens"))

	total := promptTotal + output
	if total <= 0 {
		return core.UsageEvent{}, false
	}

	ts, ok := parseISO8601(obj.Str("ts"))
	if !ok {
		ts = time.Now()
	}

	id := "grok:unified:" + obj.StrOr("sid", "unknown") + ":" +
		formatSeconds3(ts.UTC().Sub(epoch).Seconds()) + ":" + itoa(total)

	return core.UsageEvent{
		ID:        id,
		Source:    core.Grok,
		Timestamp: ts,
		Tokens:    total,
		Breakdown: core.UsageBreakdown{
			Input:     core.IntPtr(max(0, promptTotal-cached)),
			Output:    core.IntPtr(output),
			CacheRead: posPtr(cached),
		},
		FilePath: filePath,
	}, true
}

// ---------------------------------------------------------------------------
// Pi (pi-agent) — ~/.pi/agent/sessions/**/*.jsonl with message.role == assistant
// ---------------------------------------------------------------------------

type piAdapter struct{ baseAdapter }

func newPiAdapter() *piAdapter {
	root := ""
	if env, ok := pathEnv("PI_AGENT_DIR"); ok {
		root = expandTilde(env)
	} else {
		root = combinePath(core.AppPaths.Home(), ".pi", "agent", "sessions")
	}
	return &piAdapter{baseAdapter{root: root}}
}

// CheckConnection treats a present ~/.pi/agent as "installed but perhaps not
// used yet", so the source is not reported as missing.
func (a *piAdapter) CheckConnection() (core.SourceConnectionState, string) {
	state, detail := a.baseAdapter.CheckConnection()
	if state != core.SourceNotFound {
		return state, detail
	}
	if parent := dirName(a.root); parent != "" && isDir(parent) {
		return core.SourceOk, core.AppPaths.Shorten(parent)
	}
	return core.SourceNotFound, detail
}

func (a *piAdapter) DiscoverLogFiles(since time.Time) []string {
	return discoverJSONL(a.root, since)
}

func (a *piAdapter) ParseLine(line, filePath string) (core.UsageEvent, bool) {
	if blank(line) {
		return core.UsageEvent{}, false
	}
	obj, ok := parseJObj(line)
	if !ok {
		return core.UsageEvent{}, false
	}

	message := obj.Obj("message")
	if message.Str("role") != "assistant" {
		return core.UsageEvent{}, false
	}
	usage, ok := objField(message, "usage")
	if !ok {
		return core.UsageEvent{}, false
	}

	input := intVal(usage, "input")
	output := intVal(usage, "output")
	cacheRead := intVal(usage, "cacheRead")
	cacheWrite, ok := usage.Int("cacheWrite")
	if !ok {
		cacheWrite, _ = usage.Int("cacheWrite1h")
	}

	total := input + output + cacheRead + cacheWrite
	if listed, ok := usage.Int("totalTokens"); ok && listed > 0 {
		total = listed
	}
	if total <= 0 {
		return core.UsageEvent{}, false
	}

	ts, ok := parseISO8601(obj.Str("timestamp"))
	if !ok {
		if v, has := message.Num("timestamp"); has {
			ts, ok = parseEpochAny(v)
		}
	}
	if !ok {
		ts = time.Now()
	}

	msgID, ok := firstStr(message, "id")
	if !ok {
		msgID, ok = firstStr(obj, "id")
	}
	if !ok {
		msgID = filepath.Base(filePath) + ":" + stableHash(line)
	}

	return core.UsageEvent{
		ID:        "pi:" + msgID,
		Source:    core.Pi,
		Timestamp: ts,
		Tokens:    total,
		Breakdown: core.UsageBreakdown{
			Input:      posPtr(input),
			Output:     posPtr(output),
			CacheRead:  posPtr(cacheRead),
			CacheWrite: posPtr(cacheWrite),
		},
		FilePath: filePath,
	}, true
}

// ---------------------------------------------------------------------------
// Amp (Sourcegraph) — <data root>/threads/**/*.json with usageLedger.events
//   macOS keeps this under ~/Library/Application Support/amp; on Windows it is
//   %APPDATA%\amp.
// ---------------------------------------------------------------------------

type ampAdapter struct {
	baseAdapter
	roots []string
}

func newAmpAdapter() *ampAdapter {
	roots := ampDataRoots()
	root := ""
	if len(roots) > 0 {
		root = roots[0]
	}
	for _, r := range roots {
		if isDir(r) {
			root = r
			break
		}
	}
	return &ampAdapter{baseAdapter: baseAdapter{root: root}, roots: roots}
}

func ampDataRoots() []string {
	if env, ok := pathEnv("AMP_DATA_DIR"); ok {
		var list []string
		for _, p := range strings.Split(env, ",") {
			if t := strings.TrimSpace(p); t != "" {
				list = append(list, expandTilde(t))
			}
		}
		if len(list) > 0 {
			return list
		}
	}
	return []string{
		combinePath(core.AppPaths.Home(), ".local", "share", "amp"),
		combinePath(appDataDir(), "amp"),
	}
}

func (a *ampAdapter) DiscoverLogFiles(since time.Time) []string {
	var out []string
	for _, root := range a.roots {
		threads := combinePath(root, "threads")
		if !isDir(threads) {
			continue
		}
		for _, f := range walkFiles(threads, ".json") {
			if modifiedSince(f, since) {
				out = append(out, f)
			}
		}
	}
	return out
}

// ParseThread reads a whole thread file, which may yield several events. It
// always claims the file: Amp has no line format, so an empty (but non-nil)
// result is correct and must not fall back to line reading.
func (a *ampAdapter) ParseThread(filePath string) []core.UsageEvent {
	out := []core.UsageEvent{}

	text, ok := readAllTextShared(filePath)
	if !ok {
		return out
	}
	m, ok := core.ParseJSON(text).(map[string]any)
	if !ok {
		return out
	}
	obj := core.Of(m)

	threadID, ok := obj.StrOk("id")
	if !ok {
		threadID = nameWithoutExt(filePath)
	}

	events := obj.Obj("usageLedger").Arr("events")
	if events.Len() == 0 {
		return out
	}
	messages := obj.Arr("messages")

	for i := 0; i < events.Len(); i++ {
		ev := events.ObjAt(i)
		model, ok := ev.StrOk("model")
		if !ok {
			continue
		}
		tokens, ok := objField(ev, "tokens")
		if !ok {
			continue
		}

		input := intVal(tokens, "input")
		output := intVal(tokens, "output")
		toMessageID, hasToMessage := ev.Int("toMessageId")
		cacheWrite, cacheRead := ampCacheTokens(messages, toMessageID, hasToMessage)

		total := input + output + cacheWrite + cacheRead
		if total <= 0 {
			continue
		}

		ts, ok := parseISO8601(ev.Str("timestamp"))
		if !ok {
			ts = time.Now()
		}

		msgRef := -1
		if hasToMessage {
			msgRef = toMessageID
		}
		id := strings.Join([]string{
			"amp", threadID,
			formatSeconds3(ts.UTC().Sub(epoch).Seconds()),
			model, itoa(input), itoa(output), itoa(cacheWrite), itoa(cacheRead), itoa(msgRef),
		}, ":")

		out = append(out, core.UsageEvent{
			ID:        id,
			Source:    core.Amp,
			Timestamp: ts,
			Tokens:    total,
			Breakdown: core.UsageBreakdown{
				Input:      posPtr(input),
				Output:     posPtr(output),
				CacheRead:  posPtr(cacheRead),
				CacheWrite: posPtr(cacheWrite),
			},
			FilePath: filePath,
		})
	}
	return out
}

// ampCacheTokens finds the assistant message a ledger entry points at, which is
// where Amp records the cache classes.
func ampCacheTokens(messages core.JArr, toMessageID int, hasToMessage bool) (cacheWrite, cacheRead int) {
	if !hasToMessage {
		return 0, 0
	}
	for i := 0; i < messages.Len(); i++ {
		m := messages.ObjAt(i)
		if m.Str("role") != "assistant" {
			continue
		}
		msgID, ok := m.Int("messageId")
		if !ok {
			msgID = -1
		}
		if msgID != toMessageID {
			continue
		}
		usage := m.Obj("usage")
		return intVal(usage, "cacheCreationInputTokens"), intVal(usage, "cacheReadInputTokens")
	}
	return 0, 0
}

// ---------------------------------------------------------------------------
// The source → adapter map
// ---------------------------------------------------------------------------

// DefaultAdapters returns one adapter per collected source, keyed by source.
//
// Cursor and Kiro are deliberately absent: they exist in the UsageSource enum
// only to keep the colour-band indices stable, and neither has local token
// metadata to read. That is 23 entries, not 25.
func DefaultAdapters() map[core.UsageSource]LogAdapter {
	return map[core.UsageSource]LogAdapter{
		core.ClaudeCode:      newClaudeCodeAdapter(),
		core.Codex:           newCodexAdapter(),
		core.Grok:            newGrokAdapter(),
		core.Pi:              newPiAdapter(),
		core.Amp:             newAmpAdapter(),
		core.WorkBuddy:       newWorkBuddyAdapter(),
		core.WorkBuddyIntl:   newWorkBuddyIntlAdapter(),
		core.CodeBuddy:       newCodeBuddyAdapter(),
		core.Qoder:           newQoderAdapter(),
		core.QwenCode:        newQwenCodeAdapter(),
		core.Kimi:            newKimiAdapter(),
		core.Copilot:         newCopilotAdapter(),
		core.ZCode:           newZCodeAdapter(),
		core.OpenCode:        newOpenCodeAdapter(),
		core.GeminiCli:       newGeminiCliAdapter(),
		core.Droid:           newDroidAdapter(),
		core.Cline:           newClineAdapter(),
		core.RooCode:         newRooCodeAdapter(),
		core.KiloCode:        newKiloCodeAdapter(),
		core.DeepSeekHarness: newDeepSeekHarnessAdapter(),
		core.CommandCode:     newCommandCodeAdapter(),
		core.OpenClaw:        newOpenClawAdapter(),
		core.EveryCode:       newEveryCodeAdapter(),
	}
}
