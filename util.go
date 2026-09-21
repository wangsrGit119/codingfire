package main

import (
	"os"
	"path/filepath"
)

// writeTextFile writes UTF-8 without a BOM, matching the C# build's
// UTF8Encoding(false). A BOM would show up as a stray character at the top of
// the pasted report.
func writeTextFile(path, content string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func filepathJoin(elem ...string) string { return filepath.Join(elem...) }
