//go:build windows

package ui

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// go-gui gives us a transparent frameless window, but it has no always-on-top
// and no click-through, and it keeps the HWND unexported. Both are load-bearing
// for a desktop campfire: it has to float above other windows and must not
// steal clicks from whatever is underneath.
//
// So we reach for user32 directly. Everything here is cgo-free (pure syscall)
// and degrades to a no-op on failure — a window that fails to go topmost is
// still a usable campfire.

var (
	user32 = syscall.NewLazyDLL("user32.dll")

	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLongW           = user32.NewProc("GetWindowLongW")
	procSetWindowLongW           = user32.NewProc("SetWindowLongW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procGetAsyncKeyState         = user32.NewProc("GetAsyncKeyState")
	procMonitorFromPoint         = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procGetDpiForSystem          = user32.NewProc("GetDpiForSystem")
	procGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	procGetDC                    = user32.NewProc("GetDC")
	procReleaseDC                = user32.NewProc("ReleaseDC")
	procSystemParametersInfoW    = user32.NewProc("SystemParametersInfoW")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	gdi32                        = syscall.NewLazyDLL("gdi32.dll")
	procGetDeviceCaps            = gdi32.NewProc("GetDeviceCaps")
	shell32                      = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW            = shell32.NewProc("ShellExecuteW")
)

const (
	// HWND_TOPMOST is (HWND)-1.
	hwndTopmost = ^uintptr(0)
	// HWND_NOTOPMOST is (HWND)-2.
	hwndNotTopmost = ^uintptr(1)

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	// GWL_EXSTYLE is -20.
	gwlExStyle = ^uintptr(19)

	wsExLayered     = 0x00080000
	wsExTransparent = 0x00000020
	wsExNoActivate  = 0x08000000
	// WS_EX_TOOLWINDOW keeps a window out of Alt+Tab and the taskbar. The C#
	// build's LayeredWindow set it on both the campfire and its hover card.
	wsExToolWindow = 0x00000080

	// ShowWindow commands. SW_SHOWNOACTIVATE shows without stealing focus,
	// which is what a card the user is only hovering over needs.
	swShowNoActivate = 4
	swHide           = 0

	// LOGPIXELSX is the device-caps index for the horizontal DPI.
	logPixelsX = 88

	// SPI_GETWORKAREA returns the primary monitor's work area — the screen
	// minus the taskbar — which is where the campfire parks by default.
	spiGetWorkArea = 0x0030

	// MONITOR_DEFAULTTONEAREST: a point off every monitor still resolves to
	// the closest one, which is what a stale saved position needs.
	monitorDefaultToNearest = 0x00000002

	vkLButton = 0x01
)

// rect mirrors the Win32 RECT for SystemParametersInfoW.
type rect struct {
	Left, Top, Right, Bottom int32
}

// point mirrors the Win32 POINT for GetCursorPos.
type point struct {
	X, Y int32
}

// monitorInfo mirrors MONITORINFO. cbSize must be set before the call.
type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}

// OwnWindow is a top-level window belonging to this process.
type OwnWindow struct {
	Hwnd  uintptr
	Title string
}

// enumTarget collects windows during a single EnumWindows pass.
//
// EnumWindows is synchronous, so a package-level slot is safe here — and it
// avoids smuggling a Go pointer through LPARAM, which go vet flags and which
// would be genuinely unsafe if the callback could outlive the call.
var enumTarget *[]OwnWindow

// enumCallback is allocated once; syscall.NewCallback must not be called per
// enumeration or the callback slots leak.
var enumCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
	out := enumTarget
	if out == nil {
		return 1
	}
	// Do not filter by IsWindowVisible here. The go-gui backend calls OnInit
	// before its final ShowWindow call; the campfire must be discoverable while
	// hidden so it can be moved to the final corner before the first paint.
	var buf [512]uint16
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	title := ""
	if n > 0 {
		title = syscall.UTF16ToString(buf[:n])
	}
	*out = append(*out, OwnWindow{Hwnd: hwnd, Title: title})
	return 1
})

// FindOwnWindows lists this process's top-level windows, including windows that
// are still hidden during OnInit. The caller can use the exact title to select
// the intended overlay.
func FindOwnWindows() []OwnWindow {
	out := make([]OwnWindow, 0, 16)
	enumTarget = &out
	procEnumWindows.Call(enumCallback, 0)
	enumTarget = nil

	pid := uint32(syscall.Getpid())
	mine := out[:0]
	for _, w := range out {
		var wpid uint32
		procGetWindowThreadProcessID.Call(w.Hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid == pid {
			mine = append(mine, w)
		}
	}
	return mine
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

// SetTopMost pins or unpins a window above all non-topmost windows.
func SetTopMost(hwnd uintptr, on bool) bool {
	if hwnd == 0 {
		return false
	}
	insertAfter := hwndNotTopmost
	if on {
		insertAfter = hwndTopmost
	}
	r, _, _ := procSetWindowPos.Call(hwnd, insertAfter, 0, 0, 0, 0,
		swpNoMove|swpNoSize|swpNoActivate)
	return r != 0
}

// windowExStyle reads the extended style using the pointer-sized API first and
// the 32-bit API as a fallback. Some Windows compatibility layers expose only
// the latter even in a 64-bit process. Returning zero is still useful: an
// extended style of zero is valid, and callers can add the overlay bits rather
// than incorrectly refusing to configure the window.
func windowExStyle(hwnd uintptr) uintptr {
	style, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlExStyle)
	if style != 0 {
		return style
	}
	style32, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)
	return style32
}

// setWindowExStyle writes the extended style using the pointer-sized API and
// falls back to the legacy export when necessary.
func setWindowExStyle(hwnd, style uintptr) bool {
	if prev, _, _ := procSetWindowLongPtrW.Call(hwnd, gwlExStyle, style); prev != 0 {
		return true
	}
	if prev, _, _ := procSetWindowLongW.Call(hwnd, gwlExStyle, style); prev != 0 {
		return true
	}
	return false
}

// SetClickThrough makes a window ignore the mouse so clicks reach whatever is
// behind it, and stops it taking focus.
//
// WS_EX_TRANSPARENT is the compatibility path used by the original layered
// window. A go-gui GL window cannot use WS_EX_LAYERED on top of its DWM-backed
// surface — go-gui explicitly rejects that combination — so do not add the
// layered bit here.
func SetClickThrough(hwnd uintptr, on bool) bool {
	if hwnd == 0 {
		return false
	}
	style := windowExStyle(hwnd)
	next := style
	if on {
		next |= wsExTransparent | wsExNoActivate | wsExToolWindow
	} else {
		next &^= wsExTransparent
	}
	if next == style {
		return true
	}
	return setWindowExStyle(hwnd, next)
}

// ApplyOverlayStyles turns an existing window into a desktop overlay: topmost,
// absent from Alt+Tab and the taskbar, and never activating itself.
func ApplyOverlayStyles(hwnd uintptr, topMost, clickThrough bool) bool {
	if hwnd == 0 {
		return false
	}
	style := windowExStyle(hwnd)
	next := style | wsExToolWindow | wsExNoActivate
	if clickThrough {
		next |= wsExTransparent
	} else {
		next &^= wsExTransparent
	}
	if next != style && !setWindowExStyle(hwnd, next) {
		core.LogWarn("overlay styles: SetWindowLongPtrW/SetWindowLongW failed")
		return false
	}
	if topMost && !SetTopMost(hwnd, true) {
		core.LogWarn("overlay styles: SetWindowPos(HWND_TOPMOST) failed")
		return false
	}
	return true
}

// SetWindowVisible shows or hides a window without activating it.
func SetWindowVisible(hwnd uintptr, visible bool) bool {
	if hwnd == 0 {
		return false
	}
	cmd := uintptr(swHide)
	if visible {
		cmd = swShowNoActivate
	}
	procShowWindow.Call(hwnd, cmd)
	return true
}

// ActivateWindow brings a window to the foreground.
//
// Windows only honours SetForegroundWindow for a process that already holds
// foreground rights, which a tray click grants; a refusal is normal and is not
// treated as an error. BringWindowToTop is the fallback, since raising the
// window at least puts it above its siblings even when focus is denied.
func ActivateWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	procBringWindowToTop.Call(hwnd)
	r, _, _ := procSetForegroundWindow.Call(hwnd)
	return r != 0
}

// SetWindowSize resizes a window in physical pixels.
//
// go-gui has no window-resize API, and keeps the HWND unexported, so the flame
// size menu has nowhere else to go. This is safe despite being an outside
// mutation: the GL backend handles WM_SIZE by re-reading the client rect and
// re-deriving its physical, logical and DPI sizes, then repaints (see
// backend/gl/events_win32.go, case wmSize). So the toolkit adopts the change
// rather than desyncing from it.
//
// SWP_NOACTIVATE keeps a resize from stealing focus, and SWP_NOZORDER leaves
// the topmost state alone.
func SetWindowSize(hwnd uintptr, width, height int) bool {
	if hwnd == 0 || width <= 0 || height <= 0 {
		return false
	}
	r, _, _ := procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(width), uintptr(height),
		swpNoMove|swpNoZOrder|swpNoActivate)
	return r != 0
}

// SetWindowPosition moves a window without resizing it.
func SetWindowPosition(hwnd uintptr, x, y int) bool {
	if hwnd == 0 {
		return false
	}
	r, _, _ := procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0,
		swpNoSize|swpNoZOrder|swpNoActivate)
	return r != 0
}

// PrimaryWorkArea returns the primary monitor's work area in physical pixels:
// the screen minus the taskbar, which is what "bottom-right" has to mean if the
// campfire is not to sit under the clock.
func PrimaryWorkArea() (x, y, w, h int) {
	var r rect
	ok, _, _ := procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0)
	if ok == 0 {
		return 0, 0, 0, 0
	}
	return int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)
}

// CursorPos returns the mouse position in physical screen pixels.
func CursorPos() (x, y int, ok bool) {
	var pt point
	if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return 0, 0, false
	}
	return int(pt.X), int(pt.Y), true
}

// LeftButtonDown reads the global mouse button state. This is intentionally
// independent of window hit-testing: the campfire is click-through by default,
// so it cannot rely on receiving WM_LBUTTON* messages to implement dragging.
func LeftButtonDown() bool {
	state, _, _ := procGetAsyncKeyState.Call(vkLButton)
	return state&0x8000 != 0
}

// CursorWorkArea returns the work area of the monitor the mouse is on.
//
// This is what the C# build used for "reset position" (Screen.FromPoint(
// Cursor.Position).WorkingArea), and it matters on a multi-monitor desk: parking
// the campfire on the primary monitor when the user is working on the second one
// is not a reset, it is a disappearance.
func CursorWorkArea() (x, y, w, h int) {
	ptX, ptY, ok := CursorPos()
	if !ok {
		return PrimaryWorkArea()
	}
	// MonitorFromPoint takes POINT by value, so the ABI decides how it is
	// passed: one 64-bit register holding both int32s on amd64, two stack slots
	// on 386. Getting this wrong yields a monitor handle of 0 rather than a
	// crash, which would silently degrade to the primary work area.
	var mon uintptr
	if unsafe.Sizeof(uintptr(0)) == 8 {
		packed := uintptr(uint32(ptX)) | uintptr(uint32(ptY))<<32
		mon, _, _ = procMonitorFromPoint.Call(packed, monitorDefaultToNearest)
	} else {
		mon, _, _ = procMonitorFromPoint.Call(uintptr(ptX), uintptr(ptY), monitorDefaultToNearest)
	}
	if mon == 0 {
		return PrimaryWorkArea()
	}
	mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return PrimaryWorkArea()
	}
	return int(mi.RcWork.Left), int(mi.RcWork.Top),
		int(mi.RcWork.Right - mi.RcWork.Left), int(mi.RcWork.Bottom - mi.RcWork.Top)
}

// WorkAreaAt returns the work area of the monitor containing the point. It is
// the placement counterpart to CursorWorkArea: once the campfire has a position,
// the monitor it lives on — not the one the mouse happens to be on — is what
// constrains it.
func WorkAreaAt(ptX, ptY int) (x, y, w, h int) {
	var mon uintptr
	if unsafe.Sizeof(uintptr(0)) == 8 {
		packed := uintptr(uint32(ptX)) | uintptr(uint32(ptY))<<32
		mon, _, _ = procMonitorFromPoint.Call(packed, monitorDefaultToNearest)
	} else {
		mon, _, _ = procMonitorFromPoint.Call(uintptr(ptX), uintptr(ptY), monitorDefaultToNearest)
	}
	if mon == 0 {
		return PrimaryWorkArea()
	}
	mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	if r, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return PrimaryWorkArea()
	}
	return int(mi.RcWork.Left), int(mi.RcWork.Top),
		int(mi.RcWork.Right - mi.RcWork.Left), int(mi.RcWork.Bottom - mi.RcWork.Top)
}

// WindowRect returns a window's screen rectangle in physical pixels.
func WindowRect(hwnd uintptr) (x, y, w, h int, ok bool) {
	if hwnd == 0 {
		return 0, 0, 0, 0, false
	}
	var r rect
	if r1, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); r1 == 0 {
		return 0, 0, 0, 0, false
	}
	return int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top), true
}

// OpenURL hands a URL to the shell's default handler.
//
// ShellExecuteW rather than a spawned rundll32: no child process to reap, no
// console window to flash, and no quoting rules to get wrong. It returns an
// error code <= 32 on failure, which the caller treats as "just log it" — a
// browser that will not open must never take the app down.
func OpenURL(url string) bool {
	if url == "" {
		return false
	}
	target, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return false
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	// SW_SHOWNORMAL is 1.
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)), 0, 0, 1)
	return r > 32
}

var (
	dpiOnce  sync.Once
	dpiScale float64 = 1
)

// WindowDPIScale returns the DPI scale of the monitor a window is on, which is
// the scale go-gui itself uses (backend/gl reads GetDpiForWindow the same way).
//
// This is the authority, not SystemDPIScale: go-gui only calls
// SetProcessDpiAwarenessContext from inside its own window creation, so anything
// that asks the system before that gets the DPI of a DPI-unaware process — a
// flat 96, i.e. scale 1.0 — on every display that is not at 100%.
func WindowDPIScale(hwnd uintptr) float64 {
	if hwnd == 0 {
		return SystemDPIScale()
	}
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	if dpi == 0 {
		return SystemDPIScale()
	}
	s := float64(dpi) / 96.0
	if s < 1 {
		return 1
	}
	return s
}

// SystemDPIScale returns the system DPI divided by 96.
//
// Only trustworthy once the process is DPI aware, which go-gui arranges during
// window creation; before that it reports 1.0 whatever the display says. It is
// kept as the fallback for the moments before a window handle exists.
func SystemDPIScale() float64 {
	dpiOnce.Do(func() {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi != 0 {
			dpiScale = float64(dpi) / 96.0
			return
		}
		// Pre-1607 fallback: ask a screen DC.
		hdc, _, _ := procGetDC.Call(0)
		if hdc == 0 {
			return
		}
		defer procReleaseDC.Call(0, hdc)
		if v, _, _ := procGetDeviceCaps.Call(hdc, logPixelsX); v != 0 {
			dpiScale = float64(v) / 96.0
		}
	})
	if dpiScale < 1 {
		return 1
	}
	return dpiScale
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

// WindowAlive reports whether a handle still refers to a live window.
//
// go-gui destroys windows without telling the app, so a cached *gui.Window can
// outlive the native window it wraps. Anything holding a cached handle needs to
// be able to ask.
func WindowAlive(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	r, _, _ := procIsWindow.Call(hwnd)
	return r != 0
}

// WindowVisible reports the native visibility state, used by the tray show/hide
// path and diagnostics.
func WindowVisible(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}
