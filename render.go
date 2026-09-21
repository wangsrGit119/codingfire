package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"github.com/wangsrGit119/codingfire/internal/core"
	"github.com/wangsrGit119/codingfire/internal/fire"
	"github.com/wangsrGit119/codingfire/internal/ui"
)

// previewFrames is how many simulation steps each tier is advanced before the
// capture. The heat field needs time to grow into its shape; at 12fps this is
// 7.5 seconds of fire.
const previewFrames = 90

// previewFPS matches the simulation's own stepping rate.
const previewFPS = 12.0

// previewBackdrop is the dark plate the fire is composited onto. Without it the
// transparent pixels are indistinguishable from the flame's own dark edges in
// most image viewers.
var previewBackdrop = color.NRGBA{R: 26, G: 22, B: 20, A: 255}

// renderPreviews writes one PNG per fire tier, plus the tray icon and a hover
// card sample, into dir.
func renderPreviews(dir string) int {
	outDir := dir
	if outDir == "" {
		outDir = "preview"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "could not create", outDir, ":", err)
		return 1
	}

	settings := core.DefaultSettings()
	settings.Size = core.SizeMedium
	fire.SourceFlameColors.Attach(settings)

	renderer := fire.NewCampfireRenderer()
	px := core.SizeMedium.PixelScale()
	w := int(ceilF(float64(fire.FireW)*px + 28*fire.DpiScale))
	h := int(ceilF((float64(fire.FireH)+float64(fire.LogH))*px + 36*fire.DpiScale))
	renderer.Resize(w, h)

	t := 0.0
	for _, style := range fire.FirePreviewsAll {
		name := style.Raw()
		snap := style.Snapshot()

		// Reset between tiers, or the previous fire's heat bleeds into this one.
		renderer.ResetEngine()
		for i := 0; i < previewFrames; i++ {
			t += 1.0 / previewFPS
			renderer.Render(snap, px, false, t)
		}

		path := filepath.Join(outDir, "fire_"+name+".png")
		if err := writeCompositedPNG(path, renderer.Pix(), w, h); err != nil {
			writeCrash("render style " + name + " failed: " + err.Error())
			return 1
		}
	}

	if err := writePNGBytes(filepath.Join(outDir, "tray_icon.png"), ui.TrayIconArt.BuildPNG(64)); err != nil {
		writeCrash("render tray icon failed: " + err.Error())
		return 1
	}

	// The hover card is drawn by the toolkit rather than by a bespoke painter —
	// go-gui renders text, so the card is a view, not a bitmap. Rendering it
	// through the same view the app uses is what keeps the sample honest.
	// Scale 1, matching the C# build, which saved its painter's raw bitmap.
	if err := ui.RenderHoverCardPNG(sampleHoverModel(), filepath.Join(outDir, "hover_card.png"), 1); err != nil {
		writeCrash("render hover card failed: " + err.Error())
		return 1
	}

	return 0
}

// sampleHoverModel is the same fixture the C# build's --render used, so the two
// cards can be compared directly.
func sampleHoverModel() ui.HoverModel {
	model := ui.HoverModel{
		TodayTokens:     1284000,
		TokensPerSecond: 42.7,
		ShowLiveRate:    true,
		UpdatedAt:       core.Time{},
		HasUpdated:      true,
	}
	model.Rows = []ui.HoverRow{
		{Source: core.ClaudeCode, Tokens: 812000},
		{Source: core.Codex, Tokens: 344000},
		{Source: core.Cursor, Tokens: 128000, Estimated: true},
	}
	return model
}

// writeCompositedPNG flattens the renderer's straight-alpha buffer onto the
// backdrop and encodes it.
//
// The renderer emits NRGBA in memory order, which is exactly what image.NRGBA
// wants, so this is a copy plus a source-over composite — no channel swizzle.
// (The C# renderer emitted premultiplied BGRA for UpdateLayeredWindow, so it
// needed a conversion here; go-gui's UseImage wants straight RGBA, which moved
// that work into the renderer instead.)
func writeCompositedPNG(path string, pix []byte, w, h int) error {
	if len(pix) < w*h*4 {
		return fmt.Errorf("pixel buffer is %d bytes, want %d", len(pix), w*h*4)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			src := color.NRGBA{R: pix[i], G: pix[i+1], B: pix[i+2], A: pix[i+3]}
			img.SetNRGBA(x, y, over(src, previewBackdrop))
		}
	}
	return writeImage(path, img)
}

// over composites src over dst, both straight-alpha, returning straight alpha.
func over(src, dst color.NRGBA) color.NRGBA {
	if src.A == 255 {
		return src
	}
	if src.A == 0 {
		return dst
	}
	sa := float64(src.A) / 255
	da := float64(dst.A) / 255
	outA := sa + da*(1-sa)
	if outA <= 0 {
		return color.NRGBA{}
	}
	blend := func(s, d uint8) uint8 {
		v := (float64(s)*sa + float64(d)*da*(1-sa)) / outA
		return clampByte(v)
	}
	return color.NRGBA{
		R: blend(src.R, dst.R),
		G: blend(src.G, dst.G),
		B: blend(src.B, dst.B),
		A: clampByte(outA * 255),
	}
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func writeImage(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// writePNGBytes writes an already-encoded PNG.
func writePNGBytes(path string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("no PNG data")
	}
	return os.WriteFile(path, data, 0o644)
}

func ceilF(v float64) float64 {
	i := float64(int(v))
	if v > i {
		return i + 1
	}
	return i
}
