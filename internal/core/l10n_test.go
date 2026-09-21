package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCompact(t *testing.T) {
	cases := map[int64]string{
		999:           "999",
		1_200:         "1.20k",
		12_500:        "12.5k",
		999_999:       "1.00M",
		3_400_000:     "3.40M",
		999_999_999:   "1.00B",
		2_100_000_000: "2.10B",
	}
	for value, want := range cases {
		if got := Compact(value); got != want {
			t.Errorf("Compact(%d) = %q, want %q", value, got, want)
		}
	}
}

// TestEveryLiteralKeyExists walks the source for T("...") calls and checks each
// key against the table.
//
// This is the one l10n mistake the compiler cannot catch: T returns the key
// itself when it is missing, so a typo shows up as the literal text "menu.xyz"
// in the UI, on one screen, in whatever language happens to be active. The
// column count is safe — the table is a [4]string — but a wrong or deleted key
// is not.
func TestEveryLiteralKeyExists(t *testing.T) {
	dirs := []string{".", "../data", "../ui", "../fire"}
	pattern := regexp.MustCompile(`\bT\("([A-Za-z0-9_.]+)"\)`)

	checked := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("cannot read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range pattern.FindAllStringSubmatch(string(body), -1) {
				checked++
				if _, ok := Table[m[1]]; !ok {
					t.Errorf("%s: T(%q) has no row in Table — it would render as the key itself",
						name, m[1])
				}
			}
		}
	}
	// A floor, not a target: it exists only to catch a scan that silently stops
	// reaching the call sites and would otherwise pass by finding nothing. The
	// real number is larger and grows with the UI.
	if checked < 60 {
		t.Fatalf("only %d keys found; the scan is not reaching the call sites", checked)
	}
}
