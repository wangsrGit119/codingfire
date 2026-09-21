//go:build linux && !android

package core

import (
	"os"
	"path/filepath"
	"strings"
)

// XDG autostart: a .desktop file under $XDG_CONFIG_HOME/autostart is run at
// login by every freedesktop desktop environment. It is the Linux equivalent
// of the HKCU Run key, including the property that matters most here — the
// entry only takes effect at the next login, never for the running session.
//
// The file name is deliberately "CodingFireGo.desktop", NOT
// "CodingFire.desktop", matching the Windows Run value: the two builds are
// meant to be able to coexist, and a shared name would make them overwrite
// each other's entry — the last one to run would win the login slot while both
// still reported autostart as enabled, and turning it off in one would
// silently disable it for the other.
const autoStartDesktopFile = "CodingFireGo.desktop"

// autoStartPath is the .desktop file the desktop environment reads.
func autoStartPath() string {
	return filepath.Join(configDir(), "autostart", autoStartDesktopFile)
}

// AutoStartEnabled reports whether the autostart entry is actually present.
// The .desktop file is the authority here, not the setting.
func AutoStartEnabled() bool {
	info, err := os.Stat(autoStartPath())
	return err == nil && !info.IsDir() && info.Size() > 0
}

// AutoStartApply drives the autostart entry to desired.
//
// It writes nothing when the entry is already in the target state, so a normal
// startup never touches the file. A false return means the write failed (a
// read-only config directory, a full disk) and the caller should roll the
// setting back.
func AutoStartApply(desired bool) bool {
	path := autoStartPath()

	if desired {
		exe := executablePath()
		if exe == "" {
			LogWarn("autostart skipped: cannot resolve executable path")
			return false
		}
		wanted := desktopEntry(exe)
		if cur, err := os.ReadFile(path); err == nil && string(cur) == wanted {
			return true // exe has not moved; already correct
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			LogWarn("autostart write failed: " + err.Error())
			return false
		}
		if err := os.WriteFile(path, []byte(wanted), 0o644); err != nil {
			LogWarn("autostart write failed: " + err.Error())
			return false
		}
		return true
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return true // it was not there to begin with
		}
		LogWarn("autostart delete failed: " + err.Error())
		return false
	}
	return true
}

// desktopEntry renders the autostart entry for the running binary.
//
// There is no Comment key. The format localises a key by suffixing it with a
// locale (Comment[zh_CN]=…), so a single unsuffixed Comment would be English
// for every user, and a login item is not worth four translations plus a
// locale mapping. Name and Exec are all the entry needs to work.
func desktopEntry(exe string) string {
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Version=1.0\n" +
		"Name=" + AppInfo.Name() + "\n" +
		"Exec=" + desktopExecQuote(exe) + "\n" +
		"Terminal=false\n" +
		"StartupNotify=false\n" +
		// GNOME only lists an entry that either omits the hidden flag or
		// states this one; the other desktops ignore it.
		"X-GNOME-Autostart-enabled=true\n"
}

// desktopExecQuote renders a path as the single argument of an Exec line.
//
// The Exec value is a command line, not a bare path, so a path with spaces has
// to be quoted or it is split into two arguments. Inside a quoted argument the
// spec reserves backslash, double quote, backtick and dollar, and each must be
// escaped with a backslash — a path may legally contain any of them.
func desktopExecQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '`', '$', '\\':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('"')
	return b.String()
}
