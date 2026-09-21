package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// TrayIconArt generates the tray and window icon in code — a small pixel
// campfire — so the app ships no external resource files.
type trayIconArt struct{}

// TrayIconArt is the exported singleton.
var TrayIconArt trayIconArt

// BuildPNG renders a campfire icon of the given edge length as PNG bytes, which
// is what go-gui's tray and window icon configs want.
//
// The tray uses 16 and 32 px, so 32 is drawn and the shell scales it down.
func (trayIconArt) BuildPNG(size int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := size
	// A compact ember halo gives the icon a stronger silhouette in the tray,
	// where the shell scales it down to 16px and transparent pixels disappear.
	halo := color.NRGBA{255, 126, 32, 72}
	cx, cy := float64(s)/2, float64(s)*0.56
	radius := float64(s) * 0.42
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			if dx*dx+dy*dy <= radius*radius {
				img.SetNRGBA(x, y, halo)
			}
		}
	}

	// Flame: four stacked bands, narrowing upward.
	// Each entry is colour plus [baseHalfWidth, tipHalfWidth, bottomY, topY]
	// as fractions of the icon size.
	layers := []struct {
		c             color.NRGBA
		baseW, tipW   float64
		bottomY, topY float64
	}{
		{color.NRGBA{255, 92, 16, 255}, 0.46, 0.16, 0.72, 0.16},
		{color.NRGBA{255, 158, 30, 255}, 0.32, 0.10, 0.70, 0.28},
		{color.NRGBA{255, 212, 60, 255}, 0.19, 0.05, 0.68, 0.44},
		{color.NRGBA{255, 246, 200, 255}, 0.08, 0.02, 0.66, 0.58},
	}

	for _, l := range layers {
		yTop := int(math.Round(l.topY * float64(s)))
		yBottom := int(math.Round(l.bottomY * float64(s)))
		for y := yTop; y <= yBottom; y++ {
			t := 0.0
			if yBottom != yTop {
				t = float64(y-yTop) / float64(yBottom-yTop)
			}
			halfW := (l.tipW + (l.baseW-l.tipW)*t) * float64(s)
			cx := s / 2
			x0 := int(math.Round(float64(cx) - halfW))
			x1 := int(math.Round(float64(cx) + halfW))
			for x := x0; x <= x1; x++ {
				if x < 0 || y < 0 || x >= s || y >= s {
					continue
				}
				img.SetNRGBA(x, y, l.c)
			}
		}
	}

	// Two small sparks make the flame read at a glance without adding noise.
	spark := color.NRGBA{255, 224, 126, 235}
	for _, p := range [][2]int{{int(float64(s) * 0.34), int(float64(s) * 0.23)}, {int(float64(s) * 0.66), int(float64(s) * 0.31)}} {
		if p[0] >= 0 && p[0] < s && p[1] >= 0 && p[1] < s {
			img.SetNRGBA(p[0], p[1], spark)
		}
	}

	// Log pile.
	logDark := color.NRGBA{96, 54, 22, 255}
	logMid := color.NRGBA{140, 86, 36, 255}
	ly0 := int(math.Round(0.72 * float64(s)))
	ly1 := int(math.Round(0.86 * float64(s)))
	for y := ly0; y <= ly1; y++ {
		inset := int(math.Round(0.12 * float64(s)))
		if y == ly0 {
			inset = int(math.Round(0.20 * float64(s)))
		}
		c := logDark
		if y <= ly0+1 {
			c = logMid
		}
		for x := inset; x < s-inset; x++ {
			if x >= 0 && y < s {
				img.SetNRGBA(x, y, c)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// cachedPNG memoises the generated icon, which is needed on every tray menu
// rebuild.
var cachedPNG = map[int][]byte{}

// CachedPNG returns the PNG for a size, generating it once.
func (trayIconArt) CachedPNG(size int) []byte {
	if b, ok := cachedPNG[size]; ok {
		return b
	}
	b := TrayIconArt.BuildPNG(size)
	cachedPNG[size] = b
	return b
}
