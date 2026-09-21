package ui

import (
	"math"
	"strconv"
	"time"

	"github.com/go-gui-org/go-glyph"
	"github.com/go-gui-org/go-gui/gui"

	"github.com/wangsrGit119/codingfire/internal/core"
	"github.com/wangsrGit119/codingfire/internal/fire"
)

// ---------------------------------------------------------------------------
// Console
// ---------------------------------------------------------------------------

// The console's tabs, in the order the tab control shows them.
//
// Stats leads because that is what the console is opened for: the C# build had
// a full BuildStatsTab that was never added to the tab control, so its numbers
// were unreachable. This ports that tab and puts it first.
const (
	ConsoleTabStats = iota
	ConsoleTabSources
	ConsoleTabSettings
	ConsoleTabAbout
)

// consoleTabIDs are the stable, language-neutral widget IDs behind the tab
// constants. The order must match the constants above.
var consoleTabIDs = []string{"stats", "sources", "settings", "about"}

// consoleState is the console window's per-window state.
type consoleState struct {
	App *App
	// Tab is the selected tab's ID, not its index: the tab control is a
	// controlled component and reports the ID it selected.
	Tab string
	// PickingCustom reveals the inline colour picker behind the "..." swatch.
	PickingCustom bool
}

// consoleTitle is the window title, which carries the version — the first thing
// anyone asks when they report a problem is which build they are on.
func consoleTitle() string {
	return core.T("console.title") + " — " + core.Version
}

// consoleOpenTimeout is how long a queued open is allowed to stay unfulfilled
// before the menu item is willing to try again.
//
// OpenWindow is asynchronous and reports nothing back: if the backend declines
// the window (a second GL surface that will not create, say) there is no error
// to observe. A bare "pending" flag would therefore latch on forever after one
// failure and the menu item would be dead for the rest of the session, with
// nothing in the log to say why. A timestamp can expire.
const consoleOpenTimeout = 3 * time.Second

// OpenConsole shows the console, or raises the open one and switches tab.
//
// tab is one of the ConsoleTab constants. Out-of-range values fall back to the
// first tab rather than panicking: this is reachable from a tray callback, and a
// tray item should never be able to take the app down.
func (a *App) OpenConsole(tab int) {
	if tab < 0 || tab >= len(consoleTabIDs) {
		tab = ConsoleTabStats
	}
	want := consoleTabIDs[tab]

	// Already open: re-select the tab and raise the window. QueueCommand runs
	// this on the UI thread, which matters because the caller is a tray
	// callback on a backend goroutine.
	if w := a.liveConsole(); w != nil {
		w.QueueCommand(func(w *gui.Window) {
			s := gui.State[consoleState](w)
			s.Tab = want
			w.InvalidateLayout()
		})
		SetWindowVisible(FindOwnWindowByTitle(consoleTitle()), true)
		a.raiseConsole()
		return
	}

	// OpenWindow is asynchronous, so a double-click on the tray icon would
	// otherwise open two consoles. The guard expires rather than latching, so a
	// request that never becomes a window does not disable the menu item.
	a.mu.Lock()
	if !a.consolePendingAt.IsZero() && time.Since(a.consolePendingAt) < consoleOpenTimeout {
		a.mu.Unlock()
		return
	}
	a.consolePendingAt = time.Now()
	a.mu.Unlock()

	a.gapp.OpenWindow(gui.WindowCfg{
		InitiallyHidden: a.probeHidden,
		Software:        a.bitmapOverlays,
		State:           &consoleState{App: a, Tab: want},
		Title:           consoleTitle(),
		IconPNG:         TrayIconArt.CachedPNG(32),
		Width:           820,
		// Tall enough for the stats tab's four sections to fit without
		// scrolling on a 1080p desktop; the tab content scrolls if the user
		// shrinks the window below this.
		Height:    760,
		MinWidth:  720,
		MinHeight: 620,
		OnInit: func(w *gui.Window) {
			a.mu.Lock()
			a.console = w
			a.consolePendingAt = time.Time{}
			a.mu.Unlock()
			w.SetView(consoleView)
			// The console is a normal window, so it has to be brought forward
			// by hand. go-gui creates it without activating it, and a window
			// that appears behind whatever the user was working in reads as
			// "the menu item did nothing".
			go func() {
				time.Sleep(300 * time.Millisecond)
				if !a.probeHidden {
					a.raiseConsole()
				}
			}()
		},
		OnCloseRequest: func(w *gui.Window) {
			// Closing the console does not quit — the tray keeps the app
			// alive, same as the C# build, which merely hid the form. Here
			// the window is really destroyed, so the App's handle has to be
			// dropped or the next OpenConsole would queue onto a dead window.
			SetWindowVisible(FindOwnWindowByTitle(consoleTitle()), false)
			a.mu.Lock()
			a.console = nil
			a.mu.Unlock()
			w.Close()
		},
	})
}

// liveConsole returns the console window if one is actually open.
//
// go-gui destroys windows without telling the app, so a cached handle can point
// at a window that no longer exists. Trusting it would send every later open
// request to a dead window — which is indistinguishable, from the outside, from
// the menu item being broken. The handle is validated against the native window
// list before it is believed.
func (a *App) liveConsole() *gui.Window {
	a.mu.Lock()
	w := a.console
	a.mu.Unlock()
	if w == nil {
		return nil
	}
	if FindOwnWindowByTitle(consoleTitle()) != 0 {
		return w
	}
	core.LogWarn("console window had gone away; reopening")
	a.mu.Lock()
	a.console = nil
	a.mu.Unlock()
	return nil
}

// raiseConsole brings the console to the front.
func (a *App) raiseConsole() {
	hwnd := FindOwnWindowByTitle(consoleTitle())
	if hwnd == 0 {
		return
	}
	if !ActivateWindow(hwnd) {
		// Not an error: Windows only grants foreground rights to a process that
		// already has them, and the window has still been raised.
		core.LogInfo("console could not take focus; raised instead")
	}
}

// refreshConsole asks the console to repaint, if it is open. Called on the
// ~900ms console cadence, matching the C# refresh timer.
func (a *App) refreshConsole() {
	if w := a.liveConsole(); w != nil {
		// Console rows are generated from live monitor snapshots. A render-only
		// refresh would repaint the old layout; regenerate the view so source
		// states and today's totals actually update.
		w.InvalidateLayout()
	}
}

// retitleConsole re-applies the localised title after a language switch.
func (a *App) retitleConsole() {
	if w := a.console; w != nil {
		w.QueueCommand(func(w *gui.Window) {
			w.SetTitle(consoleTitle())
			w.InvalidateLayout()
		})
	}
}

// consoleView builds the whole console.
func consoleView(w *gui.Window) gui.View {
	s := gui.State[consoleState](w)

	if s.Tab == "" {
		s.Tab = consoleTabIDs[ConsoleTabStats]
	}

	tabs := make([]gui.TabItemCfg, 0, len(consoleTabIDs))
	for i, id := range consoleTabIDs {
		tabs = append(tabs, gui.TabItemCfg{
			ID:      id,
			Label:   core.T("console.tab." + id),
			Content: consoleTabContent(i, s),
		})
	}

	return gui.Column(gui.ContainerCfg{
		ID:      "console.root",
		Sizing:  gui.FillFill,
		Padding: gui.PadAll(10),
		Content: []gui.View{
			gui.TabControl(gui.TabControlCfg{
				ID:       "console.tabs",
				Selected: s.Tab,
				Items:    tabs,
				OnSelect: func(id string, ctx gui.EventCtx) {
					gui.State[consoleState](ctx.Window).Tab = id
				},
			}),
		},
	})
}

// consoleTabContent dispatches to the per-tab builders. The index is used
// rather than the ID so an unknown ID cannot silently render an empty tab.
func consoleTabContent(tab int, s *consoleState) []gui.View {
	switch tab {
	case ConsoleTabStats:
		return statsTab(s)
	case ConsoleTabSettings:
		return settingsTab(s)
	case ConsoleTabAbout:
		return aboutTab(s)
	default:
		return sourcesTab(s)
	}
}

// ---------------------------------------------------------------------------
// Stats tab
// ---------------------------------------------------------------------------

// The stats tab's palette. It is the same warm dark glass as the hover card
// rather than the window theme, because these cards are CodingFire's own HUD and
// should look the same wherever the console is opened from.
var (
	statCardBG     = gui.RGBA(26, 23, 21, 242)
	statCardBorder = gui.RGBA(255, 184, 92, 46)
	statDim        = gui.RGBA(255, 255, 255, 150)
	statFaint      = gui.RGBA(255, 255, 255, 110)
	statBright     = gui.RGBA(255, 255, 255, 245)
	statAmber      = gui.RGBA(255, 180, 80, 255)
	statTrack      = gui.RGBA(255, 255, 255, 26)
)

// statClassColors are the C# UsageBars palette: one colour per token class, so
// the four bars stay distinguishable even when cache traffic dwarfs the rest.
var statClassColors = []gui.Color{
	gui.RGBA(255, 160, 60, 255),  // input — orange
	gui.RGBA(255, 200, 60, 255),  // output — yellow
	gui.RGBA(100, 160, 220, 255), // cache read — blue
	gui.RGBA(140, 140, 160, 255), // cache write — grey
}

// statTimelineGradient is the C# hourly chart's warm ramp: deep orange at the
// base up to a bright yellow cap, which is what gives a tall bar its glow.
func statTimelineGradient(dim bool) *gui.GradientDef {
	base, mid, cap := uint8(140), uint8(220), uint8(255)
	if dim {
		base, mid, cap = 110, 180, 210
	}
	return &gui.GradientDef{
		Direction: gui.GradientToTop,
		Stops: []gui.GradientStop{
			{Color: gui.RGBA(255, base, 50, 255), Pos: 0},
			{Color: gui.RGBA(255, mid, 100, 255), Pos: 0.88},
			{Color: gui.RGBA(255, cap, 200, 255), Pos: 1},
		},
	}
}

// Stats card geometry, in logical pixels. The bar widths are fixed rather than
// proportional because go-gui has no flex-with-max layout: the console is a
// fixed-width window, so a fixed track is the honest way to get an accurate
// bar length.
const (
	statCardPad   = 8
	statCardGap   = 5
	statLabelW    = 104
	statBarW      = 224
	statValueW    = 112
	statPercentW  = 58
	statNameW     = 146
	statBarH      = 9
	statBarRadius = 4
	hourChartH    = 72
	hourBarW      = 15
)

// statsTab renders today's usage: the headline card, the token-class breakdown,
// the per-source ranking and the hourly timeline.
func statsTab(s *consoleState) []gui.View {
	a := s.App
	theme := gui.CurrentTheme()

	total := a.Monitor.TodayTokens()
	breakdown := a.Monitor.TodayBreakdown()
	bySource := a.Monitor.TodayBySource()
	hourly := a.Monitor.TodayHourly()
	rolling := rollingUsage(a)

	left := []gui.View{
		statsSectionHeader(core.T("stats.overview"), theme),
		statsCard(rollingSummaryRows(rolling)),
		statsSectionHeader(core.T("stats.last7"), theme),
		statsCard(rollingDailyRows(rolling)),
		statsSectionHeader(core.T("stats.timeline"), theme),
		todayTimeline(hourly),
	}
	right := []gui.View{
		statsSectionHeader(core.T("stats.breakdown"), theme),
		statsCard(breakdownRows(breakdown, total)),
	}

	// The per-source and timeline sections are hidden rather than shown empty:
	// a list of zero-length bars says nothing that the empty-state line below
	// does not say better.
	if len(bySource) > 0 {
		right = append(right,
			statsSectionHeader(core.T("stats.bySource"), theme),
			statsCard(sourceBars(bySource)),
		)
	}

	// Keep the raw event trail visible below the aggregates. This makes the
	// numbers auditable without exposing prompts or source file contents.
	if recent := recentEventRows(a); len(recent) > 0 {
		right = append(right,
			statsSectionHeader(core.T("stats.recent"), theme),
			statsCard(recent),
		)
	}

	if !a.Monitor.HasAnySource() {
		left = append(left, muted(core.T("hover.none"), theme))
	}
	out := []gui.View{
		statsHeadCard(a, total, theme),
		gui.Row(gui.ContainerCfg{
			ID: "console.stats.columns", Sizing: gui.FillFill, Spacing: gui.SomeF(8),
			VAlign: gui.VAlignTop,
			Content: []gui.View{
				gui.Column(gui.ContainerCfg{ID: "console.stats.left", Sizing: gui.FillFill, Spacing: gui.SomeF(4), Content: left}),
				gui.Column(gui.ContainerCfg{ID: "console.stats.right", Sizing: gui.FillFill, Spacing: gui.SomeF(4), Content: right}),
			},
		}),
	}

	return []gui.View{
		gui.Column(gui.ContainerCfg{
			ID:         "console.stats.scroll",
			Sizing:     gui.FillFill,
			Scrollable: true,
			ScrollbarCfgY: &gui.ScrollbarCfg{
				Overflow: gui.ScrollbarAuto,
			},
			Spacing: gui.SomeF(4),
			Padding: gui.PadAll(2),
			Content: out,
		}),
	}
}

func todayTimeline(hourly []core.HourlyUsage) gui.View {
	if hourMax(hourly) > 0 {
		return statsCard(hourlyChart(hourly))
	}
	return statsCard([]gui.View{gui.Text(gui.TextCfg{
		Text: core.T("hover.none"), TextStyle: statStyle(12, statFaint, false),
		Mode: gui.TextModeWrap, Sizing: gui.FillFit,
	})})
}

// statsSectionHeader aligns section labels with the text inset inside the
// cards below them, so the two-column grid reads as one continuous panel.
func statsSectionHeader(text string, theme gui.Theme) gui.View {
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Padding: gui.NewPadding(0, statCardPad, 0, statCardPad),
		Content: []gui.View{gui.Text(gui.TextCfg{Text: text, TextStyle: theme.B3})},
	})
}

type rollingUsageStats struct {
	seven, thirty, retained int
	activeDays              int
	days                    []dailyUsage
}

type dailyUsage struct {
	day    time.Time
	tokens int
}

// rollingUsage derives the retained-history summaries from the local event
// store. The store itself enforces the 45-day retention period.
func rollingUsage(a *App) rollingUsageStats {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	retainedStart := today.AddDate(0, 0, -44)
	sevenStart := today.AddDate(0, 0, -6)
	thirtyStart := today.AddDate(0, 0, -29)

	r := rollingUsageStats{days: make([]dailyUsage, 7)}
	byDay := make(map[string]int, 45)
	for i := range r.days {
		r.days[i].day = sevenStart.AddDate(0, 0, i)
	}
	if a == nil || a.Store == nil {
		return r
	}
	for _, e := range a.Store.RecentEvents(retainedStart, 0) {
		if e.Tokens <= 0 {
			continue
		}
		day := time.Date(e.Timestamp.Year(), e.Timestamp.Month(), e.Timestamp.Day(), 0, 0, 0, 0, now.Location())
		if day.Before(retainedStart) || day.After(today) {
			continue
		}
		r.retained += e.Tokens
		if !day.Before(thirtyStart) {
			r.thirty += e.Tokens
			key := day.Format("2006-01-02")
			byDay[key] += e.Tokens
		}
		if !day.Before(sevenStart) {
			r.seven += e.Tokens
		}
	}
	for day, tokens := range byDay {
		if day != "" && tokens > 0 {
			r.activeDays++
		}
	}
	for i := range r.days {
		r.days[i].tokens = byDay[r.days[i].day.Format("2006-01-02")]
	}
	return r
}

func rollingSummaryRows(r rollingUsageStats) []gui.View {
	return []gui.View{
		statLine(core.T("stats.last7"), core.Compact(int64(r.seven)), statBright),
		statLine(core.T("stats.last30"), core.Compact(int64(r.thirty)), statBright),
		statLine(core.T("stats.activeDays"), strconv.Itoa(r.activeDays)+" / 30", statAmber),
		statLine(core.T("stats.storedHistory"), core.Compact(int64(r.retained))+"  (45 days)", statDim),
	}
}

func rollingDailyRows(r rollingUsageStats) []gui.View {
	max := 0
	for _, day := range r.days {
		if day.tokens > max {
			max = day.tokens
		}
	}
	rows := make([]gui.View, 0, len(r.days))
	for _, day := range r.days {
		fraction := 0.0
		if max > 0 {
			fraction = float64(day.tokens) / float64(max)
		}
		rows = append(rows, gui.Row(gui.ContainerCfg{
			Sizing:  gui.FillFit,
			Spacing: gui.SomeF(8),
			Padding: gui.NoPadding,
			Content: []gui.View{
				cell(54, styled(day.day.Format("01/02"), statStyle(11, statFaint, false))),
				statBar(fraction, statBarW, statAmber, statTrack),
				cell(statValueW, rightAligned(core.Compact(int64(day.tokens)), statStyle(12, statBright, false))),
			},
		}))
	}
	return rows
}

func recentEventRows(a *App) []gui.View {
	if a == nil || a.Store == nil {
		return nil
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	events := a.Store.RecentEvents(start, 0)
	if len(events) == 0 {
		return nil
	}
	if len(events) > 5 {
		events = events[len(events)-5:]
	}
	rows := make([]gui.View, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		timeText := e.Timestamp.Local().Format("15:04:05")
		source := e.Source.DisplayName()
		if e.IsEstimated {
			source += " ~"
		}
		breakdown := ""
		if n := derefInt(e.Breakdown.Input); n > 0 {
			breakdown += "in " + core.Compact(int64(n))
		}
		if n := derefInt(e.Breakdown.Output); n > 0 {
			if breakdown != "" {
				breakdown += "  "
			}
			breakdown += "out " + core.Compact(int64(n))
		}
		if breakdown == "" {
			breakdown = "token event"
		}
		rows = append(rows, gui.Row(gui.ContainerCfg{
			Sizing:  gui.FillFit,
			Spacing: gui.SomeF(8),
			Padding: gui.PadAll(1),
			Content: []gui.View{
				cell(62, styled(timeText, statStyle(11, statFaint, false))),
				flexCell(styled(source, statStyle(12, statDim, false))),
				cell(112, rightAligned(core.Compact(int64(e.Tokens)), statStyle(12, statBright, true))),
				cell(128, rightAligned(breakdown, statStyle(10, statFaint, false))),
			},
		}))
	}
	return rows
}

// statsHeadCard is the headline: today's total, the live fire, and the peak
// hour. The big number is the one thing a glance should land on.
func statsHeadCard(a *App, total int, theme gui.Theme) gui.View {
	a.fireMu.Lock()
	rate := a.Fire.TokensPerSecond
	snap := a.Fire.LiveSnapshot
	a.fireMu.Unlock()

	headline := gui.Column(gui.ContainerCfg{
		ID:      "console.stats.headline",
		Sizing:  gui.FitFit,
		Spacing: gui.SomeF(2),
		Padding: gui.NoPadding,
		Content: []gui.View{
			gui.Text(gui.TextCfg{
				Text:      core.T("stats.today"),
				TextStyle: statStyle(11, statDim, false),
				Sizing:    gui.FitFit,
			}),
			gui.Text(gui.TextCfg{
				Text:      core.Compact(int64(total)),
				TextStyle: statStyle(26, statBright, true),
				Sizing:    gui.FitFit,
			}),
		},
	})

	rateText := "—"
	if core.AppInfo.Version() != "" && a.Settings.ShowLiveRate && rate > 0 {
		rateText = format1(rate) + " " + core.T("hover.tokensS")
	}

	hours := a.Monitor.TodayHourly()
	peakText := "—"
	if hour := peakHour(hours); hour >= 0 {
		label := strconv.Itoa(hour)
		if hour < 10 {
			label = "0" + label
		}
		peakText = label + ":00  ·  " + core.Compact(int64(hourTokens(hours, hour)))
	}

	details := gui.Column(gui.ContainerCfg{
		ID:      "console.stats.details",
		Sizing:  gui.FitFit,
		Spacing: gui.SomeF(4),
		Padding: gui.NoPadding,
		Content: []gui.View{
			statLine(core.T("stats.tier"), snap.Tier.Label(), statAmber),
			statLine(core.T("hover.rate"), rateText, statBright),
			statLine(core.T("stats.peak"), peakText, statDim),
		},
	})

	return statsCard([]gui.View{
		gui.Row(gui.ContainerCfg{
			ID:      "console.stats.headrow",
			Sizing:  gui.FillFit,
			Spacing: gui.SomeF(28),
			Padding: gui.NoPadding,
			Content: []gui.View{headline, details},
		}),
	})
}

// statLine is a caption/value pair inside a card. The caption column is fixed so
// the values line up down the card.
func statLine(label, value string, valueColor gui.Color) gui.View {
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SomeF(8),
		Padding: gui.NoPadding,
		Content: []gui.View{
			cell(74, gui.Text(gui.TextCfg{
				Text:      label,
				TextStyle: statStyle(11, statFaint, false),
			})),
			gui.Text(gui.TextCfg{
				Text:      value,
				TextStyle: statStyle(12, valueColor, false),
				Sizing:    gui.FitFit,
			}),
		},
	})
}

// breakdownRows is the token-class list: one labelled bar per class, each in its
// own colour.
//
// Bars are proportional to the total rather than to the largest class, matching
// the C# UsageBars. Cache reads usually dominate a coding session, so the other
// classes legitimately render as short bars; the percentage column beside each
// value is what makes their weight readable.
func breakdownRows(b core.UsageBreakdown, total int) []gui.View {
	rows := []struct {
		label  string
		tokens int
	}{
		{core.T("breakdown.input"), derefInt(b.Input)},
		{core.T("breakdown.output"), derefInt(b.Output)},
		{core.T("breakdown.cacheRead"), derefInt(b.CacheRead)},
		{core.T("breakdown.cacheWrite"), derefInt(b.CacheWrite)},
	}

	classes := 0
	for _, r := range rows {
		classes += r.tokens
	}
	// The class sum can differ from the headline total (a source may report a
	// total without a per-class split), so scale against whichever is larger to
	// keep the bars inside their track.
	scale := total
	if classes > scale {
		scale = classes
	}

	out := make([]gui.View, 0, len(rows))
	for i, r := range rows {
		out = append(out, statBarRow(
			r.label, r.tokens, scale, statClassColors[i%len(statClassColors)]))
	}
	return out
}

// sourceBars ranks today's sources by tokens.
func sourceBars(bySource map[core.UsageSource]int) []gui.View {
	type entry struct {
		src    core.UsageSource
		tokens int
	}
	list := make([]entry, 0, len(bySource))
	max := 0
	for _, src := range core.UsageSourcesAll {
		tokens := bySource[src]
		if tokens <= 0 {
			continue
		}
		list = append(list, entry{src, tokens})
		if tokens > max {
			max = tokens
		}
	}
	// Descending; insertion sort keeps it dependency-free and the list is short.
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].tokens > list[j-1].tokens; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	if len(list) > 8 {
		list = list[:8]
	}

	out := make([]gui.View, 0, len(list))
	for _, e := range list {
		accent := fire.SourceFlameColors.Accent(e.src)
		dotStyle := statStyle(12, statBright, false)
		dotStyle.Color = gui.RGB(
			byte(accent[0]*255), byte(accent[1]*255), byte(accent[2]*255))

		out = append(out, gui.Row(gui.ContainerCfg{
			Sizing:  gui.FillFit,
			Spacing: gui.SomeF(8),
			Padding: gui.NoPadding,
			Content: []gui.View{
				cell(16, gui.Text(gui.TextCfg{Text: "●", TextStyle: dotStyle})),
				cell(statNameW, gui.Text(gui.TextCfg{
					Text:      e.src.DisplayName(),
					TextStyle: statStyle(12, statDim, false),
				})),
				statBar(float64(e.tokens)/float64(max), statBarW, dotStyle.Color, statTrack),
				cell(statValueW, rightAligned(
					core.Compact(int64(e.tokens)), statStyle(12, statBright, false))),
			},
		}))
	}
	return out
}

// statBarRow is a labelled bar with an absolute value and a share of the scale.
func statBarRow(label string, tokens, scale int, fill gui.Color) gui.View {
	fraction := 0.0
	share := "0.0%"
	if scale > 0 {
		fraction = float64(tokens) / float64(scale)
		share = format1(float64(tokens)*100/float64(scale)) + "%"
	}

	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SomeF(8),
		Padding: gui.NoPadding,
		Content: []gui.View{
			cell(statLabelW, gui.Text(gui.TextCfg{
				Text:      label,
				TextStyle: statStyle(12, statDim, false),
			})),
			statBar(fraction, statBarW, fill, statTrack),
			cell(statValueW, rightAligned(
				core.Compact(int64(tokens)), statStyle(12, statBright, false))),
			cell(statPercentW, rightAligned(
				share, statStyle(11, statFaint, false))),
		},
	})
}

// statBar draws a proportional bar as a filled segment plus the remaining track.
//
// Two rectangles rather than an overlay because go-gui lays out in flow order
// rather than in layers; laid end to end with no gap they read as one bar, and
// the rounded fill end gives the usual progress-bar cap.
func statBar(fraction float64, width float32, fill, track gui.Color) gui.View {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filledW := float32(math.Round(float64(width) * fraction))

	parts := make([]gui.View, 0, 2)
	if filledW > 0 {
		parts = append(parts, gui.Rectangle(gui.RectangleCfg{
			Width:  filledW,
			Height: statBarH,
			Radius: statBarRadius,
			Color:  fill,
			Sizing: gui.FixedFixed,
		}))
	}
	if rest := width - filledW; rest > 0 {
		parts = append(parts, gui.Rectangle(gui.RectangleCfg{
			Width:  rest,
			Height: statBarH,
			Radius: statBarRadius,
			Color:  track,
			Sizing: gui.FixedFixed,
		}))
	}

	return gui.Row(gui.ContainerCfg{
		ID:      "console.stats.bar",
		Sizing:  gui.FixedFixed,
		Width:   width,
		Height:  statBarH,
		Spacing: gui.SomeF(0),
		Padding: gui.NoPadding,
		VAlign:  gui.VAlignMiddle,
		Content: parts,
	})
}

// hourlyChart draws the 24-hour histogram with its axis labels.
func hourlyChart(hours []core.HourlyUsage) []gui.View {
	max := hourMax(hours)
	peak := peakHour(hours)

	bars := make([]gui.View, 0, len(hours))
	for _, h := range hours {
		frac := 0.0
		if max > 0 {
			frac = float64(h.Tokens) / float64(max)
		}
		barH := float32(math.Round(frac * float64(hourChartH-6)))
		if h.Tokens > 0 && barH < 3 {
			barH = 3
		}

		// Every bar gets the warm ramp; the peak hour gets the full-strength
		// version so the eye finds it without counting the axis.
		body := []gui.View{}
		if barH > 0 {
			body = append(body, gui.Rectangle(gui.RectangleCfg{
				Width:    hourBarW - 4,
				Height:   barH,
				Radius:   2,
				Gradient: statTimelineGradient(h.Hour != peak),
				Sizing:   gui.FixedFixed,
			}))
		}

		bars = append(bars, gui.Column(gui.ContainerCfg{
			ID:      "console.stats.hour." + strconv.Itoa(h.Hour),
			Sizing:  gui.FixedFixed,
			Width:   hourBarW,
			Height:  hourChartH,
			Padding: gui.NoPadding,
			HAlign:  gui.HAlignCenter,
			VAlign:  gui.VAlignBottom,
			Content: body,
		}))
	}

	// One label slot per bar, every third slot filled — the C# chart's
	// i % 3 == 0 rule. Keeping a slot per hour is what makes the labels line up
	// with the bars they belong to.
	axis := make([]gui.View, 0, len(hours))
	for _, h := range hours {
		label := ""
		if h.Hour%3 == 0 {
			label = strconv.Itoa(h.Hour)
			if h.Hour < 10 {
				label = "0" + label
			}
		}
		axis = append(axis, gui.Column(gui.ContainerCfg{
			ID:      "console.stats.tick." + strconv.Itoa(h.Hour),
			Sizing:  gui.FixedFit,
			Width:   hourBarW,
			Padding: gui.NoPadding,
			HAlign:  gui.HAlignCenter,
			Content: []gui.View{
				gui.Text(gui.TextCfg{
					Text:      label,
					TextStyle: statStyle(10, statFaint, false),
					Sizing:    gui.FitFit,
				}),
			},
		}))
	}

	return []gui.View{
		gui.Row(gui.ContainerCfg{
			ID:      "console.stats.chart",
			Sizing:  gui.FitFit,
			Spacing: gui.SomeF(0),
			Padding: gui.NoPadding,
			Content: bars,
		}),
		gui.Row(gui.ContainerCfg{
			ID:      "console.stats.axis",
			Sizing:  gui.FitFit,
			Spacing: gui.SomeF(0),
			Padding: gui.NoPadding,
			Content: axis,
		}),
	}
}

func hourMax(hours []core.HourlyUsage) int {
	max := 0
	for _, h := range hours {
		if h.Tokens > max {
			max = h.Tokens
		}
	}
	return max
}

func peakHour(hours []core.HourlyUsage) int {
	best, bestHour := 0, -1
	for _, h := range hours {
		if h.Tokens > best {
			best, bestHour = h.Tokens, h.Hour
		}
	}
	return bestHour
}

func hourTokens(hours []core.HourlyUsage, hour int) int {
	for _, h := range hours {
		if h.Hour == hour {
			return h.Tokens
		}
	}
	return 0
}

// statsCard wraps content in the warm dark glass panel the console's own stats
// views use, so they read as CodingFire's HUD rather than as theme chrome.
func statsCard(content []gui.View) gui.View {
	return gui.Column(gui.ContainerCfg{
		ID:          "console.stats.card",
		Sizing:      gui.FillFit,
		Padding:     gui.PadAll(statCardPad),
		Spacing:     gui.SomeF(statCardGap),
		Radius:      gui.SomeF(hoverCardRadius),
		Color:       statCardBG,
		ColorBorder: statCardBorder,
		SizeBorder:  gui.SomeF(1),
		Content:     content,
	})
}

// derefInt reads an optional token count; nil means the source did not report
// that class, which counts as zero for display.
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// statStyle is a card text style: explicit family, size, colour and weight so the
// stats cards do not inherit the console theme's typography.
func statStyle(size float32, color gui.Color, bold bool) gui.TextStyle {
	s := gui.TextStyle{Family: "Segoe UI", Size: size, Color: color}
	if bold {
		s.Typeface = glyph.TypefaceBold
	}
	return s
}

// ---------------------------------------------------------------------------
// Sources tab
// ---------------------------------------------------------------------------

// Column widths, in logical pixels. These are the C# ListView's column widths:
// the name column is 132 there because 96 clipped "WorkBuddy (INTL)".
const (
	colDotW    = 16
	colNameW   = 150
	colStateW  = 100
	colTodayW  = 78
	colHeaderH = 22
	colRowH    = 22
)

func sourcesTab(s *consoleState) []gui.View {
	a := s.App
	theme := gui.CurrentTheme()

	statuses := a.Monitor.Statuses()
	okCount := 0
	for _, st := range statuses {
		if st.State == core.SourceOk {
			okCount++
		}
	}

	rows := make([]gui.View, 0, len(statuses)+1)
	rows = append(rows, gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFixed,
		Height:  colHeaderH,
		Spacing: gui.SomeF(4),
		Padding: gui.NoPadding,
		Content: []gui.View{
			cell(colDotW, muted("", theme)),
			cell(colNameW, muted(core.T("sources.source"), theme)),
			cell(colStateW, muted(core.T("sources.state"), theme)),
			cell(colTodayW, muted(core.T("sources.today"), theme)),
			flexCell(muted(core.T("sources.path"), theme)),
		},
	}))

	for _, st := range statuses {
		rows = append(rows, sourceRow(st, theme))
	}

	head := []gui.View{
		gui.Row(gui.ContainerCfg{
			Sizing:  gui.FillFit,
			Spacing: gui.SomeF(10),
			Padding: gui.NoPadding,
			Content: []gui.View{
				gui.Button(gui.ButtonCfg{
					ID:    "console.rescan",
					Label: core.T("sources.rescan"),
					OnClick: func(gui.EventCtx) {
						a.Rescan()
					},
				}),
			},
		}),
	}

	if a.Monitor.IsScanning() {
		head = append(head, muted(core.T("sources.scanning"), theme))
	}

	return append(head,
		gui.Column(gui.ContainerCfg{
			ID:         "console.sources.scroll",
			Sizing:     gui.FillFill,
			Spacing:    gui.SomeF(0),
			Padding:    gui.NoPadding,
			Scrollable: true,
			ScrollbarCfgY: &gui.ScrollbarCfg{
				Overflow: gui.ScrollbarAuto,
			},
			Content: rows,
		}),
		// The empty-state hint sits below the list rather than inside it, so
		// it does not scroll away with the rows.
		gui.Text(gui.TextCfg{
			Text:      core.T("hover.none"),
			TextStyle: theme.TextStyleSecondary,
			Invisible: okCount != 0,
		}),
	)
}

// sourceRow renders one source line.
func sourceRow(st core.SourceStatus, theme gui.Theme) gui.View {
	style := theme.N3
	if st.State != core.SourceOk {
		// Dim the whole row when the source is not connected, so the eye can
		// find the working ones without reading every state cell.
		style = theme.TextStyleSecondary
	}

	// The dot is a glyph, not a Rectangle: a fixed-size box in a Row does not
	// share the text baseline, so a shape-based dot floats above its own label.
	// See hoverCardRow for the same reasoning.
	accent := fire.SourceFlameColors.Accent(st.Source)
	dotStyle := style
	dotStyle.Color = gui.RGB(byte(accent[0]*255), byte(accent[1]*255), byte(accent[2]*255))
	dot := gui.Text(gui.TextCfg{Text: "●", TextStyle: dotStyle, Sizing: gui.FitFit})

	path := st.Detail
	if path == "" {
		path = "—"
	}

	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFixed,
		Height:  colRowH,
		Spacing: gui.SomeF(4),
		Padding: gui.NoPadding,
		Content: []gui.View{
			// The dot is centred vertically by the row's own alignment; it is
			// only here to tie the row back to the console's colour legend.
			cell(colDotW, dot),
			cell(colNameW, styled(st.Source.DisplayName(), style)),
			cell(colStateW, styled(st.State.Label(), style)),
			cell(colTodayW, rightAligned(core.Compact(int64(st.TodayTokens)), style)),
			flexCell(styled(path, style)),
		},
	})
}

// ---------------------------------------------------------------------------
// Settings tab
// ---------------------------------------------------------------------------

func settingsTab(s *consoleState) []gui.View {
	a := s.App
	theme := gui.CurrentTheme()

	out := []gui.View{
		sectionHeader(core.T("settings.flame"), theme),

		labelledField(core.T("settings.flameSize"), func() gui.View {
			labels := make([]string, 0, len(core.FlameSizesAll))
			for _, sz := range core.FlameSizesAll {
				labels = append(labels, sz.Label())
			}
			return gui.Select(gui.SelectCfg{
				ID:       "console.size",
				Options:  labels,
				Selected: []string{a.Settings.Size.Label()},
				OnSelect: func(sel []string, _ gui.EventCtx) {
					if len(sel) == 0 {
						return
					}
					for _, sz := range core.FlameSizesAll {
						if sz.Label() == sel[0] {
							a.SetFlameSize(sz)
							return
						}
					}
				},
			})
		}()),

		checkRow("console.visible", core.T("settings.visible"), a.Settings.FlameVisible,
			func(on bool) { a.SetFlameVisible(on) }),
		checkRow("console.rate", core.T("settings.showRate"), a.Settings.ShowLiveRate,
			func(on bool) {
				a.Settings.ShowLiveRate = on
				a.Settings.Save()
			}),
		checkRow("console.autostart", core.T("settings.autoStart"), a.Settings.AutoStart,
			func(on bool) { a.SetAutoStart(on) }),

		checkRow("console.clickthrough", core.T("settings.clickThrough"), a.clickThrough,
			func(on bool) { a.SetClickThrough(on) }),
		muted(core.T("settings.clickThroughHint"), theme),

		labelledField(core.T("settings.language"), func() gui.View {
			return gui.Select(gui.SelectCfg{
				ID:       "console.language",
				Options:  consoleLanguageNames(),
				Selected: []string{consoleLanguageNames()[int(a.Settings.Language)]},
				OnSelect: func(sel []string, _ gui.EventCtx) {
					if len(sel) == 0 {
						return
					}
					for i, name := range consoleLanguageNames() {
						if name == sel[0] {
							a.SetLanguage(core.AppLanguage(i))
							return
						}
					}
				},
			})
		}()),

		gui.Row(gui.ContainerCfg{
			Sizing:  gui.FillFit,
			Spacing: gui.SomeF(8),
			Padding: gui.NoPadding,
			Content: []gui.View{
				gui.Button(gui.ButtonCfg{
					ID:    "console.resetpos",
					Label: core.T("settings.resetPosition"),
					OnClick: func(gui.EventCtx) {
						a.placeDefault()
					},
				}),
				gui.Button(gui.ButtonCfg{
					ID:    "console.resetcolors",
					Label: core.T("settings.resetColors"),
					OnClick: func(gui.EventCtx) {
						a.ResetColors()
					},
				}),
			},
		}),

		sectionHeader(core.T("settings.flameColor"), theme),
		muted(core.T("settings.colorsHint"), theme),
		swatchRow(s, theme),
	}

	if s.PickingCustom {
		rgb := a.Settings.FlameColor
		out = append(out, gui.ColorPicker(gui.ColorPickerCfg{
			ID:      "console.customcolor",
			Color:   gui.RGB(byte(rgb[0]*255), byte(rgb[1]*255), byte(rgb[2]*255)),
			Width:   320,
			Height:  200,
			ShowHSL: true,
			OnColorChange: func(c gui.Color, _ gui.EventCtx) {
				a.SetFlameColor(float64(c.R)/255, float64(c.G)/255, float64(c.B)/255)
			},
		}))
	}

	return []gui.View{
		gui.Column(gui.ContainerCfg{
			ID:         "console.settings.scroll",
			Sizing:     gui.FillFill,
			Scrollable: true,
			ScrollbarCfgY: &gui.ScrollbarCfg{
				Overflow: gui.ScrollbarAuto,
			},
			Spacing: gui.SomeF(8),
			Padding: gui.PadAll(4),
			Content: out,
		}),
	}
}

// consoleLanguageNames is the language picker's option list. The order is the
// AppLanguage enum order, and the labels are deliberately self-naming: a
// language list is the one place where translating the names is actively
// unhelpful, because you need to find your own language in a UI you cannot read.
func consoleLanguageNames() []string {
	return []string{
		core.T("settings.lang.system"),
		"English",
		"简体中文",
		"日本語",
		"한국어",
	}
}

// exoticFire is one flame-colour preset. The values are the C# build's, and the
// names are intentionally not translated — they are colour names, like "Teal",
// and a translated "Teal" would not help anyone identify the swatch.
type exoticFire struct {
	Name    string
	R, G, B float64
}

var exoticFires = []exoticFire{
	{"Teal", 0.00, 0.83, 0.67},
	{"Emerald", 0.00, 1.00, 0.70},
	{"Crimson", 1.00, 0.13, 0.00},
	{"Ocean", 0.00, 0.53, 1.00},
	{"Violet", 0.60, 0.20, 1.00},
	{"Ice Blue", 0.53, 0.87, 1.00},
	{"Gold", 1.00, 0.67, 0.00},
	{"Dark Violet", 0.40, 0.00, 0.80},
	{"Electric", 1.00, 0.84, 0.00},
	{"Void", 0.10, 0.00, 0.20},
	{"Imperial", 1.00, 0.27, 0.00},
	{"Silver", 0.67, 0.80, 1.00},
}

// activeExoticIndex returns the preset matching the current flame colour, or -1.
// The tolerance is the C# build's 0.01, which is loose enough to survive the
// round-trip through the settings file's four decimal places.
func activeExoticIndex(current core.AccentRGB) int {
	for i, t := range exoticFires {
		if absDiff(t.R, current[0]) < 0.01 &&
			absDiff(t.G, current[1]) < 0.01 &&
			absDiff(t.B, current[2]) < 0.01 {
			return i
		}
	}
	return -1
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

// swatchRow is the preset palette plus the "..." button that reveals the picker.
func swatchRow(s *consoleState, theme gui.Theme) gui.View {
	a := s.App
	active := activeExoticIndex(a.Settings.FlameColor)

	swatches := make([]gui.View, 0, len(exoticFires)+1)
	for i, t := range exoticFires {
		idx := i
		// The selection ring is a border on the swatch itself rather than a
		// separate overlay: go-gui composites the whole window, so there is no
		// per-swatch paint hook to draw into.
		border := gui.ColorTransparent
		size := gui.SomeF(float32(0))
		if idx == active {
			border = gui.RGB(220, 220, 215)
			size = gui.SomeF(2)
		}
		swatches = append(swatches, gui.Column(gui.ContainerCfg{
			ID:          "console.swatch." + strconv.Itoa(idx),
			Sizing:      gui.FixedFixed,
			Width:       22,
			Height:      22,
			Radius:      gui.SomeF(11),
			Color:       gui.RGB(byte(t.R*255), byte(t.G*255), byte(t.B*255)),
			ColorBorder: border,
			SizeBorder:  size,
			OnClick: func(gui.EventCtx) {
				a.SetFlameColor(t.R, t.G, t.B)
			},
		}))
	}

	swatches = append(swatches, gui.Button(gui.ButtonCfg{
		ID:    "console.customcolor.toggle",
		Label: "...",
		OnClick: func(ctx gui.EventCtx) {
			st := gui.State[consoleState](ctx.Window)
			st.PickingCustom = !st.PickingCustom
			ctx.Window.InvalidateLayout()
		},
	}))

	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SomeF(6),
		Padding: gui.NoPadding,
		Content: swatches,
	})
}

// ---------------------------------------------------------------------------
// About tab
// ---------------------------------------------------------------------------

func aboutTab(s *consoleState) []gui.View {
	a := s.App
	theme := gui.CurrentTheme()

	// The block order is the C# build's, so the two read identically.
	lines := []string{
		core.T("app.name") + " " + core.Version + "  ·  Windows",
		core.T("app.tagline"),
		core.T("about.origin"),
		core.T("about.sources"),
		core.T("about.privacy"),
		core.T("about.powered"),
		core.T("about.gap"),
	}

	body := make([]gui.View, 0, len(lines)+1)
	for i, line := range lines {
		style := theme.N3
		if i == 0 {
			style = theme.B3
		}
		body = append(body, gui.Text(gui.TextCfg{
			Text:      line,
			TextStyle: style,
			Sizing:    gui.FillFit,
		}))
	}

	// The URL itself never changes with the language, so it is not in the L10n
	// table — it comes from the single source of truth in AppInfo.
	body = append(body, gui.Button(gui.ButtonCfg{
		ID:    "console.projectlink",
		Label: core.AppInfo.ProjectUrl(),
		OnClick: func(gui.EventCtx) {
			OpenLink(core.AppInfo.ProjectUrl())
		},
	}))

	_ = a
	return []gui.View{
		gui.Column(gui.ContainerCfg{
			ID:         "console.about.scroll",
			Sizing:     gui.FillFill,
			Scrollable: true,
			ScrollbarCfgY: &gui.ScrollbarCfg{
				Overflow: gui.ScrollbarAuto,
			},
			Spacing: gui.SomeF(10),
			Padding: gui.PadAll(8),
			Content: body,
		}),
	}
}

// ---------------------------------------------------------------------------
// Small view helpers
// ---------------------------------------------------------------------------

// cell is a fixed-width column. go-gui's Text has no width of its own, so the
// width goes on a wrapping row — Sizing is FixedFit, which pins the width and
// lets the height follow the content.
func cell(width float32, v gui.View) gui.View {
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FixedFit,
		Width:   width,
		Padding: gui.NoPadding,
		HAlign:  gui.HAlignStart,
		VAlign:  gui.VAlignMiddle,
		Content: []gui.View{v},
	})
}

// flexCell is the last column: it takes whatever width is left.
func flexCell(v gui.View) gui.View {
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Padding: gui.NoPadding,
		HAlign:  gui.HAlignStart,
		VAlign:  gui.VAlignMiddle,
		Content: []gui.View{v},
	})
}

func styled(text string, style gui.TextStyle) gui.View {
	return gui.Text(gui.TextCfg{Text: text, TextStyle: style})
}

func rightAligned(text string, style gui.TextStyle) gui.View {
	style.Align = gui.TextAlignRight
	return gui.Text(gui.TextCfg{Text: text, TextStyle: style, Sizing: gui.FillFit})
}

func muted(text string, theme gui.Theme) gui.View {
	return gui.Text(gui.TextCfg{Text: text, TextStyle: theme.TextStyleSecondary})
}

func sectionHeader(text string, theme gui.Theme) gui.View {
	return gui.Text(gui.TextCfg{Text: text, TextStyle: theme.B3})
}

// labelledField puts a caption to the left of a control, which is what the C#
// build's two-column settings table did.
func labelledField(caption string, control gui.View) gui.View {
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SomeF(8),
		Padding: gui.NoPadding,
		Content: []gui.View{
			cell(130, muted(caption, gui.CurrentTheme())),
			control,
		},
	})
}

// checkRow is a checkbox wired to a setter. The setter is called with the new
// value rather than the caller reading the widget, because the widget is
// rebuilt from state every frame and holds nothing itself.
func checkRow(id, label string, checked bool, set func(bool)) gui.View {
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SomeF(8),
		Padding: gui.NoPadding,
		Content: []gui.View{
			gui.Checkbox(gui.ToggleCfg{
				ID:       id,
				Label:    label,
				Selected: checked,
				OnClick: func(ctx gui.EventCtx) {
					set(!checked)
					ctx.Window.InvalidateLayout()
				},
			}),
		},
	})
}
