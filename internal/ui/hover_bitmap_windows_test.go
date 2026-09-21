//go:build windows

package ui

import (
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
)

func TestNativeHoverBitmap(t *testing.T) {
	core.SetLanguage(core.LangChineseSimplified)
	defer core.SetLanguage(core.LangSystem)
	p := &nativeHoverPainter{}
	defer p.close()
	m := HoverModel{TodayTokens: 1234567, TokensPerSecond: 1234.5, ShowLiveRate: true, HasUpdated: true, UpdatedAt: time.Date(2026, 9, 21, 15, 42, 0, 0, time.Local), Rows: []HoverRow{
		{Source: core.UsageSourcesAll[0], Tokens: 1000000},
		{Source: core.UsageSourcesAll[1], Tokens: 234567},
	}}
	img := p.draw(m, 1.5)
	if img == nil || img.Bounds().Dx() != 324 || p.bitmap == 0 {
		t.Fatal("native card did not render")
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
