package fire

import (
	"testing"

	"github.com/wangsrGit119/codingfire/internal/core"
)

func BenchmarkCampfireRender(b *testing.B) {

	r := NewCampfireRenderer()
	r.Resize(172, 208)
	snap := core.FireSnapshot{Phase: core.PhaseFlame, Tier: core.TierRoar, Intensity: 0.72, EmberHeat: 0.7, FlameAccent: core.DefaultFlameAccent}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.Render(snap, core.SizeMedium.PixelScale(), false, float64(i+1)/10)
	}
}

func TestPaletteReusedUntilAccentChanges(t *testing.T) {
	r := NewCampfireRenderer()
	r.Resize(126, 204)
	snap := core.FireSnapshot{Phase: core.PhaseFlame, Tier: core.TierRoar, Intensity: 0.72, FlameAccent: core.DefaultFlameAccent}
	r.Render(snap, 3.5, false, 1)
	epoch := r.engine.AccentEpoch
	for i := 1; i <= 10; i++ {
		r.Render(snap, 3.5, false, 1+float64(i)/10)
	}
	if r.engine.AccentEpoch != epoch {
		t.Fatal("unchanged accent rebuilt the palette")
	}
	snap.FlameAccent = core.AccentRGB{0.2, 0.8, 0.4}
	r.Render(snap, 3.5, false, 3)
	if r.engine.AccentEpoch == epoch || r.engine.FlameAccent != snap.FlameAccent {
		t.Fatal("new accent was not applied")
	}
}
