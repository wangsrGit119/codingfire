package data

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// LogAdapter reads one tool's local usage logs. Every implementation is
// strictly read-only.
type LogAdapter interface {
	// WatchRoots returns the paths whose changes should trigger a scan. A path
	// may be a directory or a single file.
	WatchRoots() []string

	// CheckConnection probes whether the tool's logs are present and readable.
	// detail is a short human-facing path or reason for the console.
	CheckConnection() (core.SourceConnectionState, string)

	// DiscoverLogFiles lists candidate log files modified at or after since.
	DiscoverLogFiles(since core.Time) []string

	// ParseThread parses a whole-file format (session archives, ui_messages.json,
	// thread JSON). It returns nil when the file is not that format, and the
	// caller then falls back to incremental line-by-line reading.
	ParseThread(path string) []core.UsageEvent

	// ParseLine parses one JSONL line.
	ParseLine(line, path string) (core.UsageEvent, bool)

	// ParseDatabase reads usage that lives in a SQLite database, which has no
	// log file to enumerate. Returns nil when the source has no database.
	ParseDatabase() []core.UsageEvent

	// IsCumulative reports whether this source rewrites a running total in
	// place, in which case only the delta since the last snapshot is new usage.
	IsCumulative() bool
}

const (
	// statusProbeSeconds throttles the connection probe, which has to list
	// directories and does not need to run every scan.
	statusProbeSeconds = 20

	// heartbeatMs is the fallback scan period. It can be relaxed once the file
	// watcher is up; when watching failed it is the only mechanism left.
	heartbeatMs = 4000

	// slowScanMs logs a warning above this. Under event-driven scanning a pass
	// may run several times a second (an idle pass measures ~35ms), so 300ms
	// only fires when something is genuinely wrong.
	slowScanMs = 300
)

// warmWindow is how much recent usage counts as "still burning", so a restart
// does not put the fire out instantly.
const warmWindow = 4 * time.Minute

// UsageMonitor schedules scans: every 4 seconds it incrementally reads each
// source's logs, deduplicates, stores, feeds the fire and refreshes stats.
type UsageMonitor struct {
	store    *UsageStore
	adapters map[core.UsageSource]LogAdapter

	// bestTotals tracks, per source, the largest total seen for an event id.
	// A record whose token count grows as output streams in only contributes
	// the difference as new usage.
	bestTotals     map[core.UsageSource]map[string]int
	bestTotalsGate sync.Mutex

	timer   *time.Timer
	watcher *LogWatcher

	// post marshals a callback onto the UI goroutine. The UI layer sets it;
	// when nil, callbacks run inline.
	post func(func())

	scanning atomic.Bool
	// scanInFlight / scanAgain coalesce scan requests. An int (not a bool) so
	// the read-and-clear is a single atomic exchange.
	scanInFlight   atomic.Int32
	scanAgain      atomic.Int32
	baselineWanted atomic.Int32

	didCompleteBaseline bool

	lastStatusProbe time.Time
	statusGate      sync.Mutex

	// ---- injected by the app ----
	Ingest             func(tokens float64, source *core.UsageSource, at core.Time, animate bool)
	TodayTokensChanged func(total int, bySource map[core.UsageSource]int)
	Updated            func()
	EventsIngested     func(n int)

	statsGate      sync.Mutex
	statuses       []core.SourceStatus
	todayTokens    int
	todayBySource  map[core.UsageSource]int
	todayHourly    []core.HourlyUsage
	todayBreakdown core.UsageBreakdown
	lastEvent      core.UsageEvent
	hasLastEvent   bool
	hasAnySource   bool

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewUsageMonitor returns a monitor bound to a store.
func NewUsageMonitor(store *UsageStore) *UsageMonitor {
	return &UsageMonitor{
		store:         store,
		adapters:      DefaultAdapters(),
		bestTotals:    map[core.UsageSource]map[string]int{},
		todayBySource: map[core.UsageSource]int{},
		todayHourly:   emptyHourly(),
		stopCh:        make(chan struct{}),
	}
}

// SetPost installs the UI-goroutine marshaller.
func (m *UsageMonitor) SetPost(fn func(func())) { m.post = fn }

// Statuses returns a copy of the current source statuses.
func (m *UsageMonitor) Statuses() []core.SourceStatus {
	m.statsGate.Lock()
	defer m.statsGate.Unlock()
	return append([]core.SourceStatus(nil), m.statuses...)
}

// TodayTokens returns today's total.
func (m *UsageMonitor) TodayTokens() int {
	m.statsGate.Lock()
	defer m.statsGate.Unlock()
	return m.todayTokens
}

// TodayBySource returns today's per-source split.
func (m *UsageMonitor) TodayBySource() map[core.UsageSource]int {
	m.statsGate.Lock()
	defer m.statsGate.Unlock()
	out := make(map[core.UsageSource]int, len(m.todayBySource))
	for k, v := range m.todayBySource {
		out[k] = v
	}
	return out
}

// TodayHourly returns the 24 local-time buckets.
func (m *UsageMonitor) TodayHourly() []core.HourlyUsage {
	m.statsGate.Lock()
	defer m.statsGate.Unlock()
	return append([]core.HourlyUsage(nil), m.todayHourly...)
}

// TodayBreakdown returns today's per-class split.
func (m *UsageMonitor) TodayBreakdown() core.UsageBreakdown {
	m.statsGate.Lock()
	defer m.statsGate.Unlock()
	return m.todayBreakdown
}

// LastEvent returns the most recently seen event.
func (m *UsageMonitor) LastEvent() (core.UsageEvent, bool) {
	m.statsGate.Lock()
	defer m.statsGate.Unlock()
	return m.lastEvent, m.hasLastEvent
}

// IsScanning reports whether a scan is in flight.
func (m *UsageMonitor) IsScanning() bool { return m.scanning.Load() }

// HasAnySource reports whether at least one source probed as connected.
func (m *UsageMonitor) HasAnySource() bool {
	m.statsGate.Lock()
	defer m.statsGate.Unlock()
	return m.hasAnySource
}

// ---------------------------------------------------------------------------

// Start begins scanning. It returns after the initial baseline has been
// requested (the baseline itself runs on a background goroutine).
func (m *UsageMonitor) Start() {
	m.statsGate.Lock()
	m.statuses = buildStatuses()
	m.todayBySource = map[core.UsageSource]int{}
	m.todayHourly = emptyHourly()
	m.todayBreakdown = core.UsageBreakdown{}
	m.statsGate.Unlock()

	m.ReloadStats()
	// Source probing can touch many directories and SQLite headers. Do it off
	// the UI startup path so launching the tray does not briefly monopolise a
	// CPU core before the first window is responsive.
	go m.RefreshStatuses()
	m.WarmFromStore()

	// Make scanning driven by "a log file was actually written". The heartbeat
	// below covers the case where watching cannot be established (permissions,
	// directory not created yet) or its buffer overflows.
	m.watcher = NewLogWatcher(
		func() { m.EnqueueScan(false) },
		func() {
			core.LogWarn("log watcher buffer overflowed; forcing a full scan")
			m.EnqueueScan(false)
		})
	m.watcher.Sync(m.watchRoots())
	roots := m.watcher.WatchedRoots()
	note := ""
	if roots == 0 {
		note = " (falling back to the timer only)"
	}
	core.LogInfo(sprintf("log watcher: %d root(s) watched%s", roots, note))

	// Give the tray/window a short head start before the initial full scan.
	time.AfterFunc(250*time.Millisecond, func() {
		select {
		case <-m.stopCh:
			return
		default:
			m.EnqueueScan(true)
		}
	})

	m.timer = time.AfterFunc(heartbeatMs, m.heartbeat)
}

// Stop shuts the monitor down.
func (m *UsageMonitor) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
		if m.timer != nil {
			m.timer.Stop()
		}
		if m.watcher != nil {
			m.watcher.Close()
		}
	})
}

// heartbeat is the fallback scan. It also attaches watches for tools installed
// after startup: watches are lazy, so a directory that appeared later is only
// noticed on the next heartbeat.
func (m *UsageMonitor) heartbeat() {
	select {
	case <-m.stopCh:
		return
	default:
	}
	if m.watcher != nil {
		m.watcher.Sync(m.watchRoots())
	}
	m.EnqueueScan(false)

	m.timer = time.AfterFunc(heartbeatMs, m.heartbeat)
}

// watchRoots collects every adapter's watch paths.
func (m *UsageMonitor) watchRoots() []string {
	var out []string
	for _, a := range m.adapters {
		func() {
			// A single bad adapter must not lose every other watch point.
			defer func() { _ = recover() }()
			for _, r := range a.WatchRoots() {
				if r != "" {
					out = append(out, r)
				}
			}
		}()
	}
	return out
}

// Rescan forgets all cursors and snapshot totals, then does a full re-read.
func (m *UsageMonitor) Rescan() {
	m.didCompleteBaseline = false
	m.bestTotalsGate.Lock()
	m.bestTotals = map[core.UsageSource]map[string]int{}
	m.bestTotalsGate.Unlock()
	m.store.ClearFileCursors()
	m.ReloadStats()
	m.RefreshStatuses()
	m.EnqueueScan(true)
}

// RefreshStatuses probes every source synchronously. Only used at startup and
// on an explicit rescan — moments when the user is watching.
func (m *UsageMonitor) RefreshStatuses() {
	probes := m.collectProbes()
	m.applyStatuses(probes)
	m.statusGate.Lock()
	m.lastStatusProbe = time.Now()
	m.statusGate.Unlock()
}

type probe struct {
	source core.UsageSource
	state  core.SourceConnectionState
	detail string
}

// ProbeStatuses probes in the background, throttled to statusProbeSeconds.
// Listing 23 directories on the UI thread every 4 seconds used to stall every
// repaint.
func (m *UsageMonitor) ProbeStatuses(force bool) {
	m.statusGate.Lock()
	if !force && time.Since(m.lastStatusProbe).Seconds() < statusProbeSeconds {
		m.statusGate.Unlock()
		return
	}
	m.lastStatusProbe = time.Now()
	m.statusGate.Unlock()

	probes := m.collectProbes()
	m.postUI(func() { m.applyStatuses(probes) })
}

func (m *UsageMonitor) collectProbes() []probe {
	out := make([]probe, 0, len(m.adapters))
	for _, src := range core.UsageSourcesAll {
		a, ok := m.adapters[src]
		if !ok {
			continue
		}
		state, detail := func() (core.SourceConnectionState, string) {
			defer func() { _ = recover() }()
			return a.CheckConnection()
		}()
		out = append(out, probe{source: src, state: state, detail: detail})
	}
	return out
}

func (m *UsageMonitor) applyStatuses(probes []probe) {
	m.statsGate.Lock()
	defer m.statsGate.Unlock()

	prev := m.statuses
	list := make([]core.SourceStatus, 0, len(probes))
	okCount := 0

	for _, p := range probes {
		if p.state == core.SourceOk {
			okCount++
		}
		st := core.SourceStatus{
			Source:      p.source,
			State:       p.state,
			Detail:      p.detail,
			TodayTokens: m.todayBySource[p.source],
		}
		for _, old := range prev {
			if old.Source == p.source {
				st.LastReadAt = old.LastReadAt
				st.HasLastRead = old.HasLastRead
				break
			}
		}
		list = append(list, st)
	}
	m.statuses = list
	m.hasAnySource = okCount > 0
}

// bestTotalsFor returns the snapshot table for a source, resetting it when it
// grows unreasonably large.
func (m *UsageMonitor) bestTotalsFor(src core.UsageSource) map[string]int {
	mp, ok := m.bestTotals[src]
	if !ok {
		mp = map[string]int{}
		m.bestTotals[src] = mp
	}
	if len(mp) > 200000 {
		clear(mp)
	}
	return mp
}

// ---------------------------------------------------------------------------
// Scanning
// ---------------------------------------------------------------------------

// EnqueueScan requests a scan. Requests are never dropped.
//
// The original `if (_scanning) return;` meant that one slow pass (many sources,
// busy disk, logs being written) silently lengthened the period and the fire's
// responsiveness collapsed — worse, the dropped pass had to wait for the next
// heartbeat. Now a "scan again" flag is recorded and the next pass starts the
// moment this one finishes.
func (m *UsageMonitor) EnqueueScan(baseline bool) {
	if baseline {
		m.baselineWanted.Store(1)
	}

	if !m.scanInFlight.CompareAndSwap(0, 1) {
		m.scanAgain.Store(1)
		return
	}

	fromStart := m.baselineWanted.Swap(0) == 1
	completeBaseline := m.didCompleteBaseline

	m.scanning.Store(true)
	m.raiseUpdated()

	fileSince := time.Now().Truncate(24 * time.Hour).Add(-12 * time.Hour)

	go func() {
		scanStart := time.Now()
		var collected []core.UsageEvent
		var touched []core.UsageSource

		for _, src := range core.UsageSourcesAll {
			adapter, ok := m.adapters[src]
			if !ok {
				continue
			}
			touched = append(touched, src)

			func() {
				// One broken source must not take down the whole scan.
				defer func() {
					if r := recover(); r != nil {
						core.LogWarn(src.Raw() + " scan failed: " + toString(r))
					}
				}()

				for _, f := range adapter.DiscoverLogFiles(fileSince) {
					// Whole-file formats (session archives, ui_messages.json,
					// thread JSON) are claimed by ParseThread; a nil result
					// means it is not that format, so fall back to incremental
					// line reading.
					if whole := adapter.ParseThread(f); whole != nil {
						collected = append(collected, whole...)
					} else {
						collected = append(collected, ReadNewJSONL(f, m.store, fromStart, adapter.ParseLine)...)
					}
				}

				// Some sources keep their data in SQLite with no log file to
				// enumerate.
				collected = append(collected, adapter.ParseDatabase()...)
			}()
		}

		// Stable sort: several records inside the same millisecond (Claude
		// streaming snapshots) must keep their original file order. Go's
		// sort.SliceStable gives that.
		sort.SliceStable(collected, func(i, j int) bool {
			return collected[i].Timestamp.Before(collected[j].Timestamp)
		})

		// Dedup, delta, and persist all happen on this goroutine — pure data
		// work that used to stall the UI thread with disk I/O every 4 seconds.
		accepted := func() []core.UsageEvent {
			defer func() {
				if r := recover(); r != nil {
					core.LogWarn("store apply failed: " + toString(r))
				}
			}()
			return m.applyAllToStore(collected)
		}()

		func() {
			defer func() { _ = recover() }()
			m.store.Flush()
		}()
		func() {
			defer func() { _ = recover() }()
			m.store.FlushCursors()
		}()
		func() {
			defer func() { _ = recover() }()
			m.ProbeStatuses(false)
		}()

		// Scans are event-driven now and may run several times a second while
		// busy, so a pass getting expensive turns straight into CPU cost.
		if ms := time.Since(scanStart).Milliseconds(); ms > slowScanMs {
			core.LogWarn(sprintf("slow scan: %d ms (%d new event(s))", ms, len(accepted)))
		}

		m.postUI(func() {
			func() {
				defer func() {
					if r := recover(); r != nil {
						core.LogWarn("finishScan failed: " + toString(r))
					}
				}()
				m.finishScan(accepted, touched, fromStart, completeBaseline)
			}()

			m.scanning.Store(false)
			m.raiseUpdated()
			m.scanInFlight.Store(0)
			// A request arrived while scanning (a log was just written) → run
			// another pass immediately instead of waiting for the heartbeat.
			if m.scanAgain.Swap(0) == 1 {
				m.EnqueueScan(false)
			}
		})
	}()
}

func (m *UsageMonitor) postUI(fn func()) {
	if m.post != nil {
		m.post(fn)
		return
	}
	fn()
}

func (m *UsageMonitor) finishScan(events []core.UsageEvent, touched []core.UsageSource, isBaseline, alreadyBaselined bool) {
	warmCutoff := time.Now().Add(-warmWindow)
	baselinePass := isBaseline || !alreadyBaselined

	// The events were already deduplicated and stored on the background
	// goroutine; only "feed the fire" has to stay on the UI goroutine.
	accepted := 0
	for _, e := range events {
		m.statsGate.Lock()
		m.lastEvent = e
		m.hasLastEvent = true
		m.statsGate.Unlock()

		if e.Tokens <= 0 {
			continue
		}
		if baselinePass {
			// The first pass only counts the last few minutes so historical
			// data cannot produce a burst of fuel.
			if e.Timestamp.Before(warmCutoff) {
				accepted++
				continue
			}
			if m.Ingest != nil {
				src := e.Source
				m.Ingest(float64(e.Tokens), &src, e.Timestamp, false)
			}
		} else if m.Ingest != nil {
			src := e.Source
			m.Ingest(float64(e.Tokens), &src, e.Timestamp, true)
		}
		accepted++
	}

	if accepted > 0 && m.EventsIngested != nil {
		m.EventsIngested(accepted)
	}

	m.ReloadStats()
	if baselinePass {
		m.didCompleteBaseline = true
	}
	m.applyTouched(touched)
	m.raiseUpdated()
}

func (m *UsageMonitor) applyTouched(touched []core.UsageSource) {
	m.statsGate.Lock()
	defer m.statsGate.Unlock()

	for _, src := range touched {
		for i := range m.statuses {
			if m.statuses[i].Source != src {
				continue
			}
			m.statuses[i].LastReadAt = time.Now()
			m.statuses[i].HasLastRead = true
			m.statuses[i].TodayTokens = m.todayBySource[src]
		}
	}
}

// applyAllToStore deduplicates, converts cumulative snapshots to deltas, and
// persists. It returns the events actually stored, which the UI goroutine then
// feeds to the fire.
func (m *UsageMonitor) applyAllToStore(events []core.UsageEvent) []core.UsageEvent {
	accepted := make([]core.UsageEvent, 0, len(events))

	for _, e := range events {
		toStore := e.Clone()

		// Cumulative sources — Claude's streaming snapshots, and the running
		// totals OpenCode / ZCode / Gemini / Droid report — keep only the
		// richest snapshot and store the difference as the increment.
		//
		// After a restart the first total seen is stored under the original id,
		// which is already present and therefore idempotently dropped; only
		// subsequent growth produces a delta row suffixed with the total. That
		// is why a restart cannot double-count.
		if m.adapters[e.Source] != nil && m.adapters[e.Source].IsCumulative() {
			var previous int
			m.bestTotalsGate.Lock()
			best := m.bestTotalsFor(e.Source)
			previous = best[e.ID]
			if e.Tokens <= previous {
				m.bestTotalsGate.Unlock()
				continue
			}
			best[e.ID] = e.Tokens
			m.bestTotalsGate.Unlock()

			if previous == 0 {
				toStore.ID = e.ID
			} else {
				toStore.ID = e.ID + "#" + itoa(e.Tokens)
			}
			toStore.Tokens = e.Tokens - previous
		}

		if !m.store.InsertEvent(toStore) {
			continue
		}
		accepted = append(accepted, toStore)
	}
	return accepted
}

// ---------------------------------------------------------------------------

// ReloadStats refreshes the cached today figures from the store.
func (m *UsageMonitor) ReloadStats() {
	now := time.Now()
	totals := m.store.TodayTotals(now)
	hourly := m.store.TodayHourlyTotals(now)
	breakdown := m.store.TodayBreakdown(now)

	m.statsGate.Lock()
	m.todayTokens = totals.Total
	m.todayBySource = totals.BySource
	m.todayHourly = hourly
	m.todayBreakdown = breakdown
	m.statsGate.Unlock()

	if m.TodayTokensChanged != nil {
		m.TodayTokensChanged(totals.Total, totals.BySource)
	}
}

// WarmFromStore silently replays the last few minutes of usage so a restart
// does not start with a dead fire.
func (m *UsageMonitor) WarmFromStore() {
	cutoff := time.Now().Add(-warmWindow)
	for _, e := range m.store.RecentEvents(cutoff, 0) {
		if m.Ingest != nil {
			src := e.Source
			m.Ingest(float64(e.Tokens), &src, e.Timestamp, false)
		}
	}

	// Burned today but already cold → leave a little residual heat.
	last, ok := m.store.LatestEvent()
	if !ok {
		return
	}
	age := time.Since(last.Timestamp).Seconds()
	const horizon = 2 * 60 * 60
	if age >= 0 && age < horizon {
		remaining := maxF(0.12, 1-age/horizon)
		if m.Ingest != nil {
			src := last.Source
			m.Ingest(30000*remaining, &src, last.Timestamp, false)
		}
	}
}

func (m *UsageMonitor) raiseUpdated() {
	if m.Updated != nil {
		m.Updated()
	}
}

func emptyHourly() []core.HourlyUsage {
	out := make([]core.HourlyUsage, 24)
	for h := 0; h < 24; h++ {
		out[h] = core.HourlyUsage{Hour: h}
	}
	return out
}

func buildStatuses() []core.SourceStatus {
	out := make([]core.SourceStatus, 0, len(core.UsageSourcesAll))
	for _, s := range core.UsageSourcesAll {
		out = append(out, core.SourceStatus{
			Source: s,
			State:  core.SourceNotFound,
			Detail: "—",
		})
	}
	return out
}
