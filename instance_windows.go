//go:build windows

package main

import (
	"errors"

	"golang.org/x/sys/windows"
)

// singleInstance takes a named mutex so only one copy of the app owns the tray.
//
// It returns (release, alreadyRunning). alreadyRunning is true when another
// process holds the mutex, in which case release is a no-op and the caller must
// exit.
//
// x/sys declares CreateMutex with ERROR_ALREADY_EXISTS in its failure set, so an
// existing mutex comes back as that error rather than as a silent success with a
// live handle — which is why this does not have to consult GetLastError.
//
// On any other failure the app starts rather than refusing to: a second
// campfire is a cosmetic problem, a silently dead app is not.
func singleInstance(name string) (func(), bool) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return func() {}, false
	}

	handle, err := windows.CreateMutex(nil, false, namePtr)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		return func() {}, true
	}
	if err != nil || handle == 0 {
		return func() {}, false
	}

	return func() { windows.CloseHandle(handle) }, false
}
