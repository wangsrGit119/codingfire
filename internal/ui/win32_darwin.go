//go:build darwin && cgo

package ui

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation

#include <stdlib.h>
#include "cocoa_darwin.h"
*/
import "C"

import (
	"math"
	"sync"
	"unsafe"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// The macOS half of the overlay-window layer. The AppKit calls themselves live
// in cocoa_darwin.m; this file is the bridge, and it owns the one thing the
// other two platforms get for free — a coherent answer to "where is the window"
// when the question is asked from a goroutine that is not allowed to touch it.
//
// Requires cgo, which the metal backend already does, so a darwin build without
// it has no GUI backend either way (see gui/backend/run_default.go).
//
// Coordinate systems differ from both other platforms: Cocoa puts the origin at
// the bottom-left of the menu-bar display with y increasing upward, while the
// rest of the app — and both other shims — use a top-left origin with y
// increasing downward. Every value crossing this boundary is converted, and the
// conversion is done here rather than in Objective-C so it is testable by
// reading one file.

// winFrame is a window rectangle in top-left screen coordinates.
type winFrame struct{ x, y, w, h int }

// darwinFrames remembers the frame we last asked each window to have.
//
// It exists because AppKit is a main-thread API while the drag and hover code
// polls the window rectangle from the 20 Hz tick goroutine. Mutations are
// dispatched to the main queue and the shadow is updated immediately, so a
// caller on any goroutine reads a current answer without ever touching a window
// from the wrong thread.
//
// It stays accurate because nothing else moves these windows: both overlay
// windows are borderless and click-through, so the user has no way to drag one.
// The shadow is seeded from the real frame in ApplyOverlayStyles, which runs on
// the main thread from OnInit.
var (
	darwinFramesMu sync.Mutex
	darwinFrames   = map[uintptr]winFrame{}
)

func rememberFrame(hwnd uintptr, f winFrame) {
	darwinFramesMu.Lock()
	darwinFrames[hwnd] = f
	darwinFramesMu.Unlock()
}

func recallFrame(hwnd uintptr) (winFrame, bool) {
	darwinFramesMu.Lock()
	f, ok := darwinFrames[hwnd]
	darwinFramesMu.Unlock()
	return f, ok
}

// darwinFrame returns the window's frame, seeding the shadow from AppKit the
// first time it is asked.
//
// Seeding is deliberately limited to the main thread. The AppKit read itself is
// safe from anywhere — cfWindowFrame hops to the main queue — but this is polled
// at 20 Hz from the tick goroutine, and a poll that blocks waiting on another
// thread to answer is worse than one that admits it does not know. The shadow is
// seeded by ApplyOverlayStyles before the window ever moves, so the miss path
// only exists for a handle that was never styled.
func darwinFrame(hwnd uintptr) (winFrame, bool) {
	if f, ok := recallFrame(hwnd); ok {
		return f, true
	}
	if hwnd == 0 || C.cfOnMainThread() == 0 {
		// Nothing to read and nothing worth guessing at.
		return winFrame{}, false
	}
	var out [4]C.double
	C.cfWindowFrame(C.uintptr_t(hwnd), (*C.double)(unsafe.Pointer(&out[0])))
	if out[2] <= 0 || out[3] <= 0 {
		return winFrame{}, false
	}
	f := cocoaRectToTopLeft(float64(out[0]), float64(out[1]), float64(out[2]), float64(out[3]))
	rememberFrame(hwnd, f)
	return f, true
}

// cocoaRectToTopLeft converts a Cocoa rectangle to top-left screen coordinates.
func cocoaRectToTopLeft(x, y, w, h float64) winFrame {
	primary := float64(C.cfPrimaryScreenHeight())
	return winFrame{
		x: int(math.Round(x)),
		// Cocoa's y is the bottom edge measured upward, so the top edge is the
		// primary display's height minus the rectangle's far edge.
		y: int(math.Round(primary - (y + h))),
		w: int(math.Round(w)),
		h: int(math.Round(h)),
	}
}

// topLeftToCocoa is the inverse of cocoaRectToTopLeft.
func topLeftToCocoa(f winFrame) (x, y, w, h float64) {
	primary := float64(C.cfPrimaryScreenHeight())
	return float64(f.x), primary - float64(f.y+f.h), float64(f.w), float64(f.h)
}

// topLeftPointToCocoa converts a top-left screen point to Cocoa coordinates.
func topLeftPointToCocoa(px, py int) (float64, float64) {
	return float64(px), float64(C.cfPrimaryScreenHeight()) - float64(py)
}

func boolInt(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Window discovery
// ---------------------------------------------------------------------------

// FindOwnWindows lists this process's windows.
//
// Unlike the other two platforms this needs no ownership test: every window in
// the application's list already belongs to this process.
func FindOwnWindows() []OwnWindow {
	n := int(C.cfWindowCount())
	if n <= 0 {
		return nil
	}
	out := make([]OwnWindow, 0, n)
	buf := make([]byte, 512)
	for i := 0; i < n; i++ {
		hwnd := uintptr(C.cfWindowAt(C.int(i)))
		if hwnd == 0 {
			continue
		}
		title := ""
		if length := int(C.cfWindowTitleAt(C.int(i),
			(*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))); length > 0 {
			title = string(buf[:length])
		}
		out = append(out, OwnWindow{Hwnd: hwnd, Title: title})
	}
	return out
}

// FindOwnWindowByTitle returns the first own window whose title matches.
func FindOwnWindowByTitle(title string) uintptr {
	if title == "" {
		return 0
	}
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	return uintptr(C.cfWindowFindByTitle(cTitle))
}

// ---------------------------------------------------------------------------
// Window manager
// ---------------------------------------------------------------------------

// SetTopMost pins or unpins a window above all normal windows.
func SetTopMost(hwnd uintptr, on bool) bool {
	if hwnd == 0 {
		return false
	}
	C.cfWindowSetLevel(C.uintptr_t(hwnd), boolInt(on))
	return true
}

// SetClickThrough makes a window ignore the mouse so clicks reach whatever is
// behind it.
//
// setIgnoresMouseEvents is the whole feature on macOS: the window server stops
// delivering events to it, which is also why the app polls the cursor instead of
// waiting for mouse events (see the drag loop in app.go).
func SetClickThrough(hwnd uintptr, on bool) bool {
	if hwnd == 0 {
		return false
	}
	C.cfWindowSetIgnoresMouse(C.uintptr_t(hwnd), boolInt(on))
	return true
}

// ApplyOverlayStyles turns an existing window into a desktop overlay: on top,
// on every Space, and optionally click-through.
func ApplyOverlayStyles(hwnd uintptr, topMost, clickThrough bool) bool {
	if hwnd == 0 {
		return false
	}
	// Seed the shadow before anything moves the window: every later position
	// and size calculation is relative to it.
	if _, ok := darwinFrame(hwnd); !ok {
		core.LogWarn("overlay styles: could not read the window frame")
	}
	C.cfWindowSetOverlayBehavior(C.uintptr_t(hwnd), 1)
	C.cfWindowSetLevel(C.uintptr_t(hwnd), boolInt(topMost))
	C.cfWindowSetIgnoresMouse(C.uintptr_t(hwnd), boolInt(clickThrough))
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

// SetWindowVisible shows or hides a window without activating it.
func SetWindowVisible(hwnd uintptr, visible bool) bool {
	if hwnd == 0 {
		return false
	}
	C.cfWindowSetVisible(C.uintptr_t(hwnd), boolInt(visible))
	return true
}

// ActivateWindow brings a window to the front and gives it focus.
func ActivateWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	C.cfWindowActivate(C.uintptr_t(hwnd))
	return true
}

// SetWindowSize resizes a window in physical pixels, keeping its position.
func SetWindowSize(hwnd uintptr, width, height int) bool {
	if hwnd == 0 || width <= 0 || height <= 0 {
		return false
	}
	f, ok := darwinFrame(hwnd)
	if !ok {
		return false
	}
	f.w, f.h = width, height
	cx, cy, cw, ch := topLeftToCocoa(f)
	C.cfWindowSetFrame(C.uintptr_t(hwnd), C.double(cx), C.double(cy), C.double(cw), C.double(ch))
	rememberFrame(hwnd, f)
	return true
}

// SetWindowPosition moves a window in physical pixels, keeping its size.
func SetWindowPosition(hwnd uintptr, x, y int) bool {
	if hwnd == 0 {
		return false
	}
	f, ok := darwinFrame(hwnd)
	if !ok {
		return false
	}
	f.x, f.y = x, y
	cx, cy, cw, ch := topLeftToCocoa(f)
	C.cfWindowSetFrame(C.uintptr_t(hwnd), C.double(cx), C.double(cy), C.double(cw), C.double(ch))
	rememberFrame(hwnd, f)
	return true
}

// WindowRect returns a window's screen rectangle in physical pixels.
func WindowRect(hwnd uintptr) (x, y, w, h int, ok bool) {
	f, found := darwinFrame(hwnd)
	if !found {
		return 0, 0, 0, 0, false
	}
	return f.x, f.y, f.w, f.h, true
}

// WindowAlive reports whether a handle still refers to a live window.
func WindowAlive(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	return C.cfWindowIsAlive(C.uintptr_t(hwnd)) != 0
}

// WindowVisible reports the native visibility state.
func WindowVisible(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	return C.cfWindowIsVisible(C.uintptr_t(hwnd)) != 0
}

// ---------------------------------------------------------------------------
// Screens, pointer and the shell
// ---------------------------------------------------------------------------

// PrimaryWorkArea returns the primary display's visible area: the screen minus
// the menu bar and the Dock.
func PrimaryWorkArea() (x, y, w, h int) {
	var out [4]C.double
	C.cfScreenVisibleFrame((*C.double)(unsafe.Pointer(&out[0])))
	f := cocoaRectToTopLeft(float64(out[0]), float64(out[1]), float64(out[2]), float64(out[3]))
	return f.x, f.y, f.w, f.h
}

// WorkAreaAt returns the usable area of the display containing a point.
func WorkAreaAt(ptX, ptY int) (x, y, w, h int) {
	cx, cy := topLeftPointToCocoa(ptX, ptY)
	var out [4]C.double
	C.cfScreenVisibleFrameAt(C.double(cx), C.double(cy), (*C.double)(unsafe.Pointer(&out[0])))
	f := cocoaRectToTopLeft(float64(out[0]), float64(out[1]), float64(out[2]), float64(out[3]))
	if f.w <= 0 || f.h <= 0 {
		return PrimaryWorkArea()
	}
	return f.x, f.y, f.w, f.h
}

// CursorWorkArea returns the usable area of the display the mouse is on.
func CursorWorkArea() (x, y, w, h int) {
	ptX, ptY, ok := CursorPos()
	if !ok {
		return PrimaryWorkArea()
	}
	return WorkAreaAt(ptX, ptY)
}

// CursorPos returns the mouse position in physical screen pixels.
func CursorPos() (x, y int, ok bool) {
	var out [2]C.double
	C.cfCursorPos((*C.double)(unsafe.Pointer(&out[0])))
	primary := float64(C.cfPrimaryScreenHeight())
	return int(math.Round(float64(out[0]))), int(math.Round(primary - float64(out[1]))), true
}

// LeftButtonDown reads the global mouse button state.
//
// Like the Win32 GetAsyncKeyState version this is independent of window
// hit-testing, which is the point: the campfire is click-through by default, so
// it never receives the button events a drag would otherwise be built on.
func LeftButtonDown() bool {
	return C.cfLeftButtonDown() != 0
}

// OpenURL hands a URL to the desktop's default handler.
//
// NSWorkspace routes it through the user's own browser choice, the same way
// ShellExecute and xdg-open do. A refusal is only ever logged — a browser that
// will not open must never take the app down.
func OpenURL(url string) bool {
	if url == "" {
		return false
	}
	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	if C.cfOpenURL(cURL) == 0 {
		core.LogWarn("could not open " + url)
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Display scale
// ---------------------------------------------------------------------------

var (
	darwinScaleOnce sync.Once
	darwinScale     float64 = 1
)

// SystemDPIScale returns the primary display's backing scale factor.
//
// It is kept as the fallback for the moments before a window handle exists,
// mirroring the other two shims. The value is cached for the process lifetime,
// as the Win32 one is: a display change is rare and the app re-reads the
// per-window scale anyway.
func SystemDPIScale() float64 {
	darwinScaleOnce.Do(func() {
		if s := float64(C.cfScreenScaleAt(C.double(0), C.double(0))); s > 0 {
			darwinScale = s
		}
	})
	if darwinScale < 1 {
		return 1
	}
	return darwinScale
}

// WindowDPIScale returns the backing scale factor of the display a window is
// on, which is the scale go-gui itself renders at. This is the authority, not
// SystemDPIScale: a campfire on a 2x display next to a 1x one has to be
// rendered at the scale of the display it is actually on.
func WindowDPIScale(hwnd uintptr) float64 {
	f, ok := darwinFrame(hwnd)
	if !ok {
		return SystemDPIScale()
	}
	cx, cy := topLeftPointToCocoa(f.x, f.y)
	s := float64(C.cfScreenScaleAt(C.double(cx), C.double(cy)))
	if s < 1 {
		return 1
	}
	return s
}
