// Package data mirrors the C# CodingFire.Data namespace, plus the local usage
// store that the C# version kept in CodingFire.Core.
//
// Everything here is strictly read-only with respect to the tools it observes:
// local log files are parsed, no SQL is executed against a tool's database,
// and nothing is ever uploaded.
package data

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// epoch is the Unix epoch, used for the compact on-disk timestamp form.
var epoch = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

// retainDays is how much history survives a restart. Older events are dropped
// so memory cannot grow without bound.
const retainDays = 45

// maxRetainedAppendBytes bounds how much unflushed append data we hold in
// memory when the append keeps failing.
const maxRetainedAppendBytes = 4 * 1024 * 1024

// cursorState is a per-file read position plus any partial trailing line.
type cursorState struct {
	Offset  int64
	Partial string
	HasPart bool
}

// TodayStats is a snapshot of the current day's totals.
type TodayStats struct {
	Total    int
	BySource map[core.UsageSource]int
}

// UsageStore is the local event store.
//
// The macOS version used SQLite. This uses an append-only NDJSON file plus an
// in-memory index, which keeps the binary free of any native sqlite3
// dependency:
//   - inserts are idempotent by id, equivalent to INSERT OR IGNORE
//   - only token counts and file paths are stored — never prompts, code, or
//     credentials
type UsageStore struct {
	mu       sync.Mutex
	events   []core.UsageEvent
	knownIDs map[string]struct{}

	// knownPaths records which log files have ever produced an event.
	//
	// JsonlReader asks "has this file produced anything?" once per un-grown
	// file per scan. That used to be a linear scan over every event — files ×
	// events — which slowed whole scans (and therefore the fire's reaction
	// time) for heavy users. This makes it an O(1) set lookup.
	knownPaths map[string]struct{}

	cursors      map[string]cursorState
	cursorsDirty bool
	meta         map[string]string

	// ---- incremental accumulators for today's stats ----
	// TodayTotals/TodayHourlyTotals/TodayBreakdown used to each scan the whole
	// store, three times per scan on the UI thread. They now accumulate on
	// insert and queries just take a snapshot; only a day rollover, a rescan,
	// or an event removal triggers a full recompute.
	statsDay      time.Time
	statsDaySet   bool
	statTotal     int
	statBySource  map[core.UsageSource]int
	statHourly    [24]int
	statIn        int
	statOut       int
	statCacheRead int
	statCacheWrit int

	appendBuf      strings.Builder
	appendFile     *os.File
	pendingWrites  int
	appendFailures int
}

// NewUsageStore returns an empty, unopened store.
func NewUsageStore() *UsageStore {
	return &UsageStore{
		knownIDs:     map[string]struct{}{},
		knownPaths:   map[string]struct{}{},
		cursors:      map[string]cursorState{},
		meta:         map[string]string{},
		statBySource: map[core.UsageSource]int{},
	}
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Open loads events, cursors and meta from disk.
func (s *UsageStore) Open() {
	s.mu.Lock()
	defer s.mu.Unlock()

	core.AppPaths.CleanupStaleTempFiles()
	s.loadEventsLocked()
	s.loadCursorsLocked()
	s.loadMetaLocked()
}

func (s *UsageStore) loadEventsLocked() {
	path := core.AppPaths.UsageFile()
	f, err := os.Open(path)
	if err != nil {
		s.recomputeStatsLocked(time.Now())
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retainDays)
	total, kept := 0, 0
	needsCompact := false

	// Decode as we read: retaining every raw line alongside the event index
	// doubled the startup footprint for large histories.
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		total++
		e, ok := decodeLine(line)
		if !ok {
			needsCompact = true
			continue
		}
		if e.Timestamp.Before(cutoff) {
			needsCompact = true
			continue
		}
		kept++
		s.addInMemoryLocked(e)
	}
	readErr := sc.Err()
	_ = f.Close() // close before compaction replaces the file on Windows
	if readErr != nil {
		core.LogWarn("could not read complete usage history: " + readErr.Error())
	}

	core.LogInfo(fmt.Sprintf("store loaded: %d/%d events", kept, total))
	s.recomputeStatsLocked(time.Now())
	if needsCompact && readErr == nil {
		s.rewriteFileLocked()
	}
}

func (s *UsageStore) loadCursorsLocked() {
	data, err := os.ReadFile(core.AppPaths.CursorsFile())
	if err != nil {
		return
	}
	root := core.Of(core.ParseJSON(string(data)))
	for _, key := range root.Keys() {
		o := root.Obj(key)
		off, _ := o.Long("o")
		p, hasP := o.StrOk("p")
		s.cursors[key] = cursorState{Offset: off, Partial: p, HasPart: hasP}
	}
}

func (s *UsageStore) loadMetaLocked() {
	data, err := os.ReadFile(core.AppPaths.MetaFile())
	if err != nil {
		return
	}
	root := core.Of(core.ParseJSON(string(data)))
	for _, key := range root.Keys() {
		if v, ok := root.StrOk(key); ok {
			s.meta[key] = v
		}
	}
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

// InsertEvent adds an event. It returns false when the id was already known,
// which is the equivalent of INSERT OR IGNORE reporting changes==0.
func (s *UsageStore) InsertEvent(e core.UsageEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e.ID == "" {
		return false
	}
	if _, dup := s.knownIDs[e.ID]; dup {
		return false
	}
	s.knownIDs[e.ID] = struct{}{}
	s.events = append(s.events, e)
	if e.FilePath != "" {
		s.knownPaths[e.FilePath] = struct{}{}
	}
	s.ensureStatsDayLocked(time.Now())
	s.accumulateLocked(e, s.statsDay.AddDate(0, 0, 1))

	s.appendBuf.WriteString(encodeLine(e))
	s.appendBuf.WriteByte('\n')
	s.pendingWrites++
	if s.pendingWrites >= 64 || s.appendBuf.Len() >= 128*1024 {
		s.flushAppendsLocked()
	}
	return true
}

// Flush writes any buffered appends to disk. Called at the end of each scan and
// on shutdown.
func (s *UsageStore) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushAppendsLocked()
}

func (s *UsageStore) flushAppendsLocked() {
	if s.appendBuf.Len() == 0 {
		return
	}
	if s.appendFile == nil {
		f, err := os.OpenFile(core.AppPaths.UsageFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			s.appendFailedLocked(err)
			return
		}
		s.appendFile = f
	}
	if _, err := s.appendFile.WriteString(s.appendBuf.String()); err != nil {
		s.appendFailedLocked(err)
		return
	}
	s.appendBuf.Reset()
	s.pendingWrites = 0
	s.appendFailures = 0
}

func (s *UsageStore) appendFailedLocked(err error) {
	// The buffer is retained and retried on every later insert, which would
	// flood the log while the file stays locked — throttle it.
	s.appendFailures++
	if s.appendFailures == 1 || s.appendFailures%200 == 0 {
		core.LogWarn(fmt.Sprintf("append failed (%dx, %d event(s) buffered): %v",
			s.appendFailures, s.pendingWrites, err))
	}
	s.closeWriterLocked()

	// The buffer must NOT be dropped. The read cursors already advanced, so
	// these events are gone forever if lost (a rescan cannot recover them).
	// Keep them for the next flush attempt; but if the file stays locked
	// indefinitely, cap the growth and say so explicitly.
	if s.appendBuf.Len() > maxRetainedAppendBytes {
		core.LogWarn(fmt.Sprintf(
			"giving up on %d buffered event(s): append kept failing and the buffer hit %d KB",
			s.pendingWrites, maxRetainedAppendBytes/1024))
		s.appendBuf.Reset()
		s.pendingWrites = 0
	}
}

func (s *UsageStore) closeWriterLocked() {
	if s.appendFile == nil {
		return
	}
	_ = s.appendFile.Close()
	s.appendFile = nil
}

// Close flushes and releases file handles.
func (s *UsageStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.flushAppendsLocked()
	s.closeWriterLocked()
	if s.cursorsDirty {
		s.persistCursorsLocked()
		s.cursorsDirty = false
	}
}

// HasEvent reports whether an id is already stored.
func (s *UsageStore) HasEvent(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.knownIDs[id]
	return ok
}

// EventIDsWithPrefix returns every known id starting with prefix. Equivalent to
// the original eventIDs(withPrefix:), but in one pass instead of N queries.
func (s *UsageStore) EventIDsWithPrefix(prefix string) map[string]struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[string]struct{})
	for id := range s.knownIDs {
		if strings.HasPrefix(id, prefix) {
			out[id] = struct{}{}
		}
	}
	return out
}

// HasEventsForFilePath reports whether a log file has ever produced an event.
func (s *UsageStore) HasEventsForFilePath(path string) bool {
	if path == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.knownPaths[path]
	return ok
}

// TodayTotals returns today's total and per-source split.
func (s *UsageStore) TodayTotals(now time.Time) TodayStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureStatsDayLocked(now)
	out := TodayStats{Total: s.statTotal, BySource: make(map[core.UsageSource]int, len(s.statBySource))}
	for k, v := range s.statBySource {
		out.BySource[k] = v
	}
	return out
}

// TodayHourlyTotals returns all 24 local-time buckets, empty hours included.
func (s *UsageStore) TodayHourlyTotals(now time.Time) []core.HourlyUsage {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureStatsDayLocked(now)
	out := make([]core.HourlyUsage, 24)
	for h := 0; h < 24; h++ {
		out[h] = core.HourlyUsage{Hour: h, Tokens: s.statHourly[h]}
	}
	return out
}

// TodayBreakdown returns today's per-class token split.
func (s *UsageStore) TodayBreakdown(now time.Time) core.UsageBreakdown {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ensureStatsDayLocked(now)
	return core.UsageBreakdown{
		Input:      core.IntPtr(s.statIn),
		Output:     core.IntPtr(s.statOut),
		CacheRead:  core.IntPtr(s.statCacheRead),
		CacheWrite: core.IntPtr(s.statCacheWrit),
	}
}

// ---------------------------------------------------------------------------
// Incremental today-stats maintenance
// ---------------------------------------------------------------------------

// ensureStatsDayLocked recomputes everything when the cached day is not the day
// the caller asked for (a rollover, or an explicit query for another day).
func (s *UsageStore) ensureStatsDayLocked(wanted time.Time) {
	d := time.Date(wanted.Year(), wanted.Month(), wanted.Day(), 0, 0, 0, 0, time.Local)
	if !s.statsDaySet || !s.statsDay.Equal(d) {
		s.recomputeStatsLocked(wanted)
	}
}

func (s *UsageStore) recomputeStatsLocked(day time.Time) {
	s.statsDay = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	s.statsDaySet = true
	s.statTotal = 0
	s.statBySource = map[core.UsageSource]int{}
	s.statHourly = [24]int{}
	s.statIn, s.statOut, s.statCacheRead, s.statCacheWrit = 0, 0, 0, 0

	end := s.statsDay.AddDate(0, 0, 1)
	for _, e := range s.events {
		s.accumulateLocked(e, end)
	}
}

// accumulateLocked folds one event into the current stats day, skipping events
// outside it in O(1).
func (s *UsageStore) accumulateLocked(e core.UsageEvent, end time.Time) {
	if e.Timestamp.Before(s.statsDay) || !e.Timestamp.Before(end) {
		return
	}
	s.statTotal += e.Tokens
	s.statBySource[e.Source] += e.Tokens
	if h := e.Timestamp.Hour(); h >= 0 && h < 24 {
		s.statHourly[h] += e.Tokens
	}
	s.statIn += derefInt(e.Breakdown.Input)
	s.statOut += derefInt(e.Breakdown.Output)
	s.statCacheRead += derefInt(e.Breakdown.CacheRead)
	s.statCacheWrit += derefInt(e.Breakdown.CacheWrite)
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// RecentEvents returns events at or after since, in ascending time order,
// capped at limit when limit > 0.
func (s *UsageStore) RecentEvents(since time.Time, limit int) []core.UsageEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Most queries cover minutes, not all 45 days of retained history.
	var out []core.UsageEvent
	for _, e := range s.events {
		if e.Timestamp.Before(since) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// LatestEvent returns the most recent event, and false when the store is empty.
func (s *UsageStore) LatestEvent() (core.UsageEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.events) == 0 {
		return core.UsageEvent{}, false
	}
	best := s.events[0]
	for _, e := range s.events[1:] {
		if e.Timestamp.After(best.Timestamp) {
			best = e
		}
	}
	return best, true
}

// ---------------------------------------------------------------------------
// File cursors
// ---------------------------------------------------------------------------

// FileCursor returns the stored read position and any partial trailing line.
func (s *UsageStore) FileCursor(path string) (offset int64, partial string, hasPartial bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c, ok := s.cursors[path]; ok {
		return c.Offset, c.Partial, c.HasPart
	}
	return 0, "", false
}

// SetFileCursor stores a read position. Unchanged values do not mark the store
// dirty.
//
// Scans are now driven by file events, so every log file listed in a pass comes
// through here and the vast majority did not grow at all. Rewriting the whole
// cursors.json per file (temp file + write + delete + rename) was pure disk
// churn and lengthened each scan.
func (s *UsageStore) SetFileCursor(path string, offset int64, partial string, hasPartial bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cur, ok := s.cursors[path]; ok &&
		cur.Offset == offset &&
		cur.Partial == partial &&
		cur.HasPart == hasPartial {
		return
	}
	s.cursors[path] = cursorState{Offset: offset, Partial: partial, HasPart: hasPartial}
	s.cursorsDirty = true
}

// FlushCursors persists cursors once, at the end of a scan.
//
// The ordering is deliberate: this runs after events have been flushed, so a
// crash costs at most a re-read of some log tail (idempotent by id, so nothing
// double-counts) rather than losing events that never reached disk.
func (s *UsageStore) FlushCursors() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.cursorsDirty {
		return
	}
	s.persistCursorsLocked()
	s.cursorsDirty = false
}

// ClearFileCursors forgets every read position, forcing a full re-read.
func (s *UsageStore) ClearFileCursors() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cursors = map[string]cursorState{}
	s.persistCursorsLocked()
	s.cursorsDirty = false
}

// ---------------------------------------------------------------------------
// Meta
// ---------------------------------------------------------------------------

// Meta reads a meta value.
func (s *UsageStore) Meta(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.meta[key]
	return v, ok
}

// SetMeta writes a meta value and persists immediately.
func (s *UsageStore) SetMeta(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta[key] = value
	s.persistMetaLocked()
}

// ---------------------------------------------------------------------------
// Cursor estimate cleanup (same semantics as the macOS version)
// ---------------------------------------------------------------------------

// PurgeCursorBubbleV1 removes pre-v2 Cursor estimate events.
func (s *UsageStore) PurgeCursorBubbleV1() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeWhereLocked(func(id string) bool {
		return strings.HasPrefix(id, "cursor:bubble:") && !strings.HasPrefix(id, "cursor:bubble:v2:")
	})
}

// PurgeCursorLocalEstimates removes every Cursor estimate event.
func (s *UsageStore) PurgeCursorLocalEstimates() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeWhereLocked(func(id string) bool {
		return strings.HasPrefix(id, "cursor:bubble:")
	})
}

func (s *UsageStore) removeWhereLocked(match func(string) bool) {
	kept := make([]core.UsageEvent, 0, len(s.events))
	removed := 0
	for _, e := range s.events {
		if match(e.ID) {
			delete(s.knownIDs, e.ID)
			removed++
			continue
		}
		kept = append(kept, e)
	}
	if removed == 0 {
		return
	}
	s.events = kept
	s.rebuildPathIndexLocked()
	if !s.statsDaySet {
		s.recomputeStatsLocked(time.Now())
	} else {
		s.recomputeStatsLocked(s.statsDay)
	}
	s.rewriteFileLocked()
}

// rebuildPathIndexLocked rebuilds the path index after a bulk change to the
// event set (only the removal paths reach here).
func (s *UsageStore) rebuildPathIndexLocked() {
	s.knownPaths = make(map[string]struct{}, len(s.events))
	for _, e := range s.events {
		if e.FilePath != "" {
			s.knownPaths[e.FilePath] = struct{}{}
		}
	}
}

// ---------------------------------------------------------------------------
// In-memory and on-disk representation
// ---------------------------------------------------------------------------

func (s *UsageStore) addInMemoryLocked(e core.UsageEvent) {
	s.knownIDs[e.ID] = struct{}{}
	if e.FilePath != "" {
		s.knownPaths[e.FilePath] = struct{}{}
	}
	s.events = append(s.events, e)
}

// encodeLine serialises an event into the compact on-disk form.
func encodeLine(e core.UsageEvent) string {
	var b strings.Builder
	b.WriteString(`{"i":`)
	b.WriteString(core.QuoteString(e.ID))
	b.WriteString(`,"s":`)
	b.WriteString(core.QuoteString(e.Source.Raw()))
	b.WriteString(`,"t":`)
	b.WriteString(formatEpochSeconds(e.Timestamp))
	b.WriteString(`,"k":`)
	b.WriteString(strconv.Itoa(e.Tokens))
	if e.Breakdown.Input != nil {
		fmt.Fprintf(&b, `,"in":%d`, *e.Breakdown.Input)
	}
	if e.Breakdown.Output != nil {
		fmt.Fprintf(&b, `,"out":%d`, *e.Breakdown.Output)
	}
	if e.Breakdown.CacheRead != nil {
		fmt.Fprintf(&b, `,"cr":%d`, *e.Breakdown.CacheRead)
	}
	if e.Breakdown.CacheWrite != nil {
		fmt.Fprintf(&b, `,"cw":%d`, *e.Breakdown.CacheWrite)
	}
	if e.FilePath != "" {
		b.WriteString(`,"f":`)
		b.WriteString(core.QuoteString(e.FilePath))
	}
	if e.IsEstimated {
		b.WriteString(`,"e":1`)
	}
	b.WriteByte('}')
	return b.String()
}

func formatEpochSeconds(t time.Time) string {
	return strconv.FormatFloat(t.UTC().Sub(epoch).Seconds(), 'f', 3, 64)
}

// decodeLine parses the compact on-disk form. A malformed line reports false so
// the loader can schedule a compaction.
func decodeLine(line string) (core.UsageEvent, bool) {
	raw := core.ParseJSON(line)
	if raw == nil {
		return core.UsageEvent{}, false
	}
	o := core.Of(raw)
	id := o.Str("i")
	if id == "" {
		return core.UsageEvent{}, false
	}
	src, ok := core.UsageSourceFromRaw(o.Str("s"))
	if !ok {
		return core.UsageEvent{}, false
	}
	secs, _ := o.Num("t")

	var in, out, cr, cw *int
	if v, ok := o.Int("in"); ok {
		in = &v
	}
	if v, ok := o.Int("out"); ok {
		out = &v
	}
	if v, ok := o.Int("cr"); ok {
		cr = &v
	}
	if v, ok := o.Int("cw"); ok {
		cw = &v
	}
	tok, _ := o.Int("k")
	est, _ := o.Int("e")

	return core.UsageEvent{
		ID:          id,
		Source:      src,
		Timestamp:   epoch.Add(time.Duration(secs * float64(time.Second))).Local(),
		Tokens:      tok,
		Breakdown:   core.UsageBreakdown{Input: in, Output: out, CacheRead: cr, CacheWrite: cw},
		FilePath:    o.Str("f"),
		IsEstimated: est == 1,
	}, true
}

// rewriteFileLocked compacts the store to disk in time order.
func (s *UsageStore) rewriteFileLocked() {
	// Flush and drop the handle first, or events are lost and the file stays
	// locked against the rewrite.
	s.flushAppendsLocked()
	s.closeWriterLocked()

	path := core.AppPaths.UsageFile()
	tmp := path + ".tmp"

	sort.SliceStable(s.events, func(i, j int) bool {
		return s.events[i].Timestamp.Before(s.events[j].Timestamp)
	})

	var b strings.Builder
	for _, e := range s.events {
		b.WriteString(encodeLine(e))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		core.LogWarn("compact failed: " + err.Error())
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		core.LogWarn("compact failed: " + err.Error())
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		core.LogWarn("compact failed: " + err.Error())
	}
}

func (s *UsageStore) persistCursorsLocked() {
	var b strings.Builder
	b.WriteString("{\n")
	first := true
	// Sorted so the file is stable across runs.
	keys := make([]string, 0, len(s.cursors))
	for k := range s.cursors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := s.cursors[k]
		if !first {
			b.WriteString(",\n")
		}
		first = false
		b.WriteString("  ")
		b.WriteString(core.QuoteString(k))
		fmt.Fprintf(&b, `: {"o":%d`, c.Offset)
		if c.HasPart && c.Partial != "" {
			b.WriteString(`,"p":`)
			b.WriteString(core.QuoteString(c.Partial))
		}
		b.WriteString("}")
	}
	b.WriteString("\n}\n")
	core.AppPaths.WriteAtomicFile(core.AppPaths.CursorsFile(), b.String())
}

func (s *UsageStore) persistMetaLocked() {
	var b strings.Builder
	b.WriteString("{\n")
	first := true
	keys := make([]string, 0, len(s.meta))
	for k := range s.meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !first {
			b.WriteString(",\n")
		}
		first = false
		b.WriteString("  ")
		b.WriteString(core.QuoteString(k))
		b.WriteString(": ")
		b.WriteString(core.QuoteString(s.meta[k]))
	}
	b.WriteString("\n}\n")
	core.AppPaths.WriteAtomicFile(core.AppPaths.MetaFile(), b.String())
}

// EncodePartial / DecodePartial expose the cursor carry encoding used by
// JsonlReader.
func EncodePartial(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

// DecodePartial reverses EncodePartial.
func DecodePartial(s string) []byte {
	if s == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}
