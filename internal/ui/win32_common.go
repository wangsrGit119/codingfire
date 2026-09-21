package ui

// OwnWindow is a top-level window belonging to this process. It is shared by
// every platform's overlay-window shim so the callers in app.go and console.go
// never have to care which one is compiled in.
//
// The shims live in win32_*.go because that is the name the layer has always
// had; only the Windows file is actually Win32. Each platform file implements
// the same set of functions, and every one of them degrades to a no-op on
// failure: a window that refuses to go on top is still a usable campfire, and
// nothing here is worth taking the process down for.
type OwnWindow struct {
	Hwnd  uintptr
	Title string
}
