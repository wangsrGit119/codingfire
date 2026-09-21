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
	// Deliberately the SAME name the C# build uses. The two builds are one app
	// behind one login entry, so turning autostart on in either has to replace
	// the other's path rather than add a second entry — a Run value holds
	// exactly one path, and two entries would race for the single-instance
	// mutex at every login.
	autoStartValueName = "CodingFire"

	// autoStartLegacyValueName is what this build wrote before it adopted the
	// C# build's identity. Left behind, it would start a second copy at login
	// and turning autostart off would delete only the new value — the
	// campfire would keep appearing while the checkmark said otherwise. It is
	// removed the first time AutoStartApply runs.
	autoStartLegacyValueName = "CodingFireGo"
)

// cleanLegacyAutoStart removes the pre-merge Run value, if a previous version
// of this build left one behind. "CodingFireGo" is unambiguously ours, so
// deleting it cannot touch an entry the user created by hand.
func cleanLegacyAutoStart() {
	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartRunKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return // no key at all, so nothing of ours can be in it
	}
	defer k.Close()

	if _, _, err := k.GetStringValue(autoStartLegacyValueName); err != nil {
		return // already clean
	}
	if err := k.DeleteValue(autoStartLegacyValueName); err != nil {
		LogWarn("legacy autostart cleanup failed: " + err.Error())
	}
}

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
// startup never touches the registry — the one exception being
// cleanLegacyAutoStart, which runs once after an upgrade and only deletes. A
// false return means the write failed (group policy, permissions, locked hive)
// and the caller should roll the setting back.
func AutoStartApply(desired bool) bool {
	cleanLegacyAutoStart()

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
