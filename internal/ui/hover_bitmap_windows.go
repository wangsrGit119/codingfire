//go:build windows

package ui

import (
	"image"
	"math"
	"syscall"
	"unsafe"

	"github.com/go-gui-org/go-gui/gui"

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
	content := float64(hoverCardWidth - 2*hoverCardPad)
	// The root's border is drawn inside its padding, so the content box starts a
	// pixel in. The view lays out from there; painting from the padding edge
	// instead left the whole card a pixel up and to the left of its twin.
	left := float64(hoverCardPad + hoverCardBorder)
	y := left
	p.text(core.T("hover.today"), left, y, content, hoverCardTitleH, hoverCardTitleSize, 150, false, 0)
	if model.ShowLiveRate && model.TokensPerSecond > 0 {
		p.text(format1(model.TokensPerSecond)+" "+core.T("hover.tokensS"), left+75, y, content-75, hoverCardTitleH, hoverCardFooterSize, 110, false, 2)
	}
	y += hoverCardTitleH
	p.text(core.Compact(int64(model.TodayTokens)), left, y, content, hoverCardBigH, hoverCardBigSize, 245, true, 0)
	y += hoverCardBigH + hoverCardBigGap
	if hoverChartVisible(model) {
		p.chart(model.Hourly, left, y, content)
		y += hoverCardChartH
		p.axis(model.Hourly, left, y, content)
		y += hoverCardAxisH + hoverCardChartGap
	}
	if len(model.Rows) == 0 {
		p.text(core.T("hover.none"), left, y, content, 2*hoverCardRowH, hoverCardRowSize, 110, false, 0x10)
		y += 2 * hoverCardRowH
	} else {
		for _, row := range model.Rows {
			accent := fire.SourceFlameColors.Accent(row.Source)
			p.dot(left+3, y+hoverCardRowH/2, accent)
			name := row.Source.DisplayName()
			if row.Estimated {
				name += " · " + core.T("hover.estimate")
			}
			p.text(name, left+14, y, content-76, hoverCardRowH, hoverCardRowSize, 150, false, 0)
			p.text(core.Compact(int64(row.Tokens)), left+content-62, y, 62, hoverCardRowH, hoverCardRowSize, 245, false, 2)
			y += hoverCardRowH
		}
		y += hoverCardRowsGap
	}
	if model.HasUpdated {
		y = float64(hoverCardHeight(model)) - hoverCardBorder - hoverCardBottomPad - hoverCardFooterH
		p.text(core.T("hover.updated")+" "+model.UpdatedAt.Format("15:04"), left, y, content, hoverCardFooterH, hoverCardFooterSize, 110, false, 0)
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

// chart draws the mini timeline, one bar per hour, bottom-aligned in a box of
// hoverCardChartH.
//
// This is the bitmap twin of hoverCardChart. The two have to agree on more than
// the colours: the bar heights, the minimum stub and the peak highlight are all
// part of the same picture, and a reader comparing the card on Windows with the
// one on Linux should not be able to tell which they were looking at.
func (p *nativeHoverPainter) chart(hours []core.HourlyUsage, x, y, width float64) {
	max := hourMax(hours)
	if max <= 0 || len(hours) == 0 {
		return
	}
	peak := peakHour(hours)
	slot := width / float64(len(hours))

	for _, h := range hours {
		barH := math.Round(float64(h.Tokens) / float64(max) * float64(hoverCardChartH-4))
		if h.Tokens > 0 && barH < 2 {
			barH = 2
		}
		if barH <= 0 {
			continue
		}
		c := hoverChartBar
		if h.Hour == peak {
			c = hoverChartPeak
		}
		p.bar(
			x+float64(h.Hour)*slot,
			y+float64(hoverCardChartH)-barH,
			slot-hoverCardChartInset,
			barH,
			c,
		)
	}
}

// axis draws the hour labels under the mini timeline, one per tick.
//
// The label is wider than the 8 px slot its bar occupies and DrawTextW clips to
// the rect it is given, so each one gets a rect three slots wide positioned so
// that its centre is the centre of the bar it names. Passing the slot itself
// would have produced eight ellipses.
func (p *nativeHoverPainter) axis(hours []core.HourlyUsage, x, y, width float64) {
	if len(hours) == 0 {
		return
	}
	slot := width / float64(len(hours))
	for _, h := range hours {
		label := hourLabel(h.Hour)
		if label == "" {
			continue
		}
		p.text(label,
			x+float64(h.Hour)*slot-slot,
			y, 3*slot, hoverCardAxisH,
			hoverCardAxisSize, hoverCardAxisAlpha, false, 1) // 1 = centred
	}
}

// bar fills one axis-aligned rectangle, blending it over the glass underneath.
// The partial pixels at the edges are weighted by their coverage, so a bar at a
// fractional scale does not end in a hard, wrongly-placed step.
func (p *nativeHoverPainter) bar(x, y, w, h float64, c gui.Color) {
	if w <= 0 || h <= 0 {
		return
	}
	alpha := float64(c.A) / 255
	x0, y0 := x*p.scale, y*p.scale
	x1, y1 := (x+w)*p.scale, (y+h)*p.scale

	for yy := int(math.Floor(y0)); yy < int(math.Ceil(y1)); yy++ {
		rowCover := math.Min(float64(yy)+1, y1) - math.Max(float64(yy), y0)
		if rowCover <= 0 {
			continue
		}
		for xx := int(math.Floor(x0)); xx < int(math.Ceil(x1)); xx++ {
			if !image.Pt(xx, yy).In(p.img.Bounds()) {
				continue
			}
			colCover := math.Min(float64(xx)+1, x1) - math.Max(float64(xx), x0)
			a := math.Min(rowCover, colCover) * alpha
			if a <= 0 {
				continue
			}
			i := yy*p.img.Stride + xx*4
			p.img.Pix[i] = blend(c.R, p.img.Pix[i], a)
			p.img.Pix[i+1] = blend(c.G, p.img.Pix[i+1], a)
			p.img.Pix[i+2] = blend(c.B, p.img.Pix[i+2], a)
			p.img.Pix[i+3] = blend(255, p.img.Pix[i+3], a)
		}
	}
}

// blend puts src over dst at coverage a.
func blend(src, dst uint8, a float64) byte {
	return byte(math.Round(float64(src)*a + float64(dst)*(1-a)))
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
