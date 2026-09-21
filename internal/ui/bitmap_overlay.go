package ui

import (
	"image"
	"time"

	"github.com/go-gui-org/go-gui/gui"
)

// Bitmap windows only need a lightweight view for invalidation and events.
// The fire bypasses widget rendering, texture upload, and font initialization.
func bitmapOverlayView(*gui.Window) gui.View {
	return gui.Column(gui.ContainerCfg{Sizing: gui.FillFill, Color: gui.ColorTransparent, SizeBorder: gui.NoBorder})
}

func (a *App) renderFlame() {
	a.fireMu.Lock()
	snap := a.Fire.Snapshot()
	a.fireMu.Unlock()
	a.mu.Lock()
	dragging := a.dragging
	a.mu.Unlock()
	a.Renderer.SetDragging(dragging)
	a.Renderer.Render(snap, a.Settings.Size.PixelScale(), dragging, time.Since(a.startTime).Seconds())
}

func (a *App) flameBitmapFrame(*gui.Window) image.Image {
	if a.flameHidden.Load() {
		return nil
	}
	a.renderFlame()
	a.flameBitmap.Pix = a.Renderer.Pix()
	a.flameBitmap.Stride = a.Renderer.Width() * 4
	a.flameBitmap.Rect = image.Rect(0, 0, a.Renderer.Width(), a.Renderer.Height())
	return &a.flameBitmap
}

// Windows renders the card with native fonts, avoiding full font-file copies
// and glyph atlases in the Go heap just to display a few short labels.
func (a *App) hoverBitmapFrame(w *gui.Window) image.Image {
	s := gui.State[hoverCardState](w)
	if !s.Visible {
		return nil
	}
	if a.nativeHover == nil {
		a.nativeHover = &nativeHoverPainter{}
	}
	return a.nativeHover.draw(s.Model, float64(w.BackingScale))
}
