//go:build !windows

package ui

// Non-Windows stubs. The shipped app is Windows-only; these keep the package
// buildable elsewhere so the headless modes still work.

// OwnWindow is a top-level window belonging to this process.
type OwnWindow struct {
	Hwnd  uintptr
	Title string
}

// FindOwnWindows returns nothing off Windows.
func FindOwnWindows() []OwnWindow { return nil }

// FindOwnWindowByTitle returns 0 off Windows.
func FindOwnWindowByTitle(title string) uintptr { return 0 }

// SetTopMost is a no-op off Windows.
func SetTopMost(hwnd uintptr, on bool) bool { return false }

// SetClickThrough is a no-op off Windows.
func SetClickThrough(hwnd uintptr, on bool) bool { return false }

// ApplyOverlayStyles is a no-op off Windows.
func ApplyOverlayStyles(hwnd uintptr, topMost, clickThrough bool) bool { return false }

// SetWindowVisible is a no-op off Windows.
func SetWindowVisible(hwnd uintptr, visible bool) bool { return false }

// ActivateWindow is a no-op off Windows.
func ActivateWindow(hwnd uintptr) bool { return false }

// SetWindowSize is a no-op off Windows.
func SetWindowSize(hwnd uintptr, width, height int) bool { return false }

// SetWindowPosition is a no-op off Windows.
func SetWindowPosition(hwnd uintptr, x, y int) bool { return false }

// PrimaryWorkArea reports nothing off Windows.
func PrimaryWorkArea() (x, y, w, h int) { return 0, 0, 0, 0 }

// CursorWorkArea reports nothing off Windows.
func CursorWorkArea() (x, y, w, h int) { return 0, 0, 0, 0 }

// WorkAreaAt reports nothing off Windows.
func WorkAreaAt(ptX, ptY int) (x, y, w, h int) { return 0, 0, 0, 0 }

// CursorPos reports nothing off Windows.
func CursorPos() (x, y int, ok bool) { return 0, 0, false }

// LeftButtonDown reports false off Windows.
func LeftButtonDown() bool { return false }

// WindowRect reports nothing off Windows.
func WindowRect(hwnd uintptr) (x, y, w, h int, ok bool) { return 0, 0, 0, 0, false }

// OpenURL is a no-op off Windows.
func OpenURL(url string) bool { return false }

// SystemDPIScale reports 1.0 off Windows.
func SystemDPIScale() float64 { return 1 }

// WindowDPIScale reports 1.0 off Windows.
func WindowDPIScale(hwnd uintptr) float64 { return 1 }

// ApplyOverlayWindowStyles is a no-op off Windows.
func ApplyOverlayWindowStyles(title string, topMost, clickThrough bool) uintptr {
	return 0
}

// WindowAlive reports false off Windows.
func WindowAlive(hwnd uintptr) bool { return false }

// WindowVisible reports false off Windows.
func WindowVisible(hwnd uintptr) bool { return false }
