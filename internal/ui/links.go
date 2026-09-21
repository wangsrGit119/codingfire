package ui

import "github.com/wangsrGit119/codingfire/internal/core"

// OpenLink hands a URL to the shell's default handler.
//
// Failure is logged, never returned: not being able to open a web page is not a
// reason to interrupt anyone, and it must certainly not take the app down. The
// URL is expected to come from AppInfo.ProjectUrl, which is the single place it
// is written down.
func OpenLink(url string) {
	if url == "" {
		return
	}
	if !OpenURL(url) {
		core.LogWarn("could not open " + url + " in the default browser")
	}
}
