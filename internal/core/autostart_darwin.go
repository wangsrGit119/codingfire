//go:build darwin

package core

import (
	"os"
	"path/filepath"
	"strings"
)

// macOS autostart: a LaunchAgent in ~/Library/LaunchAgents, which launchd
// reads at login. It is the per-user equivalent of the HKCU Run key, and it
// shares the property that matters most here — the entry only takes effect at
// the next login, never for the running session.
//
// The label is deliberately "com.codingfire.go", NOT "com.codingfire", for the
// same reason the Windows Run value is "CodingFireGo": a launchd label names
// exactly one job, so sharing one would make the two builds fight over the
// login slot while both still reported autostart as enabled.
const (
	autoStartLabel = "com.codingfire.go"
	autoStartPlist = autoStartLabel + ".plist"
)

// autoStartPath is the plist launchd reads.
//
// Note this is not the app's data directory: launchd only scans
// ~/Library/LaunchAgents, so the file has to live there. It is the one place
// outside our own directory that the app writes, which is why the entry is
// removed rather than left behind when the user turns autostart off.
func autoStartPath() string {
	home := AppPaths.Home()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", autoStartPlist)
}

// AutoStartEnabled reports whether the autostart entry is actually present.
// The plist is the authority here, not the setting.
func AutoStartEnabled() bool {
	path := autoStartPath()
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// AutoStartApply drives the autostart entry to desired.
//
// It writes nothing when the entry is already in the target state, so a normal
// startup never touches the file. A false return means the write failed (a
// read-only home directory, a full disk) and the caller should roll the
// setting back.
//
// The job is not bootstrapped into the running launchd session. Writing the
// file is exactly the Windows semantics — the entry applies at the next login
// — and it keeps a settings toggle from spawning launchctl.
func AutoStartApply(desired bool) bool {
	path := autoStartPath()
	if path == "" {
		LogWarn("autostart skipped: cannot resolve the home directory")
		return false
	}

	if desired {
		exe := executablePath()
		if exe == "" {
			LogWarn("autostart skipped: cannot resolve executable path")
			return false
		}
		wanted := launchAgentPlist(exe)
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

// launchAgentPlist renders the LaunchAgent for the running binary.
//
// ProcessType is Interactive so launchd does not throttle the job as a
// background one; the campfire renders continuously and would be held back by
// the default Background classification.
func launchAgentPlist(exe string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" ` +
		`"http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n" +
		`<plist version="1.0">` + "\n" +
		"<dict>\n" +
		"\t<key>Label</key>\n" +
		"\t<string>" + xmlEscape(autoStartLabel) + "</string>\n" +
		"\t<key>ProgramArguments</key>\n" +
		"\t<array>\n" +
		"\t\t<string>" + xmlEscape(exe) + "</string>\n" +
		"\t</array>\n" +
		"\t<key>RunAtLoad</key>\n" +
		"\t<true/>\n" +
		"\t<key>ProcessType</key>\n" +
		"\t<string>Interactive</string>\n" +
		"</dict>\n" +
		"</plist>\n"
}

// xmlEscape escapes the five characters XML reserves. A path may legally
// contain an ampersand or an angle bracket, and an unescaped one would make
// the plist unparseable — launchd would then silently ignore the job.
func xmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	).Replace(s)
}
