//go:build windows

package ui

import (
	"image/png"
	"math"
	"os"
	"testing"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// chartBand is where the mini timeline lands in the drawn image, in device
// pixels. It is derived from the shared constant rather than hard-coded, so
// moving the chart fails the assertion instead of silently pointing at the
// wrong rows — and so the painter is held to the same origin the view uses.
func chartBand(scale float64) (top, bottom int) {
	return int(math.Round(hoverChartTop * scale)),
		int(math.Round((hoverChartTop + hoverCardChartH) * scale))
}

func TestNativeHoverBitmap(t *testing.T) {
	core.SetLanguage(core.LangChineseSimplified)
	defer core.SetLanguage(core.LangSystem)
	p := &nativeHoverPainter{}
	defer p.close()
	m := HoverModel{TodayTokens: 1234567, TokensPerSecond: 1234.5, ShowLiveRate: true, HasUpdated: true, UpdatedAt: time.Date(2026, 9, 21, 15, 42, 0, 0, time.Local), Rows: []HoverRow{
		{Source: core.UsageSourcesAll[0], Tokens: 1000000},
		{Source: core.UsageSourcesAll[1], Tokens: 234567},
	}, Hourly: busyHours()}
	img := p.draw(m, 1.5)
	if img == nil || img.Bounds().Dx() != 324 || p.bitmap == 0 {
		t.Fatal("native card did not render")
	}
	// The surface is sized from hoverCardHeight, so a chart the height function
	// did not account for would be cut off rather than merely cramped.
	if want := int(math.Ceil(float64(hoverCardHeight(m)) * 1.5)); img.Bounds().Dy() != want {
		t.Fatalf("card is %d px tall, want %d — hoverCardHeight and the painter disagree",
			img.Bounds().Dy(), want)
	}
	white := 0
	for i := 0; i < len(p.img.Pix); i += 4 {
		if p.img.Pix[i] > 150 && p.img.Pix[i+1] > 150 && p.img.Pix[i+2] > 150 {
			white++
		}
	}
	if white < 100 {
		t.Fatal("native text mask did not render")
	}

	// The mini timeline. The bars are blended amber over warm black, so they are
	// the only pixels in that band that are bright and warm; the card's own
	// border is far dimmer. Counting columns rather than pixels is what makes
	// this a test of "24 bars were drawn" instead of "something was drawn".
	top, bottom := chartBand(1.5)
	columns := map[int]bool{}
	for y := top; y < bottom && y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			i := y*p.img.Stride + x*4
			r, g, b := p.img.Pix[i], p.img.Pix[i+1], p.img.Pix[i+2]
			if r > 80 && r > g && g >= b {
				columns[x] = true
			}
		}
	}
	// 24 slots of ~6.9 px at 1.5x is about 250 device columns.
	if len(columns) < 200 {
		t.Fatalf("mini timeline covers %d columns, want most of 24 bars", len(columns))
	}
	// The hour axis sits directly below. Its labels are pale grey; the bars are
	// amber, so "no warm pixels here" is what proves the bars stopped at the
	// bottom of the plot rather than running through their own labels. Counting
	// columns rather than pixels keeps this about 24 bars, not about paint.
	labels, warm := 0, 0
	for y := bottom; y < bottom+hoverCardAxisH && y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			i := y*p.img.Stride + x*4
			r, g, b := p.img.Pix[i], p.img.Pix[i+1], p.img.Pix[i+2]
			if r > 100 && g > 100 && b > 100 {
				labels++
			}
			if r > 80 && r > g+20 && g > b+20 {
				warm++
			}
		}
	}
	if labels == 0 {
		t.Fatal("the hour axis drew no labels")
	}
	if warm > 0 {
		t.Fatalf("%d amber pixels below the plot: the bars ran into the axis", warm)
	}

	first := p.img
	for i := 0; i < 50; i++ {
		p.draw(m, 1.5)
	}
	if p.img != first || len(p.fonts) > 5 {
		t.Fatal("card buffers/fonts not reused")
	}
	if path := os.Getenv("CODINGFIRE_HOVER_PNG"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, p.img); err != nil {
			t.Error(err)
		}
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}
	m.Rows = nil
	if p.draw(m, 1) == nil {
		t.Fatal("empty resized card failed")
	}
}

// TestNativeHoverBitmapWithoutTimeline: a day with nothing in it must not spend
// 36 px of card on a row of stubs.
func TestNativeHoverBitmapWithoutTimeline(t *testing.T) {
	p := &nativeHoverPainter{}
	defer p.close()

	rows := []HoverRow{{Source: core.UsageSourcesAll[0], Tokens: 1000}}
	bare := p.draw(HoverModel{TodayTokens: 1000, Rows: rows}, 1)
	if bare == nil {
		t.Fatal("card did not render")
	}
	bareH := bare.Bounds().Dy()

	withChart := p.draw(HoverModel{TodayTokens: 1000, Rows: rows, Hourly: busyHours()}, 1)
	if withChart == nil {
		t.Fatal("card with chart did not render")
	}
	want := bareH + hoverChartBlockH
	if withChart.Bounds().Dy() != want {
		t.Errorf("chart added %d px, want %d", withChart.Bounds().Dy()-bareH, hoverChartBlockH)
	}
}
