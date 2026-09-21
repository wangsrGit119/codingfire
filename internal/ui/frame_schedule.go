package ui

import (
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// frameSchedule leaves the 20 Hz state/interaction timer independent of GPU
// work: flame 10 fps, embers 4 fps, cold fire once, hidden fire never.
type frameSchedule struct {
	lastTime  time.Time
	lastPhase core.FirePhase
	hasFrame  bool
}

func (s *frameSchedule) due(now time.Time, phase core.FirePhase, visible bool) bool {
	if !visible {
		s.hasFrame = false
		return false
	}
	if s.hasFrame && phase == s.lastPhase {
		interval := 100 * time.Millisecond
		switch phase {
		case core.PhaseUnlit, core.PhaseOut:
			return false
		case core.PhaseEmber:
			interval = 250 * time.Millisecond
		}
		if now.Sub(s.lastTime) < interval {
			return false
		}
	}
	s.lastTime, s.lastPhase, s.hasFrame = now, phase, true
	return true
}
