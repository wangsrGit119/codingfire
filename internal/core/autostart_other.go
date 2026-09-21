//go:build !windows

package core

// AutoStartEnabled is a no-op off Windows. The shipped app is Windows-only;
// this exists so the package still compiles elsewhere.
func AutoStartEnabled() bool { return false }

// AutoStartApply is a no-op off Windows.
func AutoStartApply(desired bool) bool { return false }
