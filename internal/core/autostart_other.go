//go:build !windows && !linux && !darwin

package core

// Autostart stubs for platforms with no implementation. Windows, Linux and
// macOS each have a real one (autostart_windows.go, autostart_linux.go,
// autostart_darwin.go); these keep the package buildable on anything else.
//
// Both report failure rather than success: the caller treats "false" from
// AutoStartApply as "the setting could not be applied", which is the honest
// answer when there is nowhere to apply it.

// AutoStartEnabled reports false.
func AutoStartEnabled() bool { return false }

// AutoStartApply reports that the setting could not be applied.
func AutoStartApply(desired bool) bool { return false }
