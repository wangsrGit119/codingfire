package fire

import (
	"testing"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// Temporary diagnostic probe - delete after use.
func TestTmpProbeDailyBase(t *testing.T) {
	m := NewFireStateMachine()
	t.Logf("fresh:            phase=%v intensity=%.4f tier=%v", m.Snapshot().Phase, m.Snapshot().Intensity, m.Snapshot().Tier)

	m.UpdateTodayTokens(22914176, map[core.UsageSource]int{})
	s := m.Snapshot()
	t.Logf("after 22.9M today: phase=%v intensity=%.4f tier=%v ember=%.4f fuel=%.4f",
		s.Phase, s.Intensity, s.Tier, s.EmberHeat, s.Fuel)

	for i := 0; i < 5; i++ {
		m.Tick()
	}
	s = m.Snapshot()
	t.Logf("after 5 ticks:    phase=%v intensity=%.4f tier=%v", s.Phase, s.Intensity, s.Tier)

	t.Logf("dailyBaseIntensity() = %.4f", m.dailyBaseIntensity())
	t.Logf("tuning: start=%v half=%v max=%v",
		m.Tuning().DailyBaseStartTokens, m.Tuning().DailyBaseHalfTokens, m.Tuning().DailyBaseMaxIntensity)
}
