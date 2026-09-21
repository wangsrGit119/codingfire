package ui

// Diagnostic probes for cmd/ctprobe, which renders console tabs headlessly so
// the layout can be inspected and the samples under perf-artifacts/
// regenerated. Not part of the shipped binary.

import (
	"time"

	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend/soft"
)

// ProbeConsolePNG renders one console tab headlessly so the layout can be
// inspected without a screen.
func ProbeConsolePNG(tab int, path string, scale float32) error {
	a := NewApp()
	a.Monitor.Start()
	deadline := time.Now().Add(30 * time.Second)
	for a.Monitor.IsScanning() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond)

	if tab < 0 || tab >= len(consoleTabIDs) {
		tab = ConsoleTabStats
	}

	w := gui.NewWindow(gui.WindowCfg{
		State:  &consoleState{App: a, Tab: consoleTabIDs[tab]},
		Title:  "console probe",
		Width:  820,
		Height: 760,
		OnInit: func(w *gui.Window) { w.SetView(consoleView) },
	})
	return soft.RenderToPNG(w, scale, path)
}

// ProbeStatsInputs summarises what the stats tab is about to draw.
func ProbeStatsInputs() string {
	a := NewApp()
	a.Monitor.Start()
	deadline := time.Now().Add(30 * time.Second)
	for a.Monitor.IsScanning() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond)

	h := a.Monitor.TodayHourly()
	nonzero := 0
	for _, v := range h {
		if v.Tokens > 0 {
			nonzero++
		}
	}
	b := a.Monitor.TodayBreakdown()
	return "total=" + itoaInt(a.Monitor.TodayTokens()) +
		" hours=" + itoaInt(len(h)) + " nonZeroHours=" + itoaInt(nonzero) +
		" hourMax=" + itoaInt(hourMax(h)) + " peak=" + itoaInt(peakHour(h)) +
		" input=" + itoaInt(derefInt(b.Input)) + " output=" + itoaInt(derefInt(b.Output)) +
		" cacheRead=" + itoaInt(derefInt(b.CacheRead)) + " cacheWrite=" + itoaInt(derefInt(b.CacheWrite)) +
		" sources=" + itoaInt(len(a.Monitor.TodayBySource()))
}
