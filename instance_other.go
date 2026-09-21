//go:build !windows && !linux && !darwin

package main

// singleInstance is a no-op on platforms with no implementation. Windows,
// Linux and macOS each have a real one (instance_windows.go, instance_unix.go);
// this keeps the headless modes building on anything else.
//
// It reports "not already running" so the app starts: a second campfire is a
// cosmetic problem, a silently dead app is not.
func singleInstance(name string) (func(), bool) { return func() {}, false }
