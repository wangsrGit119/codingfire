//go:build linux && !android

package ui

import (
	"encoding/binary"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/shape"
	"github.com/jezek/xgb/xproto"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// The Linux half of the overlay-window layer, spoken over X11.
//
// go-gui's X11 backend gives us a transparent frameless window, but it has no
// always-on-top, no click-through and no move/resize, and — unlike Win32 — the
// window ID it registers is the real X window, so a handle is available. What
// is missing is the EWMH conversation with the window manager, which is what
// this file adds.
//
// Everything here talks over a connection of our own rather than reusing
// go-gui's, for two reasons: the backend keeps its conn private, and the
// window manager answers on the root window of whichever connection asks, so a
// second connection costs one file descriptor and removes any question about
// who owns the request queue.
//
// The whole file degrades to a no-op when there is no display to talk to: a
// headless run of --dump must not fail because $DISPLAY is unset.

// _NET_WM_STATE actions, in the order the protocol numbers them.
const (
	netWmStateRemove = 0
	netWmStateAdd    = 1
)

// x11Button1Mask is the Button1Press bit in the QueryPointer mask.
const x11Button1Mask = 1 << 8

// x11Display is our connection to the X server plus the atoms and extension
// state that are fixed for its lifetime. nil means "could not connect", which
// every caller treats as "no window management available".
type x11Display struct {
	conn      *xgb.Conn
	root      xproto.Window
	haveRandr bool
	haveShape bool

	atomNetWmPid               xproto.Atom
	atomNetWmName              xproto.Atom
	atomUtf8String             xproto.Atom
	atomNetWmState             xproto.Atom
	atomNetWmStateAbove        xproto.Atom
	atomNetWmStateSkipTaskbar  xproto.Atom
	atomNetWmStateSkipPager    xproto.Atom
	atomNetWmWindowType        xproto.Atom
	atomNetWmWindowTypeUtility xproto.Atom
	atomNetWorkArea            xproto.Atom
	atomNetActiveWindow        xproto.Atom
	atomResourceManager        xproto.Atom
}

var (
	x11Once sync.Once
	x11Dpy  *x11Display
)

// x11 returns the shared display connection, opening it on first use.
func x11() *x11Display {
	x11Once.Do(func() { x11Dpy = openX11() })
	return x11Dpy
}

func openX11() *x11Display {
	conn, err := xgb.NewConn()
	if err != nil {
		core.LogWarn("x11: no display connection; window management is off: " + err.Error())
		return nil
	}
	setup := xproto.Setup(conn)
	if setup == nil || len(setup.Roots) == 0 {
		conn.Close()
		core.LogWarn("x11: display has no screens; window management is off")
		return nil
	}
	d := &x11Display{conn: conn, root: setup.DefaultScreen(conn).Root}
	// Both extensions are optional. shape is what makes click-through
	// possible; randr is only a better source of monitor geometry, and every
	// caller has a fallback that works without it.
	d.haveRandr = randr.Init(conn) == nil
	d.haveShape = shape.Init(conn) == nil

	d.atomNetWmPid = d.intern("_NET_WM_PID")
	d.atomNetWmName = d.intern("_NET_WM_NAME")
	d.atomUtf8String = d.intern("UTF8_STRING")
	d.atomNetWmState = d.intern("_NET_WM_STATE")
	d.atomNetWmStateAbove = d.intern("_NET_WM_STATE_ABOVE")
	d.atomNetWmStateSkipTaskbar = d.intern("_NET_WM_STATE_SKIP_TASKBAR")
	d.atomNetWmStateSkipPager = d.intern("_NET_WM_STATE_SKIP_PAGER")
	d.atomNetWmWindowType = d.intern("_NET_WM_WINDOW_TYPE")
	d.atomNetWmWindowTypeUtility = d.intern("_NET_WM_WINDOW_TYPE_UTILITY")
	d.atomNetWorkArea = d.intern("_NET_WORKAREA")
	d.atomNetActiveWindow = d.intern("_NET_ACTIVE_WINDOW")
	d.atomResourceManager = d.intern("RESOURCE_MANAGER")

	// The connection outlives every call site, so the cached connection is
	// closed by the process exiting rather than by us.
	return d
}

func (d *x11Display) intern(name string) xproto.Atom {
	reply, err := xproto.InternAtom(d.conn, false, uint16(len(name)), name).Reply()
	if err != nil || reply == nil {
		return 0
	}
	return reply.Atom
}

// ---------------------------------------------------------------------------
// Window discovery
// ---------------------------------------------------------------------------

// Bounds on the window-tree walk. A reparenting window manager adds one level
// (root -> frame -> client) and a compositor may add another, so four is
// generous; the caps matter because the walk runs on every lookup and a
// pathological tree must not turn into an unbounded one.
const (
	maxWindowTreeDepth = 4
	maxWindowTreeNodes = 4096
)

// FindOwnWindows lists this process's windows, including windows that are still
// hidden.
//
// Ownership is decided by _NET_WM_PID, the EWMH convention every toolkit
// follows. X core has no notion of a window's owner, so without that property
// there is nothing to distinguish our windows from every other client's — the
// patched go-gui backend sets it when it creates a window (see
// third_party/go-gui/gui/backend/gl/platform_x11.go).
//
// The walk is breadth-first rather than a scan of the root's direct children,
// because a reparenting window manager moves every managed window under a frame
// of its own: under such a WM our windows are grandchildren of the root, and a
// direct-child scan finds nothing at all.
//
// Visibility is deliberately not part of the test, matching the Win32
// enumeration: the backend maps the window before OnInit runs, and the campfire
// has to be findable and positionable while it is still where the toolkit left
// it.
func FindOwnWindows() []OwnWindow {
	d := x11()
	if d == nil {
		return nil
	}
	pid := uint32(os.Getpid())
	out := make([]OwnWindow, 0, 16)

	level := []xproto.Window{d.root}
	visited := 0
	for depth := 0; depth < maxWindowTreeDepth && len(level) > 0; depth++ {
		var next []xproto.Window
		for _, w := range level {
			if visited >= maxWindowTreeNodes {
				break
			}
			visited++
			if w != d.root && d.cardinalProperty(w, d.atomNetWmPid) == pid {
				out = append(out, OwnWindow{Hwnd: uintptr(w), Title: d.windowTitle(w)})
				// Nothing we own has children of its own — go-gui creates
				// only top-level windows — so this branch is a leaf.
				continue
			}
			tree, err := xproto.QueryTree(d.conn, w).Reply()
			if err != nil || tree == nil {
				continue
			}
			next = append(next, tree.Children...)
		}
		level = next
	}
	return out
}

// FindOwnWindowByTitle returns the first own window whose title matches.
func FindOwnWindowByTitle(title string) uintptr {
	for _, w := range FindOwnWindows() {
		if w.Title == title {
			return w.Hwnd
		}
	}
	return 0
}

// windowTitle reads _NET_WM_NAME and falls back to the older WM_NAME.
//
// Both matter: go-gui writes WM_NAME as STRING (platform_x11.go,
// setWindowTitle), while a window manager or a compositor may rewrite the pair
// as UTF8_STRING. Our titles are ASCII, so either spelling compares equal.
func (d *x11Display) windowTitle(w xproto.Window) string {
	if s := d.textProperty(w, d.atomNetWmName, d.atomUtf8String); s != "" {
		return s
	}
	return d.textProperty(w, xproto.AtomWmName, xproto.AtomString)
}

// textProperty reads a format-8 property, requiring the type to match so a
// property of the same name in a different encoding is not misread as text.
func (d *x11Display) textProperty(w xproto.Window, prop, typ xproto.Atom) string {
	if prop == 0 || typ == 0 {
		return ""
	}
	reply, err := xproto.GetProperty(d.conn, false, w, prop, typ, 0, 1<<16).Reply()
	if err != nil || reply == nil || reply.Format != 8 || reply.Type == xproto.AtomNone {
		return ""
	}
	return string(reply.Value)
}

// cardinalProperty reads a single format-32 property, returning 0 when it is
// absent or not a CARDINAL.
func (d *x11Display) cardinalProperty(w xproto.Window, prop xproto.Atom) uint32 {
	if prop == 0 {
		return 0
	}
	reply, err := xproto.GetProperty(d.conn, false, w, prop, xproto.AtomCardinal, 0, 1).Reply()
	if err != nil || reply == nil || reply.Format != 32 || reply.Type == xproto.AtomNone {
		return 0
	}
	if len(reply.Value) < 4 {
		return 0
	}
	// 32-bit property values arrive in the client's byte order, which for a
	// local connection is the machine's.
	return binary.NativeEndian.Uint32(reply.Value[:4])
}

// ---------------------------------------------------------------------------
// Window manager conversation (EWMH)
// ---------------------------------------------------------------------------

// sendRootMessage posts a ClientMessage to the root window for the window
// manager to pick up. Requests to the WM travel this way rather than as direct
// requests, because the WM is a separate client that has selected
// SubstructureRedirect on the root.
func (d *x11Display) sendRootMessage(msgType xproto.Atom, win xproto.Window, data []uint32) bool {
	if msgType == 0 || len(data) != 5 {
		return false
	}
	ev := xproto.ClientMessageEvent{
		Format: 32,
		Window: win,
		Type:   msgType,
		Data:   xproto.ClientMessageDataUnionData32New(data),
	}
	// Deliberately unchecked: this is fire-and-forget, and the cookie would
	// only report whether the server accepted the event, never whether the
	// window manager acted on it. Sync is skipped for the same reason — it
	// would be a round trip per call for no answer we can use.
	xproto.SendEvent(d.conn, false, d.root,
		xproto.EventMaskSubstructureRedirect|xproto.EventMaskSubstructureNotify,
		string(ev.Bytes()))
	return true
}

// setState adds or removes one _NET_WM_STATE entry.
func (d *x11Display) setState(win xproto.Window, atom xproto.Atom, on bool) bool {
	if atom == 0 || d.atomNetWmState == 0 {
		return false
	}
	action := uint32(netWmStateRemove)
	if on {
		action = netWmStateAdd
	}
	return d.sendRootMessage(d.atomNetWmState, win, []uint32{action, uint32(atom), 0, 0, 0})
}

// setWindowType writes _NET_WM_WINDOW_TYPE.
//
// Best-effort by nature: a window manager reads the type when it first manages
// the window, and go-gui maps ours before OnInit runs, so a WM that ignores a
// late change will keep treating the campfire as an ordinary window. The
// _NET_WM_STATE bits set alongside it are what actually keep it out of the
// taskbar on such a WM.
func (d *x11Display) setWindowType(win xproto.Window, typ xproto.Atom) bool {
	if d.atomNetWmWindowType == 0 || typ == 0 {
		return false
	}
	buf := make([]byte, 4)
	binary.NativeEndian.PutUint32(buf, uint32(typ))
	xproto.ChangeProperty(d.conn, xproto.PropModeReplace, win,
		d.atomNetWmWindowType, xproto.AtomAtom, 32, 1, buf)
	return true
}

// SetTopMost pins or unpins a window above all normal windows.
func SetTopMost(hwnd uintptr, on bool) bool {
	d := x11()
	if d == nil || hwnd == 0 {
		return false
	}
	return d.setState(xproto.Window(hwnd), d.atomNetWmStateAbove, on)
}

// SetClickThrough makes a window ignore the mouse so clicks reach whatever is
// behind it.
//
// X11 has no style bit for this. The equivalent is to empty the window's input
// shape: the server then routes pointer events past the window entirely, which
// is exactly what the campfire needs and is also why the app polls the cursor
// instead of waiting for mouse events (see the drag loop in app.go).
func SetClickThrough(hwnd uintptr, on bool) bool {
	d := x11()
	if d == nil || hwnd == 0 || !d.haveShape {
		return false
	}
	win := xproto.Window(hwnd)

	var rects []xproto.Rectangle
	if !on {
		// Restoring means covering the whole window again; there is no
		// "reset to default" for an input shape.
		g, err := xproto.GetGeometry(d.conn, xproto.Drawable(win)).Reply()
		if err != nil || g == nil {
			return false
		}
		rects = []xproto.Rectangle{{X: 0, Y: 0, Width: g.Width, Height: g.Height}}
	}
	// shape.Rectangles with an empty list sets an empty region.
	return shape.Rectangles(d.conn, shape.SoSet, shape.SkInput, 0, win, 0, 0, rects).Check() == nil
}

// ApplyOverlayStyles turns an existing window into a desktop overlay: on top,
// absent from the taskbar and the alt-tab list, and optionally click-through.
//
// This is the X11 reading of the Win32 WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE |
// WS_EX_TRANSPARENT combination.
func ApplyOverlayStyles(hwnd uintptr, topMost, clickThrough bool) bool {
	d := x11()
	if d == nil || hwnd == 0 {
		return false
	}
	win := xproto.Window(hwnd)

	d.setState(win, d.atomNetWmStateSkipTaskbar, true)
	d.setState(win, d.atomNetWmStateSkipPager, true)
	if topMost {
		d.setState(win, d.atomNetWmStateAbove, true)
	}
	d.setWindowType(win, d.atomNetWmWindowTypeUtility)
	if clickThrough {
		SetClickThrough(hwnd, true)
	}
	return true
}

// ApplyOverlayWindowStyles finds an overlay window by title and applies the
// overlay styles to it, returning its handle.
func ApplyOverlayWindowStyles(title string, topMost, clickThrough bool) uintptr {
	hwnd := FindOwnWindowByTitle(title)
	if hwnd == 0 {
		return 0
	}
	if !ApplyOverlayStyles(hwnd, topMost, clickThrough) {
		core.LogWarn("could not apply overlay styles to " + title)
	}
	return hwnd
}

// GuardOverlayWindow re-asserts the campfire's always-on-top hint and restores
// a window something else hid.
//
// X11 has no Z-order query — stacking belongs to the window manager — so this
// cannot tell that another always-on-top window has displaced the campfire the
// way the Windows version can. It re-sends _NET_WM_STATE_ABOVE instead, which a
// window manager that already has the state set ignores, and that costs one
// client message a second.
func GuardOverlayWindow(hwnd uintptr, wantVisible bool) (bool, string) {
	if !WindowAlive(hwnd) {
		return false, ""
	}
	if !wantVisible {
		return true, ""
	}
	if !WindowVisible(hwnd) {
		SetWindowVisible(hwnd, true)
		SetTopMost(hwnd, true)
		return true, "had been hidden"
	}
	SetTopMost(hwnd, true)
	return true, ""
}

// SetWindowVisible shows or hides a window.
//
// Unmapping a managed window makes the window manager withdraw it, and mapping
// it again lets the WM re-run its placement, so the caller is expected to
// re-assert the position afterwards — which app.go does for the hover card
// (position, then show).
func SetWindowVisible(hwnd uintptr, visible bool) bool {
	d := x11()
	if d == nil || hwnd == 0 {
		return false
	}
	win := xproto.Window(hwnd)
	if visible {
		xproto.MapWindow(d.conn, win)
	} else {
		xproto.UnmapWindow(d.conn, win)
	}
	return true
}

// ActivateWindow asks the window manager to focus a window.
//
// Like SetForegroundWindow on Windows, this is a request the WM is free to
// refuse — focus stealing prevention will normally refuse it for a window the
// user did not just click. A refusal is not an error. Raising the window
// directly is the fallback, since that at least puts it above its siblings.
func ActivateWindow(hwnd uintptr) bool {
	d := x11()
	if d == nil || hwnd == 0 {
		return false
	}
	win := xproto.Window(hwnd)
	d.sendRootMessage(d.atomNetActiveWindow, win, []uint32{2 /* source: application */, 0, 0, 0, 0})
	xproto.ConfigureWindow(d.conn, win, xproto.ConfigWindowStackMode, []uint32{xproto.StackModeAbove})
	return true
}

// SetWindowSize resizes a window in physical pixels.
//
// go-gui has no resize API, so the flame size menu has nowhere else to go.
// ConfigureWindow is a request the WM sees as a client-side resize; the GL
// backend picks the change up through ConfigureNotify and re-derives its
// sizes, the same way the Win32 backend handles WM_SIZE.
func SetWindowSize(hwnd uintptr, width, height int) bool {
	d := x11()
	if d == nil || hwnd == 0 || width <= 0 || height <= 0 {
		return false
	}
	xproto.ConfigureWindow(d.conn, xproto.Window(hwnd),
		xproto.ConfigWindowWidth|xproto.ConfigWindowHeight,
		[]uint32{uint32(width), uint32(height)})
	return true
}

// SetWindowPosition moves a window without resizing it.
//
// X and Y are 16-bit signed on the wire, so a monitor left of or above the
// primary one — a negative coordinate — round-trips correctly through the
// CARD32 value list as long as the low 16 bits are preserved, which is what
// the int32 conversion does.
func SetWindowPosition(hwnd uintptr, x, y int) bool {
	d := x11()
	if d == nil || hwnd == 0 {
		return false
	}
	xproto.ConfigureWindow(d.conn, xproto.Window(hwnd),
		xproto.ConfigWindowX|xproto.ConfigWindowY,
		[]uint32{uint32(int32(x)), uint32(int32(y))})
	return true
}

// WindowRect returns a window's screen rectangle in physical pixels.
//
// TranslateCoordinates rather than GetGeometry: under a reparenting window
// manager the window's parent is the WM frame, so its own geometry is
// frame-relative and would be wrong by the frame's position and border.
func WindowRect(hwnd uintptr) (x, y, w, h int, ok bool) {
	d := x11()
	if d == nil || hwnd == 0 {
		return 0, 0, 0, 0, false
	}
	win := xproto.Window(hwnd)
	t, err := xproto.TranslateCoordinates(d.conn, win, d.root, 0, 0).Reply()
	if err != nil || t == nil {
		return 0, 0, 0, 0, false
	}
	g, err := xproto.GetGeometry(d.conn, xproto.Drawable(win)).Reply()
	if err != nil || g == nil {
		return 0, 0, 0, 0, false
	}
	return int(t.DstX), int(t.DstY), int(g.Width), int(g.Height), true
}

// WindowAlive reports whether a handle still refers to a live window.
func WindowAlive(hwnd uintptr) bool {
	d := x11()
	if d == nil || hwnd == 0 {
		return false
	}
	attrs, err := xproto.GetWindowAttributes(d.conn, xproto.Window(hwnd)).Reply()
	return err == nil && attrs != nil
}

// WindowVisible reports the native visibility state.
func WindowVisible(hwnd uintptr) bool {
	d := x11()
	if d == nil || hwnd == 0 {
		return false
	}
	attrs, err := xproto.GetWindowAttributes(d.conn, xproto.Window(hwnd)).Reply()
	if err != nil || attrs == nil {
		return false
	}
	return attrs.MapState == xproto.MapStateViewable
}

// ---------------------------------------------------------------------------
// Pointer, monitors and the shell
// ---------------------------------------------------------------------------

// CursorPos returns the mouse position in physical screen pixels.
func CursorPos() (x, y int, ok bool) {
	d := x11()
	if d == nil {
		return 0, 0, false
	}
	r, err := xproto.QueryPointer(d.conn, d.root).Reply()
	if err != nil || r == nil {
		return 0, 0, false
	}
	return int(r.RootX), int(r.RootY), true
}

// LeftButtonDown reads the global mouse button state.
//
// Like the Win32 GetAsyncKeyState version this is independent of window
// hit-testing, which is the point: the campfire is click-through by default, so
// it never receives the button messages a drag would otherwise be built on.
func LeftButtonDown() bool {
	d := x11()
	if d == nil {
		return false
	}
	r, err := xproto.QueryPointer(d.conn, d.root).Reply()
	if err != nil || r == nil {
		return false
	}
	return r.Mask&x11Button1Mask != 0
}

// monitor is a rectangle on the desktop, in root coordinates.
type monitor struct{ x, y, w, h int }

func (m monitor) contains(px, py int) bool {
	return px >= m.x && px < m.x+m.w && py >= m.y && py < m.y+m.h
}

// intersect returns the overlap of two rectangles, and false when they do not
// overlap at all.
func intersect(a, b monitor) (monitor, bool) {
	x0, y0 := max(a.x, b.x), max(a.y, b.y)
	x1, y1 := min(a.x+a.w, b.x+b.w), min(a.y+a.h, b.y+b.h)
	if x1 <= x0 || y1 <= y0 {
		return monitor{}, false
	}
	return monitor{x: x0, y: y0, w: x1 - x0, h: y1 - y0}, true
}

// monitors lists the active RandR monitors, or nil when RandR is unavailable
// or too old for the monitors request (RandR 1.5, 2013).
func (d *x11Display) monitors() []monitor {
	if !d.haveRandr {
		return nil
	}
	reply, err := randr.GetMonitors(d.conn, d.root, false).Reply()
	if err != nil || reply == nil || len(reply.Monitors) == 0 {
		return nil
	}
	out := make([]monitor, 0, len(reply.Monitors))
	for _, m := range reply.Monitors {
		if m.Width == 0 || m.Height == 0 {
			continue // disconnected output
		}
		out = append(out, monitor{
			x: int(m.X), y: int(m.Y), w: int(m.Width), h: int(m.Height),
		})
	}
	return out
}

// monitorAt returns the monitor containing a point.
func (d *x11Display) monitorAt(px, py int) (monitor, bool) {
	ms := d.monitors()
	// The primary monitor is preferred over "first in the list" for a point
	// that is on the seam between two, so the answer does not depend on the
	// order RandR happened to report.
	if p, ok := d.primaryMonitor(ms); ok && p.contains(px, py) {
		return p, true
	}
	for _, m := range ms {
		if m.contains(px, py) {
			return m, true
		}
	}
	return monitor{}, false
}

// primaryMonitor picks the primary monitor out of a list, falling back to the
// first entry.
func (d *x11Display) primaryMonitor(ms []monitor) (monitor, bool) {
	if ms == nil {
		ms = d.monitors()
	}
	if len(ms) == 0 {
		return monitor{}, false
	}
	if d.haveRandr {
		if reply, err := randr.GetMonitors(d.conn, d.root, false).Reply(); err == nil && reply != nil {
			for _, m := range reply.Monitors {
				if m.Primary && m.Width > 0 && m.Height > 0 {
					return monitor{int(m.X), int(m.Y), int(m.Width), int(m.Height)}, true
				}
			}
		}
	}
	return ms[0], true
}

// netWorkArea reads desktop 0's entry in _NET_WORKAREA: the screen with the
// panels and docked struts subtracted.
//
// It is a desktop-wide rectangle, not a per-monitor one, so on a multi-monitor
// desk it has to be clipped to the monitor in question — see workAreaAt.
func (d *x11Display) netWorkArea() (monitor, bool) {
	if d.atomNetWorkArea == 0 {
		return monitor{}, false
	}
	reply, err := xproto.GetProperty(d.conn, false, d.root,
		d.atomNetWorkArea, xproto.AtomCardinal, 0, 4).Reply()
	if err != nil || reply == nil || reply.Format != 32 ||
		reply.Type == xproto.AtomNone || len(reply.Value) < 16 {
		return monitor{}, false
	}
	// Four CARDINALs per desktop, desktop 0 first, in the client's byte
	// order.
	v := reply.Value[:16]
	get := func(i int) int {
		return int(binary.NativeEndian.Uint32(v[i*4 : i*4+4]))
	}
	m := monitor{get(0), get(1), get(2), get(3)}
	if m.w <= 0 || m.h <= 0 {
		return monitor{}, false
	}
	return m, true
}

// rootRect is the whole screen, used when nothing better is available.
func (d *x11Display) rootRect() (monitor, bool) {
	g, err := xproto.GetGeometry(d.conn, xproto.Drawable(d.root)).Reply()
	if err != nil || g == nil || g.Width == 0 || g.Height == 0 {
		return monitor{}, false
	}
	return monitor{0, 0, int(g.Width), int(g.Height)}, true
}

// PrimaryWorkArea returns the primary monitor's usable area: the screen minus
// the panels, which is what "bottom-right" has to mean if the campfire is not
// to sit under the clock.
func PrimaryWorkArea() (x, y, w, h int) {
	d := x11()
	if d == nil {
		return 0, 0, 0, 0
	}
	wa, haveWA := d.netWorkArea()
	if mon, ok := d.primaryMonitor(nil); ok {
		// Neither source is the answer on its own: RandR knows where the
		// monitors are but nothing about panels, and _NET_WORKAREA knows
		// about panels but not about monitors.
		if haveWA {
			if r, ok := intersect(mon, wa); ok {
				return r.x, r.y, r.w, r.h
			}
		}
		return mon.x, mon.y, mon.w, mon.h
	}
	if haveWA {
		return wa.x, wa.y, wa.w, wa.h
	}
	if r, ok := d.rootRect(); ok {
		return r.x, r.y, r.w, r.h
	}
	return 0, 0, 0, 0
}

// WorkAreaAt returns the usable area of the monitor containing a point. It is
// the placement counterpart to CursorWorkArea: once the campfire has a
// position, the monitor it lives on — not the one the mouse happens to be on —
// is what constrains it.
func WorkAreaAt(ptX, ptY int) (x, y, w, h int) {
	d := x11()
	if d == nil {
		return 0, 0, 0, 0
	}
	mon, ok := d.monitorAt(ptX, ptY)
	if !ok {
		return PrimaryWorkArea()
	}
	if wa, ok := d.netWorkArea(); ok {
		if r, ok := intersect(mon, wa); ok {
			return r.x, r.y, r.w, r.h
		}
	}
	return mon.x, mon.y, mon.w, mon.h
}

// CursorWorkArea returns the usable area of the monitor the mouse is on.
//
// This is what the C# build used for "reset position" (Screen.FromPoint(
// Cursor.Position).WorkingArea), and it matters on a multi-monitor desk:
// parking the campfire on the primary monitor when the user is working on the
// second one is not a reset, it is a disappearance.
func CursorWorkArea() (x, y, w, h int) {
	ptX, ptY, ok := CursorPos()
	if !ok {
		return PrimaryWorkArea()
	}
	return WorkAreaAt(ptX, ptY)
}

// OpenURL hands a URL to the desktop's default handler.
//
// xdg-open is the freedesktop equivalent of ShellExecute: it resolves the
// scheme through the desktop's own settings, so it opens whatever browser the
// user actually chose. The child is reaped rather than left to become a zombie
// for the lifetime of the app, and a launch failure is only ever logged — a
// browser that will not open must never take the app down.
func OpenURL(url string) bool {
	if url == "" {
		return false
	}
	cmd := exec.Command("xdg-open", url)
	if err := cmd.Start(); err != nil {
		core.LogWarn("could not open " + url + ": " + err.Error())
		return false
	}
	go func() { _ = cmd.Wait() }()
	return true
}

// ---------------------------------------------------------------------------
// Display scale
// ---------------------------------------------------------------------------

// SystemDPIScale returns the scale of the monitor covering the screen origin.
//
// It is kept as the fallback for the moments before a window handle exists,
// mirroring the Win32 shim; the value it returns is the same one the GL backend
// derives for a window created at 0,0 (backend/gl/platform_x11.go), so the
// renderer and the toolkit agree on the scale from the first frame.
func SystemDPIScale() float64 { return x11DPIScale(0, 0) }

// WindowDPIScale returns the scale of the monitor a window is on, which is the
// scale go-gui itself uses. This is the authority, not SystemDPIScale: a
// window dragged to a display with a different DPI has to be re-scaled, and
// the backend re-derives its own scale from the CRTC the window moved to
// (backend/gl/dpi_x11.go, maybeRescaleDPI).
func WindowDPIScale(hwnd uintptr) float64 {
	x, y := 0, 0
	if hwnd != 0 {
		if wx, wy, _, _, ok := WindowRect(hwnd); ok {
			x, y = wx, wy
		}
	}
	return x11DPIScale(x, y)
}

// Plausible bounds for a physical display DPI, matching the GL backend: a value
// outside this range usually means a bogus EDID physical size.
const (
	minPlausibleDPI = 50
	maxPlausibleDPI = 400
)

// x11DPIScale derives a UI scale for the monitor containing a root-relative
// point, using that monitor's physical size, and falls back to the desktop's
// Xft.dpi setting.
//
// This deliberately reproduces backend/gl/dpi_x11.go rather than approximating
// it. fire.DpiScale scales the rendered image and go-gui scales the window, so
// any disagreement between the two shows up as pixel art that is not 1:1.
func x11DPIScale(px, py int) float64 {
	d := x11()
	if d == nil {
		return 1
	}
	if s, ok := d.randrDPIScale(px, py); ok {
		return s
	}
	if s, ok := d.xftDPIScale(); ok {
		return s
	}
	return 1
}

// randrDPIScale derives the scale from the monitor's pixel and millimetre
// sizes. ok is false when RandR data is missing or implausible.
func (d *x11Display) randrDPIScale(px, py int) (float64, bool) {
	if !d.haveRandr {
		return 0, false
	}
	res, err := randr.GetScreenResourcesCurrent(d.conn, d.root).Reply()
	if err != nil || res == nil {
		return 0, false
	}
	for _, crtc := range res.Crtcs {
		info, ierr := randr.GetCrtcInfo(d.conn, crtc, res.ConfigTimestamp).Reply()
		if ierr != nil || info == nil || info.Width == 0 || info.Height == 0 {
			continue // disabled or disconnected CRTC
		}
		if px < int(info.X) || px >= int(info.X)+int(info.Width) ||
			py < int(info.Y) || py >= int(info.Y)+int(info.Height) {
			continue
		}
		if len(info.Outputs) == 0 {
			return 0, false
		}
		out, oerr := randr.GetOutputInfo(d.conn, info.Outputs[0], res.ConfigTimestamp).Reply()
		if oerr != nil || out == nil {
			return 0, false
		}
		return crtcDPI(info, out)
	}
	return 0, false
}

// crtcDPI averages the horizontal and vertical DPI of one CRTC. ok is false
// when no physical dimension is reported or the result is implausible.
func crtcDPI(info *randr.GetCrtcInfoReply, out *randr.GetOutputInfoReply) (float64, bool) {
	const mmPerInch = 25.4
	// A 90/270 degree rotation swaps the pixel axes relative to physical size.
	pw, ph := float64(info.Width), float64(info.Height)
	if info.Rotation&(randr.RotationRotate90|randr.RotationRotate270) != 0 {
		pw, ph = ph, pw
	}
	var sum float64
	var n int
	if out.MmWidth > 0 {
		sum += pw / (float64(out.MmWidth) / mmPerInch)
		n++
	}
	if out.MmHeight > 0 {
		sum += ph / (float64(out.MmHeight) / mmPerInch)
		n++
	}
	if n == 0 {
		return 0, false
	}
	dpi := sum / float64(n)
	if dpi < minPlausibleDPI || dpi > maxPlausibleDPI {
		return 0, false
	}
	return dpi / 96.0, true
}

// xftDPIScale reads the Xft.dpi entry out of the root RESOURCE_MANAGER
// property, which is where a desktop publishes the scale the user asked for.
func (d *x11Display) xftDPIScale() (float64, bool) {
	if d.atomResourceManager == 0 {
		return 0, false
	}
	reply, err := xproto.GetProperty(d.conn, false, d.root, d.atomResourceManager,
		xproto.AtomString, 0, 1<<16).Reply()
	if err != nil || reply == nil || len(reply.Value) == 0 {
		return 0, false
	}
	for line := range strings.SplitSeq(string(reply.Value), "\n") {
		k, val, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(k) != "Xft.dpi" {
			continue
		}
		dpi, convErr := strconv.Atoi(strings.TrimSpace(val))
		if convErr != nil || dpi <= 0 {
			return 0, false
		}
		return float64(dpi) / 96.0, true
	}
	return 0, false
}
