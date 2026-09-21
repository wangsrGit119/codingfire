package ui

import (
	"testing"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

func TestFrameCadence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		phase   core.FirePhase
		visible bool
		want    int
	}{
		{"flame", core.PhaseFlame, true, 10},
		{"embers", core.PhaseEmber, true, 4},
		{"cold", core.PhaseOut, true, 1},
		{"unlit", core.PhaseUnlit, true, 1},
		{"hidden", core.PhaseFlame, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s frameSchedule
			count := 0
			for i := 0; i < 20; i++ {
				if s.due(time.Unix(0, 0).Add(time.Duration(i)*50*time.Millisecond), tc.phase, tc.visible) {
					count++
				}
			}
			if count != tc.want {
				t.Fatalf("frames = %d, want %d", count, tc.want)
			}
		})
	}
}

func TestFrameResumesImmediately(t *testing.T) {
	var s frameSchedule
	now := time.Now()
	if !s.due(now, core.PhaseOut, true) {
		t.Fatal("missing first frame")
	}
	if !s.due(now, core.PhaseFlame, true) {
		t.Fatal("ignition delayed")
	}
	if s.due(now, core.PhaseFlame, false) {
		t.Fatal("hidden frame")
	}
	if !s.due(now, core.PhaseFlame, true) {
		t.Fatal("show did not resume")
	}
	if !s.due(now, core.PhaseOut, true) {
		t.Fatal("extinction did not clear flame")
	}
}
