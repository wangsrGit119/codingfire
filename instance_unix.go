//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// singleInstance takes an exclusive lock on a file in the app's data directory
// so only one copy of the app owns the tray.
//
// A file lock rather than a pid file: the kernel releases an flock when the
// process dies for any reason, so a crash cannot strand the lock and leave the
// app permanently refusing to start. flock is also the one advisory lock both
// Linux and macOS have in common.
//
// It returns (release, alreadyRunning). alreadyRunning is true when another
// process holds the lock, in which case release is a no-op and the caller must
// exit.
//
// On any other failure the app starts rather than refusing to: a second
// campfire is a cosmetic problem, a silently dead app is not.
func singleInstance(name string) (func(), bool) {
	dir := core.AppPaths.DataDir()
	if dir == "" {
		return func() {}, false
	}

	f, err := os.OpenFile(filepath.Join(dir, lockFileName(name)),
		os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false
	}
	// LOCK_NB makes a second instance report the clash instead of blocking
	// forever, which is what a plain LOCK_EX would do.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return func() {}, true
	}

	// The pid is written for diagnosis only — nothing reads it, because the
	// lock is the authority. It has to be truncated first: a shorter pid
	// overwriting a longer one would otherwise leave trailing digits.
	if _, err := f.Seek(0, 0); err == nil {
		if err := f.Truncate(0); err == nil {
			_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
		}
	}

	// The descriptor stays open for the lifetime of the process: closing it
	// is what releases the lock, which is why release must run exactly once.
	return func() { _ = f.Close() }, false
}

// lockFileName turns an instance name into a legal file name. The name is
// written for a Windows mutex, where dots and spaces are fine; here it only
// has to identify the instance on disk.
func lockFileName(name string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '-'
	}, name)
	if safe == "" {
		safe = "instance"
	}
	return safe + ".lock"
}
