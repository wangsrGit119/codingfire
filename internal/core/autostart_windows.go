//go:build windows

package core

import (
	"golang.org/x/sys/windows/registry"
)

const (
	autoStartRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	// autoStartValueName is fixed rather than derived from the exe name, so
	// renaming the binary does not strand the old entry.
	//
	// Deliberately "CodingFireGo", NOT "CodingFire": the C# build uses the
	// latter, and a Run value holds exactly one path. Sharing the name would
	// make the two builds overwrite each other's entry — the last one to run
	// would win the login slot while both still reported autostart as enabled,
	// and turning it off in one would silently disable it for the other.
	autoStartValueName = "CodingFireGo"
)

// AutoStartEnabled reports whether the Run entry is actually present. The
// registry is the authority here, not the setting.
func AutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartRunKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	v, _, err := k.GetStringValue(autoStartValueName)
	return err == nil && v != ""
}

// AutoStartApply drives the Run entry to desired.
//
// It writes nothing when the entry is already in the target state, so a normal
// startup never touches the registry. A false return means the write failed
// (group policy, permissions, locked hive) and the caller should roll the
// setting back.
func AutoStartApply(desired bool) bool {
	if desired {
		exe := executablePath()
		if exe == "" {
			LogWarn("autostart skipped: cannot resolve executable path")
			return false
		}
		// Quoted so a path containing spaces (C:\Program Files\…) is not split
		// into two arguments.
		wanted := `"` + exe + `"`

		k, _, err := registry.CreateKey(registry.CURRENT_USER, autoStartRunKey, registry.SET_VALUE|registry.QUERY_VALUE)
		if err != nil {
			LogWarn("autostart write failed: " + err.Error())
			return false
		}
		defer k.Close()

		if cur, _, err := k.GetStringValue(autoStartValueName); err == nil && cur == wanted {
			return true // exe has not moved; already correct
		}
		if err := k.SetStringValue(autoStartValueName, wanted); err != nil {
			LogWarn("autostart write failed: " + err.Error())
			return false
		}
		return true
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartRunKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return true // no key at all means it is already off
	}
	defer k.Close()

	if _, _, err := k.GetStringValue(autoStartValueName); err != nil {
		return true // value was not there to begin with
	}
	if err := k.DeleteValue(autoStartValueName); err != nil {
		LogWarn("autostart delete failed: " + err.Error())
		return false
	}
	return true
}
