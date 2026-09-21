//go:build windows

package ui

import (
	"image"
	"math"
	"syscall"
	"unsafe"

	"github.com/wangsrGit119/codingfire/internal/core"
	"github.com/wangsrGit119/codingfire/internal/fire"
)

var (
	cardCreateDC    = gdi32.NewProc("CreateCompatibleDC")
	cardCreateDIB   = gdi32.NewProc("CreateDIBSection")
	cardSelect      = gdi32.NewProc("SelectObject")
	cardDelete      = gdi32.NewProc("DeleteObject")
	cardDeleteDC    = gdi32.NewProc("DeleteDC")
	cardCreateFont  = gdi32.NewProc("CreateFontW")
	cardTextColor   = gdi32.NewProc("SetTextColor")
	cardBkMode      = gdi32.NewProc("SetBkMode")
	cardDrawText    = user32.NewProc("DrawTextW")
	cardGDIFlush    = gdi32.NewProc("GdiFlush")
	cardFontName, _ = syscall.UTF16PtrFromString("Segoe UI")
)

type cardBitmapInfo struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, SizeImage uint32
	XPels, YPels           int32
	Used, Important, Color uint32
}

type nativeHoverPainter struct {
	backgroundPixels     []byte
	dc, bitmap, previous uintptr
	mask                 []byte
	img                  *image.RGBA
	fonts                map[int]uintptr
	scale                float64
}

func (p *nativeHoverPainter) close() {
	p.mask, p.img, p.backgroundPixels = nil, nil, nil
	if p.dc != 0 && p.previous != 0 {
		cardSelect.Call(p.dc, p.previous)
	}
	for _, font := range p.fonts {
		cardDelete.Call(font)
	}
	p.fonts = nil
	if p.bitmap != 0 {
		cardDelete.Call(p.bitmap)
	}
	if p.dc != 0 {
		cardDeleteDC.Call(p.dc)
	}
	p.dc, p.bitmap, p.previous = 0, 0, 0
}

func (p *nativeHoverPainter) prepare(w, h int, scale float64) bool {
	if p.img != nil && p.img.Bounds().Dx() == w && p.img.Bounds().Dy() == h && p.scale == scale {
		return true
	}
	p.close()
	p.scale = scale
	p.dc, _, _ = cardCreateDC.Call(0)
	if p.dc == 0 {
		return false
	}
	info := cardBitmapInfo{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	p.bitmap, _, _ = cardCreateDIB.Call(p.dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if p.bitmap == 0 || bits == nil {
		p.close()
		return false
	}
	p.previous, _, _ = cardSelect.Call(p.dc, p.bitmap)
	p.mask = unsafe.Slice((*byte)(bits), w*h*4)
	p.img = image.NewRGBA(image.Rect(0, 0, w, h))
	p.fonts = map[int]uintptr{}
	cardTextColor.Call(p.dc, 0x00ffffff)
	cardBkMode.Call(p.dc, 1) // TRANSPARENT
	p.background()
	p.backgroundPixels = append([]byte(nil), p.img.Pix...)
	return true
}

func (p *nativeHoverPainter) draw(model HoverModel, scale float64) image.Image {
	if scale <= 0 || scale > 8 {
		scale = 1
	}
	w, h := int(math.Ceil(hoverCardWidth*scale)), int(math.Ceil(float64(hoverCardHeight(model))*scale))
	if !p.prepare(w, h, scale) {
		return nil
	}
	copy(p.img.Pix, p.backgroundPixels)
	pad, content := float64(hoverCardPad), float64(hoverCardWidth-2*hoverCardPad)
	y := pad
	p.text(core.T("hover.today"), pad, y, content, hoverCardTitleH, hoverCardTitleSize, 150, false, 0)
	if model.ShowLiveRate && model.TokensPerSecond > 0 {
		p.text(format1(model.TokensPerSecond)+" "+core.T("hover.tokensS"), pad+75, y, content-75, hoverCardTitleH, hoverCardFooterSize, 110, false, 2)
	}
	y += hoverCardTitleH
	p.text(core.Compact(int64(model.TodayTokens)), pad, y, content, hoverCardBigH, hoverCardBigSize, 245, true, 0)
	y += hoverCardBigH + hoverCardBigGap
	if len(model.Rows) == 0 {
		p.text(core.T("hover.none"), pad, y, content, 2*hoverCardRowH, hoverCardRowSize, 110, false, 0x10)
		y += 2 * hoverCardRowH
	} else {
		for _, row := range model.Rows {
			accent := fire.SourceFlameColors.Accent(row.Source)
			p.dot(pad+3, y+hoverCardRowH/2, accent)
			name := row.Source.DisplayName()
			if row.Estimated {
				name += " · " + core.T("hover.estimate")
			}
			p.text(name, pad+14, y, content-76, hoverCardRowH, hoverCardRowSize, 150, false, 0)
			p.text(core.Compact(int64(row.Tokens)), pad+content-62, y, 62, hoverCardRowH, hoverCardRowSize, 245, false, 2)
			y += hoverCardRowH
		}
		y += hoverCardRowsGap
	}
	if model.HasUpdated {
		p.text(core.T("hover.updated")+" "+model.UpdatedAt.Format("15:04"), pad, y, content, hoverCardFooterH, hoverCardFooterSize, 110, false, 0)
	}
	return p.img
}

func (p *nativeHoverPainter) text(text string, x, y, w, h, size float64, alpha byte, bold bool, flags uint32) {
	if text == "" {
		return
	}
	fontSize := int(math.Round(size * p.scale))
	key := fontSize
	weight := uintptr(400)
	if bold {
		key = -key
		weight = 700
	}
	font := p.fonts[key]
	if font == 0 {
		font, _, _ = cardCreateFont.Call(uintptr(int32(-fontSize)), 0, 0, 0, weight, 0, 0, 0, 1, 0, 0, 4, 0, uintptr(unsafe.Pointer(cardFontName)))
		if font == 0 {
			return
		}
		p.fonts[key] = font
	}
	old, _, _ := cardSelect.Call(p.dc, font)
	defer cardSelect.Call(p.dc, old)
	clear(p.mask)
	r := rect{int32(math.Round(x * p.scale)), int32(math.Round(y * p.scale)), int32(math.Round((x + w) * p.scale)), int32(math.Round((y + h) * p.scale))}
	u, err := syscall.UTF16FromString(text)
	if err != nil {
		return
	}
	if flags&0x10 == 0 {
		flags |= 0x20 | 4 | 0x8000
	} // single line, vertically centred, ellipsis
	cardDrawText.Call(p.dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&r)), uintptr(flags|0x800))
	cardGDIFlush.Call() // finish drawing before reading the DIB from Go
	b := image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)).Intersect(p.img.Bounds())
	for yy := b.Min.Y; yy < b.Max.Y; yy++ {
		for xx := b.Min.X; xx < b.Max.X; xx++ {
			i := yy*p.img.Stride + xx*4
			a := uint32(p.mask[i]) * uint32(alpha) / 255
			for c := 0; c < 3; c++ {
				p.img.Pix[i+c] = byte(a + (uint32(p.img.Pix[i+c])*(255-a)+127)/255)
			}
			p.img.Pix[i+3] = byte(a + (uint32(p.img.Pix[i+3])*(255-a)+127)/255)
		}
	}
}

func (p *nativeHoverPainter) background() {
	w, h := p.img.Bounds().Dx(), p.img.Bounds().Dy()
	radius := hoverCardRadius * p.scale
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			qx := math.Abs(float64(x)+0.5-float64(w)/2) - (float64(w)/2 - radius)
			qy := math.Abs(float64(y)+0.5-float64(h)/2) - (float64(h)/2 - radius)
			d := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
			coverage := math.Max(0, math.Min(1, 0.5-d))
			border := math.Max(0, math.Min(coverage, d+p.scale+0.5)) * 46 / 255
			a := coverage * 242 / 255
			i := y*p.img.Stride + x*4
			p.img.Pix[i] = byte(math.Round(26*a*(1-border) + 255*border))
			p.img.Pix[i+1] = byte(math.Round(23*a*(1-border) + 184*border))
			p.img.Pix[i+2] = byte(math.Round(21*a*(1-border) + 92*border))
			p.img.Pix[i+3] = byte(math.Round(255 * (a*(1-border) + border)))
		}
	}
}

func (p *nativeHoverPainter) dot(x, y float64, c core.AccentRGB) {
	cx, cy, r := x*p.scale, y*p.scale, 2.5*p.scale
	for yy := int(cy - r - 1); yy <= int(cy+r+1); yy++ {
		for xx := int(cx - r - 1); xx <= int(cx+r+1); xx++ {
			if !image.Pt(xx, yy).In(p.img.Bounds()) {
				continue
			}
			a := math.Max(0, math.Min(1, r+0.5-math.Hypot(float64(xx)+0.5-cx, float64(yy)+0.5-cy)))
			i := yy*p.img.Stride + xx*4
			for k := 0; k < 3; k++ {
				p.img.Pix[i+k] = byte(math.Round(c[k]*255*a + float64(p.img.Pix[i+k])*(1-a)))
			}
			p.img.Pix[i+3] = byte(math.Round(255*a + float64(p.img.Pix[i+3])*(1-a)))
		}
	}
}
