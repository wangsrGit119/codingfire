package core

import "os"

// replaceFile moves tmp over path, replacing it.
//
// os.Rename already maps to MoveFileEx(MOVEFILE_REPLACE_EXISTING) on Windows,
// but a filesystem filter or antivirus handle can still make it fail with a
// sharing violation. The fallback is delete-then-rename: not atomic, but it
// recovers the case that would otherwise lose the settings write entirely.
func replaceFile(tmp, path string) error {
	if err := os.Rename(tmp, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmp, path)
}
