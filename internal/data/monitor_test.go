package data

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

type countingAdapter struct{ scans atomic.Int32 }

func (*countingAdapter) WatchRoots() []string { return nil }
func (*countingAdapter) CheckConnection() (core.SourceConnectionState, string) {
	return core.SourceOk, "test"
}
func (a *countingAdapter) DiscoverLogFiles(core.Time) []string { a.scans.Add(1); return nil }
func (*countingAdapter) ParseThread(string) []core.UsageEvent  { return nil }
func (*countingAdapter) ParseLine(string, string) (core.UsageEvent, bool) {
	return core.UsageEvent{}, false
}
func (*countingAdapter) ParseDatabase() []core.UsageEvent { return nil }
func (*countingAdapter) IsCumulative() bool               { return false }

func TestScanScheduleAndStop(t *testing.T) {
	t.Setenv("CODINGFIRE_DATA_DIR", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		a := &countingAdapter{}
		m := NewUsageMonitor(NewUsageStore())
		m.adapters = map[core.UsageSource]LogAdapter{core.UsageSourcesAll[0]: a}
		defer m.Stop()
		go m.scheduleScans()
		synctest.Wait()
		check := func(want int32) {
			t.Helper()
			synctest.Wait()
			if got := a.scans.Load(); got != want {
				t.Fatalf("scans = %d, want %d", got, want)
			}
		}
		time.Sleep(time.Millisecond)
		check(0) // a bare 4000 duration used to request a 4 us timer
		time.Sleep(249 * time.Millisecond)
		check(1) // baseline
		time.Sleep(3749 * time.Millisecond)
		check(1) // no repeated scans between baseline and heartbeat
		time.Sleep(time.Millisecond)
		check(2)             // exactly four seconds from startup
		m.EnqueueScan(false) // watcher notifications still scan immediately
		check(3)
		time.Sleep(4 * time.Second)
		check(4)
		m.Stop()
		m.EnqueueScan(true)
		time.Sleep(8 * time.Second)
		check(4)
	})
}
