//go:build !windows

package core

import (
	"os"
	"strings"
)

// posixLanguageName reads the UI language out of the POSIX locale environment.
//
// It is the whole answer on Linux and the fallback on macOS, where a GUI launch
// has no LANG at all — see syslang_darwin.go.
func posixLanguageName() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(k)
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		// "zh_CN.UTF-8" -> "zh-CN": the codeset is not part of the language, and
		// the underscore is normalised to the hyphen the resolver expects.
		v = strings.SplitN(v, ".", 2)[0]
		return strings.ReplaceAll(v, "_", "-")
	}
	return "en"
}
