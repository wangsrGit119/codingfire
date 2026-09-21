//go:build !windows

package core

import (
	"os"
	"strings"
)

// platformLanguageName is a POSIX fallback so the package still compiles and
// behaves sensibly off Windows. The shipped app is Windows-only.
func platformLanguageName() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(k)
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		v = strings.SplitN(v, ".", 2)[0]
		return strings.ReplaceAll(v, "_", "-")
	}
	return "en"
}
