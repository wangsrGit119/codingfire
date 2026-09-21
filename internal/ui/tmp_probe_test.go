package ui

import (
	"testing"

	"github.com/wangsrGit119/codingfire/internal/core"
	"github.com/wangsrGit119/codingfire/internal/data"
	"github.com/wangsrGit119/codingfire/internal/fire"
)

// Temporary diagnostic probe - delete after use.
// Replicates the exact wiring App.NewApp() installs, minus the GUI.
func TestTmpProbeStartupPath(t *testing.T) {
	store := data.NewUsageStore()
	store.Open()

	monitor := data.NewUsageMonitor(store)

	sm := fire.NewFireStateMachine()
	monitor.TodayTokensChanged = func(total int, bySource map[core.UsageSource]int) {
		t.Logf("  TodayTokensChanged fired: total=%d", total)
		sm.UpdateTodayTokens(total, bySource)
	}
	monitor.Ingest = func(tokens float64, source *core.UsageSource, at core.Time, animate bool) {
		sm.Ingest(tokens, source, at, animate)
	}

	monitor.Start()

	s := sm.Snapshot()
	t.Logf("after Start: TodayTokens=%d phase=%v intensity=%.4f tier=%v",
		sm.TodayTokens, s.Phase, s.Intensity, s.Tier)
	t.Logf("phase==PhaseFlame? %v   (PhaseFlame=%d, PhaseEmber=%d)",
		s.Phase == core.PhaseFlame, core.PhaseFlame, core.PhaseEmber)

	store.Close()
}
