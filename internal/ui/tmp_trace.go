package ui

// TEMP PROBE — diagnostic scaffolding for the macOS "no flame" investigation.
// Delete this file and every probeLog call once the cause is confirmed.

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/wangsrGit119/codingfire/internal/core"
)

var probeFlameCalls int

func probeLog(format string, args ...any) {
	core.LogInfo("PROBE " + fmt.Sprintf(format, args...))
}

// probeDumpBuffer writes the renderer's live pixel buffer to a PNG so it can be
// compared against what actually reaches the screen.
func probeDumpBuffer(path string, w, h int, pix []byte) {
	if len(pix) < w*h*4 {
		return
	}
	img := &image.NRGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_ = png.Encode(f, img)
}
