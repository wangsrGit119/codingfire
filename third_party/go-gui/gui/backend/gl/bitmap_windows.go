//go:build windows && !js

package gl

import (
	"image"
	"log"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	bitmapClassOnce      sync.Once
	bitmapClassName      = windows.StringToUTF16Ptr("go-gui-bitmap-overlay")
	pCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	pSelectBitmap        = gdi32.NewProc("SelectObject")
	pDeleteBitmap        = gdi32.NewProc("DeleteObject")
	pDeleteBitmapDC      = gdi32.NewProc("DeleteDC")
	pUpdateLayeredWindow = user32.NewProc("UpdateLayeredWindow")
	pBitmapBitBlt        = gdi32.NewProc("BitBlt")
)

// A layered window must not use the GL class's CS_OWNDC style.
func registerBitmapClass(instance uintptr) *uint16 {
	bitmapClassOnce.Do(func() {
		wc := wndClassExW{lpfnWndProc: wndProcCallback, hInstance: instance, lpszClassName: bitmapClassName}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		wc.hCursor, _, _ = pLoadCursorW.Call(0, idcArrow)
		pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	})
	return bitmapClassName
}

type bitmapInfo struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
	Color                        uint32
}

// bitmapSurface owns one reusable top-down 32-bit DIB and memory DC. Pixels
// live in the DIB itself, so presentation needs no transient full-frame copy.
type bitmapSurface struct {
	hwnd, dc, bitmap, previous uintptr
	pixels                     []byte
	width, height              int
	presenting, loggedError    bool
	opaque                     bool
}

func (s *bitmapSurface) resize(w, h int) bool {
	if s.width == w && s.height == h && s.bitmap != 0 {
		return true
	}
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return false
	}
	s.close()
	s.dc, _, _ = pCreateCompatibleDC.Call(0)
	if s.dc == 0 {
		return false
	}
	info := bitmapInfo{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	s.bitmap, _, _ = pCreateDIBSection.Call(s.dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if s.bitmap == 0 || bits == nil {
		s.close()
		return false
	}
	s.previous, _, _ = pSelectBitmap.Call(s.dc, s.bitmap)
	if s.previous == 0 || s.previous == ^uintptr(0) {
		s.close()
		return false
	}
	s.pixels = unsafe.Slice((*byte)(bits), w*h*4)
	s.width, s.height = w, h
	return true
}

func (s *bitmapSurface) close() {
	// Stop referencing mapped bytes before releasing their GDI allocation.
	s.pixels = nil
	if s.dc != 0 && s.previous != 0 && s.previous != ^uintptr(0) {
		pSelectBitmap.Call(s.dc, s.previous)
	}
	if s.bitmap != 0 {
		pDeleteBitmap.Call(s.bitmap)
	}
	if s.dc != 0 {
		pDeleteBitmapDC.Call(s.dc)
	}
	s.dc, s.bitmap, s.previous = 0, 0, 0
	s.width, s.height = 0, 0
}

func (s *bitmapSurface) present(img image.Image) {
	if img == nil || s.presenting {
		return
	}
	s.presenting = true
	defer func() { s.presenting = false }()
	bounds := img.Bounds()
	if !s.resize(bounds.Dx(), bounds.Dy()) {
		return
	}
	writeBGRA(s.pixels, img)
	if s.opaque {
		dc, _, _ := pGetDC.Call(s.hwnd)
		if dc != 0 {
			pBitmapBitBlt.Call(dc, 0, 0, uintptr(s.width), uintptr(s.height), s.dc, 0, 0, 0x00cc0020)
			pReleaseDC.Call(s.hwnd, dc)
		}
		return
	}
	size := struct{ X, Y int32 }{int32(s.width), int32(s.height)}
	source := struct{ X, Y int32 }{}
	blend := [4]byte{0, 0, 255, 1} // AC_SRC_OVER, constant alpha, AC_SRC_ALPHA
	// A nil destination point preserves the user's window position.
	ok, _, err := pUpdateLayeredWindow.Call(s.hwnd, 0, 0, uintptr(unsafe.Pointer(&size)), s.dc,
		uintptr(unsafe.Pointer(&source)), 0, uintptr(unsafe.Pointer(&blend)), 2)
	if ok == 0 && !s.loggedError {
		log.Printf("bitmap: UpdateLayeredWindow: %v", err)
		s.loggedError = true
	}
}

// writeBGRA converts straight/premultiplied Go image buffers to the
// premultiplied BGRA format required by UpdateLayeredWindow.
func writeBGRA(dst []byte, src image.Image) {
	b := src.Bounds()
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			i := (y*b.Dx() + x) * 4
			switch im := src.(type) {
			case *image.NRGBA:
				p := im.Pix[y*im.Stride+x*4:]
				a := uint32(p[3])
				dst[i], dst[i+1], dst[i+2], dst[i+3] = byte((uint32(p[2])*a+127)/255), byte((uint32(p[1])*a+127)/255), byte((uint32(p[0])*a+127)/255), p[3]
			case *image.RGBA:
				p := im.Pix[y*im.Stride+x*4:]
				dst[i], dst[i+1], dst[i+2], dst[i+3] = p[2], p[1], p[0], p[3]
			default:
				r, g, blue, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
				dst[i], dst[i+1], dst[i+2], dst[i+3] = byte(blue>>8), byte(g>>8), byte(r>>8), byte(a>>8)
			}
		}
	}
}
