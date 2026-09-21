//go:build !windows && !darwin

package core

// platformLanguageName returns the UI language as a short tag, e.g. "zh-CN".
//
// This is the complement of syslang_windows.go and syslang_darwin.go: on every
// other platform the POSIX locale environment is the whole answer, so there is
// nothing to do but read it.
func platformLanguageName() string {
	return posixLanguageName()
}
