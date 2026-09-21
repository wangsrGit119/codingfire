package core

import "os"

// executablePath returns the running binary's full path, or "" when it cannot
// be resolved — in which case the caller must not write an autostart entry.
//
// Shared rather than per-platform because all three autostart backends need
// exactly the same answer, and a wrong path silently produces a login entry
// that fails at the next login instead of at the moment it was written.
func executablePath() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return ""
	}
	return exe
}
