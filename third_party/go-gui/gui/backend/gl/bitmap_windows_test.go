//go:build windows

package gl

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"runtime"
	"testing"

	"github.com/go-gui-org/go-gui/gui"
)

func TestBitmapPremultipliedPixels(t *testing.T) {
	for _, im := range []image.Image{
		&image.NRGBA{Pix: []byte{200, 100, 50, 128, 255, 255, 255, 0}, Stride: 8, Rect: image.Rect(0, 0, 2, 1)},
		&image.RGBA{Pix: []byte{100, 50, 25, 128, 0, 0, 0, 0}, Stride: 8, Rect: image.Rect(0, 0, 2, 1)},
	} {
		got := make([]byte, 8)
		writeBGRA(got, im)
		if !bytes.Equal(got, []byte{25, 50, 100, 128, 0, 0, 0, 0}) {
			t.Fatalf("pixels: %v", got)
		}
	}
	parent := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	parent.SetNRGBA(2, 3, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	got := make([]byte, 4)
	writeBGRA(got, parent.SubImage(image.Rect(2, 3, 3, 4)))
	if !bytes.Equal(got, []byte{25, 50, 100, 128}) {
		t.Fatalf("subimage: %v", got)
	}
}

func TestBitmapWindowWithoutGL(t *testing.T) {
	if os.Getenv("CODINGFIRE_BITMAP_TEST") != "1" {
		t.Skip("set CODINGFIRE_BITMAP_TEST=1 for native window integration")
	}
	defer runtime.UnlockOSThread() // New locks once
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
	w := gui.NewWindow(gui.WindowCfg{
		Width: 16, Height: 16, Transparent: true, Decorations: gui.DecorationNone, InitiallyHidden: true,
		BitmapFrame: func(*gui.Window) image.Image { return img },
	})
	b, err := New(w)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()
	b.plat.w = w
	if b.plat.hglrc != 0 || b.textSys != nil || b.presentBitmap == nil {
		t.Fatal("bitmap overlay initialized GPU/text resources")
	}
	for i := 0; i < 300; i++ {
		b.renderFrame(w)
	}
	// Exercise DPI/resize's non-GL path too.
	b.handleResize()
	img = image.NewNRGBA(image.Rect(0, 0, 24, 24))
	b.renderFrame(w)
	var r bitmapSurface
	r.hwnd = b.plat.hwnd
	r.present(img)
	defer r.close()
	if r.loggedError || r.bitmap == 0 {
		t.Fatal("layered presentation failed")
	}
}

func TestSoftwareWindowInteraction(t *testing.T) {
	if os.Getenv("CODINGFIRE_BITMAP_TEST") != "1" {
		t.Skip("opt-in native window test")
	}
	defer runtime.UnlockOSThread()
	initialized, clicked := 0, 0
	w := gui.NewWindow(gui.WindowCfg{Width: 240, Height: 120, Software: true, InitiallyHidden: true,
		OnInit: func(w *gui.Window) {
			initialized++
			w.SetView(func(*gui.Window) gui.View {
				return gui.Button(gui.ButtonCfg{ID: "action", Label: "Native software button", OnClick: func(gui.EventCtx) { clicked++ }})
			})
		},
	})
	b, err := New(w)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()
	b.plat.w = w
	w.Config.OnInit(w)
	w.FrameFn()
	b.renderFrame(w)
	if initialized != 1 || w.HeadlessRender() || b.plat.hglrc != 0 {
		t.Fatal("software backend initialization/input mode is wrong")
	}
	if err := w.TestClick("action"); err != nil {
		t.Fatal(err)
	}
	if clicked != 1 {
		t.Fatal("button did not handle input")
	}
	w.FrameFn()
	b.renderFrame(w)
}
