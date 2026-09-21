//go:build darwin

package core

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// appleLanguagesTimeout bounds the `defaults` read. It runs once, lazily, on the
// first language resolution; a hung `defaults` must not hang the app's startup.
const appleLanguagesTimeout = 3 * time.Second

// platformLanguageName returns the UI language as a short tag, e.g. "zh-CN" or
// "ja-JP".
//
// The POSIX locale environment is not enough on macOS: an app launched from
// Finder or from a LaunchAgent inherits no LANG at all, so the environment
// check answers "en" on a Chinese Mac and the "Follow system" setting silently
// does nothing. The authoritative source is the AppleLanguages preference,
// which is what the Language & Region pane writes.
//
// It is read by running `defaults`, which is a read-only local query. Parsing
// the plist ourselves is not an option: it is written in the binary plist
// format, and pulling in a plist decoder to read one string is a poor trade.
func platformLanguageName() string {
	if tag := appleLanguages()[0]; tag != "" {
		return tag
	}
	return posixLanguageName()
}

// appleLanguages returns the user's preferred languages, most preferred first,
// or nil when the preference cannot be read.
func appleLanguages() []string {
	ctx, cancel := context.WithTimeout(context.Background(), appleLanguagesTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return nil
	}
	// The output is a plist array:
	//
	//	(
	//	    "zh-Hans-CN",
	//	    "en-US"
	//	)
	//
	// Only the quoted entries matter, and the order is the preference order, so
	// scanning for quoted runs is both sufficient and immune to the
	// parenthesised layout changing.
	var langs []string
	for rest := string(out); ; {
		start := strings.IndexByte(rest, '"')
		if start < 0 {
			break
		}
		rest = rest[start+1:]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			break
		}
		if entry := strings.TrimSpace(rest[:end]); entry != "" {
			langs = append(langs, entry)
		}
		rest = rest[end+1:]
	}
	return langs
}
