package ui

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend"

	"github.com/wangsrGit119/codingfire/internal/core"
	"github.com/wangsrGit119/codingfire/internal/data"
	"github.com/wangsrGit119/codingfire/internal/fire"
)

// FlameWindowTitle identifies the campfire window. It is also how the Win32
// shim finds our own HWND, because go-gui keeps that unexported.
const FlameWindowTitle = "CodingFire"

// tickInterval drives the fire state machine, matching the C# version's 20Hz
// timer and the 12fps simulation stepping.
const tickInterval = 50 * time.Millisecond

// The flame reuses one CPU image and one GPU texture, independent of uptime.
const flameImageKey = "codingfire/frame"

// memImageBudget also covers static images used by the console.
const memImageBudget = 8 << 20

// App assembles the whole application: settings → event store → scanner → fire
// state machine → campfire window → tray icon → console.
type App struct {
	Settings       *core.Settings
	Store          *data.UsageStore
	Fire           *fire.FireStateMachine
	Monitor        *data.UsageMonitor
	Renderer       *fire.CampfireRenderer
	bitmapOverlays bool
	probeHidden    bool // integration probes create no visible windows or tray
	flameBitmap    image.NRGBA
	nativeHover    *nativeHoverPainter

	gapp *gui.App
	gw   *gui.Window

	tray *gui.SystemTrayHandle

	mu sync.Mutex
	// fireMu serializes the simulation between the 20Hz timer, the monitor's
	// UI-posted ingest callbacks, and the frame generator. The monitor used to
	// mutate Fire from its scanner goroutine when no post hook was installed;
	// keeping this lock here also makes the ownership explicit for future hooks.
	fireMu sync.Mutex
	// The timer reads visibility without racing with GUI settings updates.
	flameHidden atomic.Bool
	// panelPhysW/H are the physical pixels the renderer draws into;
	// panelLogW/H are what the layout is told, so the texture maps 1:1.
	panelPhysW int
	panelPhysH int
	panelLogW  float32
	panelLogH  float32

	startTime time.Time
	hwnd      uintptr

	// placed reports whether the campfire has been positioned on purpose this
	// session. Until it is true the window's rectangle is whatever the toolkit
	// picked — (0,0) — and that is not a position the user chose, so it must
	// never be recorded as one. See pollPosition.
	placed bool

	// Hover card. The card is its own always-present window, parked off-screen
	// until the cursor is over the flame; see hoverAt.
	hover      *gui.Window
	hoverHwnd  uintptr
	hoverShown bool
	// hoverMiss counts frames the cursor has been off the flame, mirroring the
	// C# build's _hoverMissCount: the flame moves, so a pixel-exact hit test
	// occasionally drops out for a frame and the card should not blink.
	hoverMiss        int
	hoverLastRefresh time.Time

	// Dragging is polled globally rather than handled through go-gui mouse
	// events. The campfire is click-through by default, so Windows routes its
	// button messages to the desktop underneath. These coordinates are screen
	// pixels, matching GetWindowRect and SetWindowPosition.
	dragging         bool
	dragReady        bool
	dragStartCursorX int
	dragStartCursorY int
	dragStartWindowX int
	dragStartWindowY int

	// ClickThrough lets the campfire sit on the desktop without swallowing
	// clicks meant for the icons underneath.
	//
	// The original got this for free: UpdateLayeredWindow makes fully
	// transparent pixels pass clicks through automatically, so the flame itself
	// stayed hoverable while the surrounding area did not block anything.
	// go-gui renders the whole window as one GPU surface and hit-tests its own
	// widgets, so per-pixel pass-through is not available — it is all or
	// nothing. All-or-nothing it is, defaulting to on.
	//
	// Hovering still works with this on, because the hover test polls the cursor
	// and samples the rendered frame rather than waiting for mouse messages.
	clickThrough bool

	console *gui.Window
	// consolePendingAt is when a console open was queued. A timestamp rather
	// than a bool so a request that never materialises — the backend refusing
	// the window, say — cannot wedge the menu item forever.
	consolePendingAt time.Time
	// overlayNoticedAt is when guardOverlay last reported a repair, so a window
	// that stays covered cannot fill the log one line per second.
	overlayNoticedAt time.Time

	quitting bool
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewApp builds the application but does not run it.
func NewApp() *App {
	return newApp(true)
}

func newApp(syncAutoStart bool) *App {
	a := &App{
		startTime:      time.Now(),
		clickThrough:   true,
		dragReady:      true,
		stopCh:         make(chan struct{}),
		bitmapOverlays: runtime.GOOS == "windows" && os.Getenv("CODINGFIRE_OVERLAY_BACKEND") != "gl",
	}

	// Pixel art must land 1:1; go-gui scales logical pixels by this itself, so
	// the renderer draws at logical*scale and the layout is told the logical
	// size.
	fire.DpiScale = SystemDPIScale()
	gui.SetMemImageBudget(memImageBudget)

	a.Settings = core.LoadSettings()
	a.flameHidden.Store(!a.Settings.FlameVisible)
	core.SetLanguage(a.Settings.Language)
	fire.SourceFlameColors.Attach(a.Settings)

	// Auto-start defaults to on: push the setting into the login entry, and if
	// the write fails, sync the setting back to the real state so the tray
	// checkmark cannot lie. Done before the tray is built so the menu is
	// correct the moment it appears.
	if syncAutoStart {
		a.syncAutoStart()
	}

	a.Store = data.NewUsageStore()
	a.Store.Open()

	a.Fire = fire.NewFireStateMachine()
	a.Fire.LiveSnapshot.FlameAccent = a.Settings.FlameColor

	a.Monitor = data.NewUsageMonitor(a.Store)
	a.Monitor.Ingest = func(tokens float64, src *core.UsageSource, at core.Time, animate bool) {
		a.fireMu.Lock()
		a.Fire.Ingest(tokens, src, at, animate)
		a.fireMu.Unlock()
	}
	a.Monitor.TodayTokensChanged = func(total int, bySource map[core.UsageSource]int) {
		a.fireMu.Lock()
		a.Fire.UpdateTodayTokens(total, bySource)
		a.fireMu.Unlock()
	}

	a.Renderer = fire.NewCampfireRenderer()
	a.applySize(a.Settings.Size)

	return a
}

// Run starts the GUI and blocks until the app exits.
func (a *App) Run() {
	a.gapp = gui.NewApp()
	a.gapp.ExitMode = gui.ExitOnTrayRemoved

	// The campfire panel: frameless, per-pixel transparent, fixed size.
	flameCfg := gui.WindowCfg{
		State:           a,
		Title:           FlameWindowTitle,
		Width:           int(a.panelLogW),
		Height:          int(a.panelLogH),
		Transparent:     true,
		FixedSize:       true,
		Decorations:     gui.DecorationNone,
		BgColor:         gui.ColorTransparent,
		InitiallyHidden: true,
		OnInit: func(w *gui.Window) {
			if a.bitmapOverlays {
				w.SetView(bitmapOverlayView)
			} else {
				w.SetView(a.flameView)
			}
			if !a.probeHidden {
				a.buildTray()
			}

			// Styling and placement happen here rather than in NewApp because
			// the native window does not exist until now. Exactly when it
			// becomes visible differs per platform — the Win32 backend shows it
			// as soon as OnInit returns, X11 has already mapped it — so doing
			// both in one pass is what keeps it from ever being seen at
			// go-gui's default (0,0).
			a.applyPlatformTweaks()
			if a.Settings.FlameVisible && !a.probeHidden {
				SetWindowVisible(a.hwnd, true)
			}

			// If the saved state says hidden, take the native window down
			// again. It stays alive so the tray's Show action can bring it
			// back later.
			if !a.Settings.FlameVisible && a.hwnd != 0 {
				SetWindowVisible(a.hwnd, false)
			}

			// All scanner results that affect the fire must be applied on the
			// GUI thread. Start only after the marshaller is installed; without
			// this hook finishScan mutates the state from the scanner goroutine.
			a.Monitor.SetPost(func(fn func()) {
				if a.gw != nil {
					a.gw.QueueCommand(func(*gui.Window) { fn() })
					return
				}
				fn()
			})
			a.Monitor.Start()
		},
	}
	if a.bitmapOverlays {
		flameCfg.BitmapFrame = a.flameBitmapFrame
	}
	a.gw = gui.NewWindow(flameCfg)

	// The hover card's own window. It is created up front and parked off-screen
	// rather than opened per hover: go-gui's OpenWindow is asynchronous, and a
	// window that has to be created, laid out and shown before it can be seen
	// would always lag the pointer by a few hundred milliseconds.
	hoverCfg := gui.WindowCfg{
		State:           &hoverCardState{},
		Title:           HoverWindowTitle,
		Width:           hoverCardWidth,
		Height:          int(hoverCardHeight(HoverModel{})),
		Transparent:     true,
		FixedSize:       true,
		Decorations:     gui.DecorationNone,
		BgColor:         gui.ColorTransparent,
		InitiallyHidden: true,
		OnInit: func(w *gui.Window) {
			if a.bitmapOverlays {
				w.SetView(bitmapOverlayView)
			} else {
				w.SetView(hoverCardWindowView)
			}
			// The card is created after the flame window, so its overlay
			// styles are applied from its own OnInit rather than guessed from
			// the flame's OnInit.
			a.applyHoverPlatformTweaks()
		},
	}
	if a.bitmapOverlays {
		hoverCfg.BitmapFrame = a.hoverBitmapFrame
	}
	a.hover = gui.NewWindow(hoverCfg)

	// 20Hz: advance the state machine, then ask for the next frame.
	go a.tickLoop()

	// The monitor is started from the flame window's OnInit, before the backend
	// enters its blocking event loop. Starting it here would never execute until
	// the GUI had already exited, which made live token changes appear frozen.
	backend.RunApp(a.gapp, a.gw, a.hover)
	if a.nativeHover != nil {
		a.nativeHover.close()
		a.nativeHover = nil
	}
	a.shutdown()
}

func (a *App) tickLoop() {
	t := time.NewTicker(tickInterval)
	defer t.Stop()

	// The console refreshes on the C# build's 900ms timer, and the window
	// position is polled once a second. Both are counted off the 20Hz tick
	// rather than given their own tickers, so there is one loop to reason about.
	const consoleEvery = int(900 * time.Millisecond / tickInterval)
	const positionEvery = int(time.Second / tickInterval)

	var n int
	var frames frameSchedule
	for {
		select {
		case <-a.stopCh:
			return
		case now := <-t.C:
			a.fireMu.Lock()
			a.Fire.Tick()
			phase := a.Fire.Snapshot().Phase
			a.fireMu.Unlock()
			a.pollDrag()
			// Hover polling is independent of mouse messages. The campfire is
			// click-through by default, so this is the only reliable trigger;
			// doing it on the same heartbeat as the fire also works while the
			// GL window is idle.
			a.updateHover()
			if w := a.gw; w != nil && frames.due(now, phase, !a.flameHidden.Load()) {
				// flameView is the view generator that samples Fire and
				// registers the next image key. Render-only invalidation would
				// reuse the old Image view, so the visible flame would barely
				// change even though the state machine was ticking. A layout
				// invalidation is intentional here: it regenerates flameView
				// only when a new visible frame is due.
				w.InvalidateLayout()
			}

			n++
			if n%consoleEvery == 0 {
				a.refreshConsole()
			}
			if n%positionEvery == 0 {
				// Placement is retried until it sticks. The native window can
				// take a moment to appear, and the one-shot attempt 400ms after
				// startup would otherwise be the only chance it ever got.
				a.ensurePlaced()
				a.pollPosition()
				a.guardOverlay()
			}
		}
	}
}

// flameView renders one frame of the campfire.
func (a *App) flameView(w *gui.Window) gui.View {
	if a.flameHidden.Load() {
		return gui.Column(gui.ContainerCfg{Sizing: gui.FillFill, SizeBorder: gui.NoBorder})
	}
	a.renderFlame()

	a.mu.Lock()
	pw, ph := a.panelPhysW, a.panelPhysH
	lw, lh := a.panelLogW, a.panelLogH
	a.mu.Unlock()

	// Copy into a reusable buffer. GL updates the same texture in place;
	// previous animation frames no longer occupy the 128-entry texture cache.
	src := gui.UpdateImage(flameImageKey, pw, ph, a.Renderer.Pix())
	if src == "" {
		return gui.Column(gui.ContainerCfg{Sizing: gui.FillFill, SizeBorder: gui.NoBorder})
	}
	return gui.Image(gui.ImageCfg{
		ID:     "codingfire_flame",
		Src:    src,
		Width:  lw,
		Height: lh,
	})
}

// applySize recomputes the panel dimensions for a flame size.
//
// Two sizes are in play and they are not the same number. The logical size is
// what go-gui lays out in, and it multiplies by the window's DPI scale itself
// when it creates the native window. The physical size is what the renderer must
// draw into for the texture to land on the screen pixel for pixel. Confusing the
// two is invisible on a 100% display and soft on every other one: the renderer
// draws a logical-sized buffer and Windows stretches it.
func (a *App) applySize(size core.FlameSize) {
	px := size.PixelScale()
	logW := ceilF(float64(fire.FireW)*px + 28)
	logH := ceilF((float64(fire.FireH)+float64(fire.LogH))*px + 36)
	scale := fire.DpiScale
	physW := int(ceilF(logW * scale))
	physH := int(ceilF(logH * scale))

	a.mu.Lock()
	a.panelLogW, a.panelLogH = float32(logW), float32(logH)
	a.panelPhysW, a.panelPhysH = physW, physH
	a.mu.Unlock()

	a.Renderer.Resize(physW, physH)

	// On the first call the window does not exist yet: Run creates it from
	// panelLogW/H, so there is nothing to resize. Later calls — the size menu,
	// and the DPI correction once the window exists — have to go out through
	// SetWindowPos, because go-gui has no resize API.
	hwnd := a.flameHwnd()
	if hwnd == 0 {
		return
	}
	if !SetWindowSize(hwnd, physW, physH) {
		core.LogWarn("could not resize the campfire window")
	}
}

// refreshDPIScale re-reads the DPI scale now that the native window exists.
//
// It has to happen here rather than in NewApp: go-gui only makes the process DPI
// aware inside its own window creation, so asking any earlier reports a scale of
// 1.0 on a 125% or 150% display. That left the renderer drawing at logical size
// for a window Windows had already scaled up, which is why the pixel art looked
// soft.
func (a *App) refreshDPIScale() {
	hwnd := a.flameHwnd()
	if hwnd == 0 {
		return
	}
	scale := WindowDPIScale(hwnd)
	if scale == fire.DpiScale {
		return
	}
	fire.DpiScale = scale
	a.applySize(a.Settings.Size)
	core.LogInfo(fmt.Sprintf("display scale is %.2fx; panel resized", scale))
}

// flameHwnd returns the campfire window's handle, caching it once found. The
// handle is 0 until the backend has actually created the window, and go-gui
// keeps it unexported, so it has to be located by title.
func (a *App) flameHwnd() uintptr {
	a.mu.Lock()
	hwnd := a.hwnd
	a.mu.Unlock()
	// The cached handle is only trusted while it still refers to a window.
	// go-gui destroys windows without telling the app, and a stale handle is
	// worse than none: every call that used it would operate on nothing, and
	// the lookup that could have found the replacement would never run.
	if hwnd != 0 && WindowAlive(hwnd) {
		return hwnd
	}
	hwnd = FindOwnWindowByTitle(FlameWindowTitle)
	a.mu.Lock()
	a.hwnd = hwnd
	a.mu.Unlock()
	return hwnd
}

// guardOverlay repairs the campfire's window if it has been covered, hidden or
// minimised, and says so at most once a minute.
//
// The campfire is meant to sit above everything, and nothing but this makes
// that true after startup: always-on-top is the front of a band, and a later
// always-on-top window takes the front from us. A window that has fallen behind
// one looks exactly like a campfire that is gone, which is what the user
// reported. Silence is deliberate for the steady state — reporting every second
// would write 86,000 log lines a day for a window that stays covered.
func (a *App) guardOverlay() {
	hwnd := a.flameHwnd()
	if hwnd == 0 {
		return
	}
	visible := a.Settings.FlameVisible && !a.probeHidden
	alive, reason := GuardOverlayWindow(hwnd, visible)
	if alive && reason == "" {
		a.mu.Lock()
		a.overlayNoticedAt = time.Time{}
		a.mu.Unlock()
		return
	}
	if !alive {
		reason = "window is gone; it cannot be brought back without a restart"
	}

	a.mu.Lock()
	quiet := !a.overlayNoticedAt.IsZero() && time.Since(a.overlayNoticedAt) < overlayNoticeQuiet
	if !quiet {
		a.overlayNoticedAt = time.Now()
	}
	a.mu.Unlock()
	if !quiet {
		core.LogWarn("campfire " + reason)
	}
}

// overlayNoticeQuiet is how long guardOverlay stays quiet after reporting.
const overlayNoticeQuiet = time.Minute

func ceilF(v float64) float64 {
	i := float64(int(v))
	if v > i {
		return i + 1
	}
	return i
}

// applyPlatformTweaks pins both overlay windows on top and applies the overlay
// styles, then puts the campfire where it belongs.
func (a *App) applyPlatformTweaks() {
	a.mu.Lock()
	ct := a.clickThrough
	a.mu.Unlock()

	a.hwnd = ApplyOverlayWindowStyles(FlameWindowTitle, true, ct)
	if a.hwnd == 0 {
		core.LogWarn("campfire window not found; overlay styles were not applied")
	}

	// Now that the flame window exists, the real DPI scale can be read and the
	// panel resized to match. This runs before placement so the corner maths uses
	// the final size.
	a.refreshDPIScale()

	// Placement is a separate concern from the window styles, and it re-finds
	// the handle itself, so it runs either way.
	a.ensurePlaced()
}

// applyHoverPlatformTweaks is called from the card's own OnInit, after its HWND
// has been registered. The card is parked before go-gui shows it, so it never
// flashes at the default origin either.
func (a *App) applyHoverPlatformTweaks() {
	a.hoverHwnd = ApplyOverlayWindowStyles(HoverWindowTitle, true, true)
	if a.hoverHwnd == 0 {
		core.LogWarn("hover card window not found; overlay styles were not applied")
		return
	}
	a.parkHover()
}

// SetClickThrough toggles mouse pass-through on the campfire window.
func (a *App) SetClickThrough(on bool) {
	a.mu.Lock()
	a.clickThrough = on
	hwnd := a.hwnd
	a.mu.Unlock()

	if hwnd == 0 {
		hwnd = FindOwnWindowByTitle(FlameWindowTitle)
	}
	if hwnd != 0 {
		SetClickThrough(hwnd, on)
	}
}

// ---------------------------------------------------------------------------
// Tray
// ---------------------------------------------------------------------------

func (a *App) buildTray() {
	handle, err := a.gapp.SetSystemTray(gui.SystemTrayCfg{
		Tooltip: core.AppInfo.DisplayName(),
		IconPNG: TrayIconArt.CachedPNG(32),
		Menu:    a.trayMenu(),
		OnAction: func(id string) {
			a.onTrayAction(id)
		},
	})
	if err != nil {
		core.LogWarn("tray unavailable: " + err.Error())
		return
	}
	a.tray = handle
}

// trayMenu builds the current menu. Item IDs are stable and language-neutral;
// only the labels are localised.
func (a *App) trayMenu() []gui.NativeMenuItemCfg {
	items := []gui.NativeMenuItemCfg{
		{ID: "toggleFlame", Text: a.toggleFlameLabel()},
		{ID: "toggleAutoStart", Text: core.T("menu.autoStart"), Checked: a.Settings.AutoStart},
		{Separator: true},
	}
	items = append(items, gui.NativeMenuItemCfg{ID: "console", Text: core.T("menu.console")})
	items = append(items, gui.NativeMenuItemCfg{Separator: true})

	sizeLabels := map[core.FlameSize]string{
		core.SizeSmall:  core.T("size.small"),
		core.SizeMedium: core.T("size.medium"),
		core.SizeLarge:  core.T("size.large"),
	}
	items = append(items, gui.NativeMenuItemCfg{Text: core.T("menu.size"), Submenu: []gui.NativeMenuItemCfg{
		{ID: "size:small", Text: sizeLabels[core.SizeSmall], Checked: a.Settings.Size == core.SizeSmall},
		{ID: "size:medium", Text: sizeLabels[core.SizeMedium], Checked: a.Settings.Size == core.SizeMedium},
		{ID: "size:large", Text: sizeLabels[core.SizeLarge], Checked: a.Settings.Size == core.SizeLarge},
	}})

	items = append(items, gui.NativeMenuItemCfg{Text: core.T("menu.language"), Submenu: []gui.NativeMenuItemCfg{
		{ID: "lang:system", Text: core.T("settings.lang.system"), Checked: a.Settings.Language == core.LangSystem},
		{ID: "lang:en", Text: "English", Checked: a.Settings.Language == core.LangEnglish},
		{ID: "lang:zh-Hans", Text: check(a.Settings.Language == core.LangChineseSimplified) + "简体中文"},
		{ID: "lang:ja", Text: check(a.Settings.Language == core.LangJapanese) + "日本語"},
		{ID: "lang:ko", Text: check(a.Settings.Language == core.LangKorean) + "한국어"},
	}})

	items = append(items, gui.NativeMenuItemCfg{Separator: true})
	for i := range items {
		if items[i].Text != core.T("menu.language") {
			continue
		}
		for j := range items[i].Submenu {
			switch items[i].Submenu[j].ID {
			case "lang:zh-Hans":
				items[i].Submenu[j].Checked = a.Settings.Language == core.LangChineseSimplified
			case "lang:ja":
				items[i].Submenu[j].Checked = a.Settings.Language == core.LangJapanese
			case "lang:ko":
				items[i].Submenu[j].Checked = a.Settings.Language == core.LangKorean
			}
		}
	}
	items = append(items, gui.NativeMenuItemCfg{ID: "resetPosition", Text: core.T("menu.resetPosition")})
	items = append(items, gui.NativeMenuItemCfg{
		ID:      "toggleClickThrough",
		Text:    core.T("menu.clickThrough"),
		Checked: a.clickThrough,
	})
	items = append(items, gui.NativeMenuItemCfg{Separator: true})
	items = append(items, gui.NativeMenuItemCfg{ID: "about", Text: core.T("menu.about")})
	items = append(items, gui.NativeMenuItemCfg{ID: "project", Text: core.T("menu.project")})
	// A disabled line at the bottom: the version is information, not an action,
	// and it means you can confirm the build without opening anything.
	items = append(items, gui.NativeMenuItemCfg{Text: core.AppInfo.DisplayName(), Disabled: true})
	items = append(items, gui.NativeMenuItemCfg{Separator: true})
	items = append(items, gui.NativeMenuItemCfg{ID: "quit", Text: core.T("menu.quit")})
	return items
}

// check renders a menu checkmark prefix. Native menus here carry no check state
// of their own, so the label encodes it.
func check(on bool) string {
	if false && on {
		return "✓ "
	}
	return ""
}

func (a *App) toggleFlameLabel() string {
	if a.Settings.FlameVisible {
		return core.T("menu.hideFlame")
	}
	return core.T("menu.show")
}

func (a *App) rebuildTrayMenu() {
	if a.tray == nil {
		return
	}
	a.gapp.UpdateSystemTray(a.tray, gui.SystemTrayCfg{
		Tooltip: core.AppInfo.DisplayName(),
		IconPNG: TrayIconArt.CachedPNG(32),
		Menu:    a.trayMenu(),
		OnAction: func(id string) {
			a.onTrayAction(id)
		},
	})
}

func (a *App) onTrayAction(id string) {
	switch {
	case id == "toggleFlame":
		a.SetFlameVisible(!a.Settings.FlameVisible)
	case id == "toggleAutoStart":
		a.SetAutoStart(!a.Settings.AutoStart)
	case id == "toggleClickThrough":
		a.SetClickThrough(!a.clickThrough)
		a.rebuildTrayMenu()
	case id == "resetPosition":
		a.placeDefault()
	case id == "console":
		a.OpenConsole(ConsoleTabStats)
	case id == "about":
		a.OpenConsole(ConsoleTabAbout)
	case id == "project":
		OpenLink(core.AppInfo.ProjectUrl())
	case id == "quit":
		a.Quit()
	case hasPrefix(id, "size:"):
		a.SetFlameSize(core.FlameSizeFromRaw(id[len("size:"):]))
	case hasPrefix(id, "lang:"):
		a.SetLanguage(langFromID(id[len("lang:"):]))
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func langFromID(id string) core.AppLanguage {
	switch id {
	case "en":
		return core.LangEnglish
	case "zh-Hans":
		return core.LangChineseSimplified
	case "ja":
		return core.LangJapanese
	case "ko":
		return core.LangKorean
	default:
		return core.LangSystem
	}
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

// SetFlameVisible shows or hides the campfire window.
func (a *App) SetFlameVisible(visible bool) {
	a.flameHidden.Store(!visible)
	a.Settings.FlameVisible = visible
	a.Settings.Save()

	// Visibility is a native-window operation; changing the setting alone does
	// not hide/show a go-gui Window. Keep the window alive so the tray action can
	// toggle it repeatedly instead of closing the backend window permanently.
	if hwnd := a.flameHwnd(); hwnd != 0 {
		SetWindowVisible(hwnd, visible)
		if visible {
			a.applyPlatformTweaks()
			a.ensurePlaced()
		}
	}

	// Hiding the campfire stops its frames, and the hover test rides on those
	// frames — so the card has to be taken down explicitly or it would be left
	// hanging over a campfire that is no longer there.
	if !visible {
		a.parkHover()
	}
	if visible && a.gw != nil {
		a.gw.InvalidateLayout()
	}
	a.rebuildTrayMenu()
}

// SetAutoStart drives the Run key and keeps the setting honest.
func (a *App) SetAutoStart(enabled bool) {
	a.Settings.AutoStart = enabled
	a.Settings.Save()
	a.syncAutoStart()
	a.rebuildTrayMenu()
}

// syncAutoStart pushes the setting into the platform's login entry. When the
// write fails (group policy, permissions, a read-only config directory) the
// setting is synced back to the real state — the checkmark must reflect the
// facts.
func (a *App) syncAutoStart() {
	if core.AutoStartApply(a.Settings.AutoStart) {
		return
	}
	real := core.AutoStartEnabled()
	if real == a.Settings.AutoStart {
		return
	}
	state := "off"
	if real {
		state = "on"
	}
	core.LogWarn("autostart could not be applied; settings synced to actual state: " + state)
	a.Settings.AutoStart = real
	a.Settings.Save()
}

// SetFlameSize changes the campfire scale and re-lays-out the window.
func (a *App) SetFlameSize(size core.FlameSize) {
	a.Settings.Size = size
	a.Settings.Save()
	a.applySize(size)
	a.rebuildTrayMenu()
}

// SetFlameColor sets the flame theme colour. The fire is single-colour — there
// is no per-source tinting.
func (a *App) SetFlameColor(r, g, b float64) {
	a.Settings.FlameColor = core.AccentRGB{clamp01(r), clamp01(g), clamp01(b)}
	a.Settings.Save()
	a.fireMu.Lock()
	a.Fire.LiveSnapshot.FlameAccent = a.Settings.FlameColor
	a.Fire.BumpPaletteEpoch()
	a.fireMu.Unlock()
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// SetLanguage switches the UI language and rebuilds the tray.
func (a *App) SetLanguage(lang core.AppLanguage) {
	a.Settings.Language = lang
	a.Settings.Save()
	core.SetLanguage(lang)
	a.rebuildTrayMenu()
	// The console's title carries the translated "CodingFire Console", and its
	// tabs are rebuilt from L10n on the next frame; only the native title needs
	// to be pushed.
	a.retitleConsole()
}

// ResetColors restores the shipped palette.
//
// It resets the flame itself, not just the per-source accents. The fire is
// single-colour now — the per-source accents only colour console dots and the
// hover card's leading dots — so resetting those alone changed nothing visible
// on the desktop, which is exactly what "reset colors" appeared not to do.
func (a *App) ResetColors() {
	a.Settings.FlameColor = core.DefaultFlameAccent
	fire.SourceFlameColors.ResetAll()
	a.Settings.Save()

	a.fireMu.Lock()
	a.Fire.LiveSnapshot.FlameAccent = a.Settings.FlameColor
	a.Fire.NotifyColorsChanged()
	a.fireMu.Unlock()
}

// Rescan forces a full re-read of every source.
func (a *App) Rescan() { a.Monitor.Rescan() }

// placeDefault puts the campfire at the bottom-right of the monitor the mouse is
// on, and remembers where it landed.
//
// The monitor is picked by cursor rather than by "primary" to match the C#
// build, which used Screen.FromPoint(Cursor.Position).WorkingArea: on a
// multi-monitor desk, resetting to a screen the user is not looking at is not a
// reset.
//
// The position is only persisted once the move has actually succeeded. Recording
// a position the window never reached is what put the campfire in the top-left
// corner: the intent was saved, the move silently did nothing, and the next
// startup faithfully restored the intent while the poller recorded the truth.
func (a *App) placeDefault() {
	areaX, areaY, areaW, areaH := CursorWorkArea()
	if areaW <= 0 || areaH <= 0 {
		core.LogWarn("reset position: could not read the work area")
		return
	}

	a.mu.Lock()
	w, h := a.panelPhysW, a.panelPhysH
	a.mu.Unlock()

	margin := int(ceilF(36 * fire.DpiScale))
	x := areaX + areaW - w - margin
	y := areaY + areaH - h - margin
	if x < areaX {
		x = areaX
	}
	if y < areaY {
		y = areaY
	}

	hwnd := a.flameHwnd()
	if hwnd == 0 {
		core.LogWarn("could not find the campfire window; position not applied")
		return
	}
	if !SetWindowPosition(hwnd, x, y) {
		core.LogWarn("could not move the campfire window")
		return
	}

	a.markPlaced()
	a.persistPosition(x, y)
	core.LogInfo(fmt.Sprintf("campfire placed at (%d,%d); work area (%d,%d %dx%d), panel %dx%d",
		x, y, areaX, areaY, areaW, areaH, w, h))
}

// markPlaced records that the campfire has a deliberate position, which is what
// licenses pollPosition to start believing the window rectangle.
func (a *App) markPlaced() {
	a.mu.Lock()
	a.placed = true
	a.mu.Unlock()
}

// ensurePlaced places the campfire if that has not happened yet.
//
// Called both right after the overlay styles go on and once a second from the
// tick loop, so a window that is slow to appear still ends up in the right
// corner instead of staying wherever the toolkit left it.
func (a *App) ensurePlaced() {
	a.mu.Lock()
	placed := a.placed
	a.mu.Unlock()
	if placed {
		return
	}
	if a.flameHwnd() == 0 {
		return
	}
	a.restoreOrPlace()
}

// restoreOrPlace applies the saved position, or falls back to the default.
//
// The saved values are physical pixels from a previous session, so a monitor
// change since then can leave them off-screen. Constraining to the work area of
// the monitor the campfire is on catches that without discarding a position that
// is still valid.
func (a *App) restoreOrPlace() {
	hwnd := a.flameHwnd()
	if hwnd == 0 {
		return
	}

	// (0,0) is the origin a window has before anything positions it, and it is
	// not a place this app ever chooses: the default is the bottom-right corner,
	// and the campfire cannot be dragged there because it is click-through. A
	// saved (0,0) is therefore the fingerprint of a failed placement — treating
	// it as a real position is what pinned the window to the corner on every
	// subsequent run.
	if !a.Settings.HasPosition || (a.Settings.PanelX == 0 && a.Settings.PanelY == 0) {
		a.placeDefault()
		return
	}

	// Constrain against the monitor the campfire is already on, not the one the
	// pointer is on: the saved position is the authority for which screen this
	// belongs to.
	areaX, areaY, areaW, areaH := WorkAreaAt(a.Settings.PanelX, a.Settings.PanelY)
	if areaW <= 0 || areaH <= 0 {
		core.LogWarn("could not read the work area; saved position left as is")
		return
	}

	a.mu.Lock()
	w, h := a.panelPhysW, a.panelPhysH
	a.mu.Unlock()

	x := clampInt(a.Settings.PanelX, areaX, areaX+areaW-w)
	y := clampInt(a.Settings.PanelY, areaY, areaY+areaH-h)

	if !SetWindowPosition(hwnd, x, y) {
		core.LogWarn("could not move the campfire window")
		return
	}
	a.markPlaced()
	if x != a.Settings.PanelX || y != a.Settings.PanelY {
		a.persistPosition(x, y)
	}
}

func (a *App) persistPosition(x, y int) {
	if a.Settings.HasPosition && a.Settings.PanelX == x && a.Settings.PanelY == y {
		return
	}
	a.Settings.PanelX, a.Settings.PanelY = x, y
	a.Settings.HasPosition = true
	a.Settings.Save()
}

// pollDrag implements drag-to-move without depending on window mouse messages.
//
// The campfire's click-through mode is deliberately still on by default. A
// click-through window does not receive WM_LBUTTONDOWN/WM_MOUSEMOVE, so the
// portable way to retain the C# build's drag behavior is to sample the global
// left-button state and cursor position. The first tick with the button down
// over the campfire arms a drag; subsequent ticks move the window by the cursor
// delta until release.
func (a *App) pollDrag() {
	down := LeftButtonDown()

	a.mu.Lock()
	dragging := a.dragging
	ready := a.dragReady
	a.mu.Unlock()

	if !down {
		if dragging {
			hwnd := a.flameHwnd()
			if hwnd != 0 {
				if x, y, _, _, ok := WindowRect(hwnd); ok {
					a.persistPosition(x, y)
				}
			}
			a.mu.Lock()
			a.dragging = false
			a.dragReady = true
			a.mu.Unlock()
			return
		}
	}
	if !ready || dragging {
		// Already dragging, or the app started while the button was held.
		if !dragging {
			return
		}
	} else {
		cx, cy, ok := CursorPos()
		if !ok {
			return
		}
		hwnd := a.flameHwnd()
		if hwnd == 0 {
			return
		}
		wx, wy, ww, wh, ok := WindowRect(hwnd)
		if !ok || cx < wx || cy < wy || cx >= wx+ww || cy >= wy+wh {
			return
		}
		a.mu.Lock()
		a.dragging = true
		a.dragReady = false
		a.dragStartCursorX = cx
		a.dragStartCursorY = cy
		a.dragStartWindowX = wx
		a.dragStartWindowY = wy
		a.mu.Unlock()
		return
	}

	cx, cy, ok := CursorPos()
	if !ok {
		return
	}
	a.mu.Lock()
	x := a.dragStartWindowX + cx - a.dragStartCursorX
	y := a.dragStartWindowY + cy - a.dragStartCursorY
	a.mu.Unlock()
	if hwnd := a.flameHwnd(); hwnd != 0 {
		if SetWindowPosition(hwnd, x, y) {
			a.markPlaced()
		}
	}
}

// pollPosition notices a window move and records it.
//
// go-gui has no moved event — it emits EventResized and nothing else — so the
// only way to learn that the window moved is to ask. This runs on the same 1s
// cadence as the console refresh, and writes only when the position actually
// changed, so a stationary campfire costs one GetWindowRect per second and no
// disk I/O at all.
//
// It stays quiet until the campfire has been placed at least once. Before that
// the rectangle is just the toolkit's default origin, and recording it would
// overwrite the real position with (0,0) — which is exactly how the campfire
// ended up in the top-left corner.
func (a *App) pollPosition() {
	a.mu.Lock()
	placed := a.placed
	a.mu.Unlock()
	if !placed {
		return
	}

	hwnd := a.flameHwnd()
	if hwnd == 0 {
		return
	}
	x, y, _, _, ok := WindowRect(hwnd)
	if !ok {
		return
	}
	a.persistPosition(x, y)
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---------------------------------------------------------------------------
// Hover card
// ---------------------------------------------------------------------------

// Hover tuning.
//
// Keep the hover grace period, but refresh the small statistics card only
// twice per second. Mouse detection still runs at 20 Hz.
const (
	hoverMissFrames = 6
	hoverRefreshMs  = 500
	hoverGap        = 8

	// hoverParkedX/Y is where the card waits while it is not being shown: well
	// outside any real desktop. go-gui has no show/hide API, so "hidden" means
	// "moved somewhere nobody can see it", which also avoids the flash a
	// hide-then-show of a freshly created window would produce.
	hoverParkedX = -32000
	hoverParkedY = -32000
)

// updateHover decides whether the card should be on screen.
//
// It runs from the 20Hz tick loop rather than from mouse events. go-gui cannot
// do per-pixel hit testing, and the campfire is click-through by default, so it
// never receives a MouseEnter at all. Asking where the pointer is reproduces
// the useful part of the C# behaviour while working with click-through left on.
func (a *App) updateHover() {
	if a.probeHidden {
		return
	}
	if !a.Settings.FlameVisible {
		a.hideHover()
		return
	}

	if a.cursorOverFlame() {
		a.mu.Lock()
		a.hoverMiss = hoverMissFrames
		a.mu.Unlock()
	} else {
		a.mu.Lock()
		a.hoverMiss--
		miss := a.hoverMiss
		a.mu.Unlock()
		if miss <= 0 {
			a.hideHover()
			return
		}
	}

	// Already up: refresh the numbers, but no faster than the C# build did.
	a.mu.Lock()
	shown := a.hoverShown
	due := time.Since(a.hoverLastRefresh) >= hoverRefreshMs*time.Millisecond
	a.mu.Unlock()
	if shown && !due {
		return
	}
	a.showHover()
}

// cursorOverFlame reports whether the pointer is over the campfire panel.
//
// Unlike the original layered WinForms window, go-gui's GL window does not
// expose per-pixel hit testing. Its transparent pixels still belong to the
// window's hit-test surface, and the renderer may legitimately have alpha 0
// while the fire is in the quiet/embers state. The correct user-facing trigger
// is therefore the campfire panel rectangle: if the pointer is over the fire's
// little window, show the same usage card even when the current frame has no
// lit pixel at that exact coordinate.
func (a *App) cursorOverFlame() bool {
	hwnd := a.flameHwnd()
	if hwnd == 0 {
		return false
	}
	wx, wy, ww, wh, ok := WindowRect(hwnd)
	if !ok {
		return false
	}
	cx, cy, ok := CursorPos()
	if !ok {
		return false
	}
	return cx >= wx && cy >= wy && cx < wx+ww && cy < wy+wh
}

// showHover refreshes the card's contents and puts it beside the flame.
func (a *App) showHover() {
	w := a.hover
	if w == nil {
		return
	}
	hwnd := a.hoverHwndFor()
	if hwnd == 0 {
		return
	}

	model := a.BuildHoverModel()
	cardH := int(hoverCardHeight(model))

	// Size before positioning: the card's height depends on how many sources
	// have usage, and the placement maths needs the real height.
	if !SetWindowSize(hwnd, int(ceilF(hoverCardWidth*fire.DpiScale)), int(ceilF(float64(cardH)*fire.DpiScale))) {
		core.LogWarn("could not size the hover card window")
	}

	w.QueueCommand(func(w *gui.Window) {
		s := gui.State[hoverCardState](w)
		s.Model = model
		s.Visible = true
		w.InvalidateLayout()
	})

	x, y := a.hoverCardPosition(cardH)
	if !SetWindowPosition(hwnd, x, y) {
		core.LogWarn("could not move the hover card window")
		return
	}
	if !a.probeHidden {
		SetWindowVisible(hwnd, true)
	}

	a.mu.Lock()
	a.hoverShown = true
	a.hoverLastRefresh = time.Now()
	a.mu.Unlock()
}

// hideHover takes the card off screen, if it is up.
func (a *App) hideHover() {
	a.mu.Lock()
	shown := a.hoverShown
	a.hoverMiss = 0
	a.mu.Unlock()
	if !shown {
		return
	}
	a.parkHover()
}

// parkHover moves the card out of sight and stops it drawing a card at all.
func (a *App) parkHover() {
	a.mu.Lock()
	a.hoverShown = false
	a.hoverMiss = 0
	a.mu.Unlock()

	if w := a.hover; w != nil {
		w.QueueCommand(func(w *gui.Window) {
			s := gui.State[hoverCardState](w)
			s.Visible = false
			w.InvalidateLayout()
		})
	}
	if hwnd := a.hoverHwndFor(); hwnd != 0 {
		SetWindowVisible(hwnd, false)
		SetWindowPosition(hwnd, hoverParkedX, hoverParkedY)
	}
}

// hoverCardPosition centres the card on the flame, above its tip, and flips it
// below when there is no room above — the C# build's placement, in physical
// pixels.
func (a *App) hoverCardPosition(cardLogH int) (int, int) {
	hwnd := a.flameHwnd()
	if hwnd == 0 {
		return hoverParkedX, hoverParkedY
	}
	fx, fy, fw, fh, ok := WindowRect(hwnd)
	if !ok {
		return hoverParkedX, hoverParkedY
	}

	scale := fire.DpiScale
	cardW := int(ceilF(hoverCardWidth * scale))
	cardH := int(ceilF(float64(cardLogH) * scale))
	gap := int(ceilF(hoverGap * scale))

	// FlameTipY is the flame's top edge inside the panel, so the card hangs just
	// off the tip rather than off the window's transparent top margin.
	a.mu.Lock()
	tipY := a.Renderer.FlameTipY
	a.mu.Unlock()

	x := fx + (fw-cardW)/2
	y := fy + tipY - cardH - gap

	// The work area of the campfire's own monitor, so a card for a campfire on
	// the second screen is not clamped back onto the first.
	areaX, areaY, areaW, areaH := WorkAreaAt(fx, fy)
	if areaW > 0 && areaH > 0 {
		if y < areaY {
			y = fy + fh + gap // no room above: flip below the campfire
		}
		x = clampInt(x, areaX, areaX+areaW-cardW)
		y = clampInt(y, areaY, areaY+areaH-cardH)
	}
	return x, y
}

// hoverHwndFor returns the card window's handle, caching it once found and
// re-finding it if the cached one has died.
func (a *App) hoverHwndFor() uintptr {
	a.mu.Lock()
	hwnd := a.hoverHwnd
	a.mu.Unlock()
	if hwnd != 0 && WindowAlive(hwnd) {
		return hwnd
	}
	hwnd = FindOwnWindowByTitle(HoverWindowTitle)
	if hwnd != 0 {
		a.mu.Lock()
		a.hoverHwnd = hwnd
		a.mu.Unlock()
	}
	return hwnd
}

// Quit shuts everything down.
func (a *App) Quit() {
	if a.quitting {
		return
	}
	a.quitting = true
	a.shutdown()
}

func (a *App) shutdown() {
	a.stopOnce.Do(func() {
		close(a.stopCh)
		if a.Monitor != nil {
			a.Monitor.Stop()
		}
		if a.Store != nil {
			a.Store.Flush()
			a.Store.Close()
		}

		// ExitOnTrayRemoved only ends the backend loop after every window is
		// gone. Removing the tray icon alone therefore leaves the flame window
		// alive on the desktop. Close every app-owned window as part of Quit,
		// including the parked hover window and an open console.
		if a.console != nil {
			a.console.Close()
		}
		if a.hover != nil {
			a.hover.Close()
		}
		if a.gw != nil {
			a.gw.Close()
		}
		if a.gapp != nil && a.tray != nil {
			a.gapp.RemoveSystemTray(a.tray)
			a.tray = nil
		}
	})
}

// hoverModel summarises today's usage for the hover card.
type HoverModel struct {
	TodayTokens     int
	TokensPerSecond float64
	ShowLiveRate    bool
	UpdatedAt       core.Time
	HasUpdated      bool
	Rows            []HoverRow
	// Hourly is today's 24 buckets, drawn as the mini timeline. Always the
	// full day, not a window: the point is the shape of today.
	Hourly []core.HourlyUsage
}

// HoverRow is one source line on the hover card.
type HoverRow struct {
	Source    core.UsageSource
	Tokens    int
	Estimated bool
}

// BuildHoverModel assembles the hover card contents.
func (a *App) BuildHoverModel() HoverModel {
	a.fireMu.Lock()
	rate := a.Fire.TokensPerSecond
	a.fireMu.Unlock()
	model := HoverModel{
		TodayTokens:     a.Monitor.TodayTokens(),
		TokensPerSecond: rate,
		ShowLiveRate:    a.Settings.ShowLiveRate,
		Hourly:          a.Monitor.TodayHourly(),
	}

	for _, st := range a.Monitor.Statuses() {
		if st.HasLastRead && (!model.HasUpdated || st.LastReadAt.After(model.UpdatedAt)) {
			model.UpdatedAt = st.LastReadAt
			model.HasUpdated = true
		}
	}

	bySource := a.Monitor.TodayBySource()
	for _, src := range core.UsageSourcesAll {
		tokens := bySource[src]
		if tokens <= 0 {
			continue
		}
		model.Rows = append(model.Rows, HoverRow{Source: src, Tokens: tokens})
	}
	// Highest usage first reads better.
	sortRowsDesc(model.Rows)
	return model
}

func sortRowsDesc(rows []HoverRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].Tokens > rows[j-1].Tokens; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// consoleStatus is a small helper the console uses to render its header.
func (a *App) consoleStatus() string {
	return fmt.Sprintf("%s — %s", core.AppInfo.DisplayName(), core.T("stats.today"))
}
