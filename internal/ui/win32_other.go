//go:build !windows && !linux && (!darwin || !cgo)

package ui

// Stubs for platforms with no overlay-window support. Windows, Linux and macOS
// each have a real implementation (win32_windows.go, win32_x11.go,
// win32_darwin.go); these keep the package buildable on anything else, which is
// what makes `go vet ./...` and the headless modes work on a machine that is
// none of the three.
//
// The `!cgo` half of the constraint matters: the macOS implementation is cgo,
// so a darwin build without it has no window layer. Such a build has no GUI
// backend either (gui/backend/run_default.go), but it still has to compile.

// FindOwnWindows returns nothing.
func FindOwnWindows() []OwnWindow { return nil }

// FindOwnWindowByTitle returns 0.
func FindOwnWindowByTitle(title string) uintptr { return 0 }

// SetTopMost is a no-op.
func SetTopMost(hwnd uintptr, on bool) bool { return false }

// SetClickThrough is a no-op.
func SetClickThrough(hwnd uintptr, on bool) bool { return false }

// ApplyOverlayStyles is a no-op.
func ApplyOverlayStyles(hwnd uintptr, topMost, clickThrough bool) bool { return false }

// SetWindowVisible is a no-op.
func SetWindowVisible(hwnd uintptr, visible bool) bool { return false }

// ActivateWindow is a no-op.
func ActivateWindow(hwnd uintptr) bool { return false }

// SetWindowSize is a no-op.
func SetWindowSize(hwnd uintptr, width, height int) bool { return false }

// SetWindowPosition is a no-op.
func SetWindowPosition(hwnd uintptr, x, y int) bool { return false }

// PrimaryWorkArea reports nothing.
func PrimaryWorkArea() (x, y, w, h int) { return 0, 0, 0, 0 }

// CursorWorkArea reports nothing.
func CursorWorkArea() (x, y, w, h int) { return 0, 0, 0, 0 }

// WorkAreaAt reports nothing.
func WorkAreaAt(ptX, ptY int) (x, y, w, h int) { return 0, 0, 0, 0 }

// CursorPos reports nothing.
func CursorPos() (x, y int, ok bool) { return 0, 0, false }

// LeftButtonDown reports false.
func LeftButtonDown() bool { return false }

// WindowRect reports nothing.
func WindowRect(hwnd uintptr) (x, y, w, h int, ok bool) { return 0, 0, 0, 0, false }

// OpenURL is a no-op.
func OpenURL(url string) bool { return false }

// SystemDPIScale reports 1.0.
func SystemDPIScale() float64 { return 1 }

// WindowDPIScale reports 1.0.
func WindowDPIScale(hwnd uintptr) float64 { return 1 }

// ApplyOverlayWindowStyles is a no-op.
func ApplyOverlayWindowStyles(title string, topMost, clickThrough bool) uintptr {
	return 0
}

// WindowAlive reports false.
func WindowAlive(hwnd uintptr) bool { return false }

// WindowVisible reports false.
func WindowVisible(hwnd uintptr) bool { return false }
