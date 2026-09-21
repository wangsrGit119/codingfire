//go:build windows

package core

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procGetUserDefaultLocaleName = kernel32.NewProc("GetUserDefaultLocaleName")
)

// platformLanguageName returns the OS UI language as a locale tag, e.g.
// "zh-CN", "ja-JP", "en-US". GetUserDefaultLocaleName needs Vista+, which is
// well below our Windows 10 floor.
func platformLanguageName() string {
	// LOCALE_NAME_MAX_LENGTH is 85.
	buf := make([]uint16, 85)
	n, _, _ := procGetUserDefaultLocaleName.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if n == 0 {
		return "en"
	}
	// The return value counts the terminating NUL.
	return syscall.UTF16ToString(buf[:int(n)-1])
}
