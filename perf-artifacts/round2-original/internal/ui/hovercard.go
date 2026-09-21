package ui

import (
	"github.com/go-gui-org/go-glyph"
	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend/soft"

	"github.com/wangsrGit119/codingfire/internal/core"
	"github.com/wangsrGit119/codingfire/internal/fire"
)

// The hover card's geometry, in logical pixels.
//
// These are the C# painter's proportions — a 216pt card, 13pt padding, 19pt
// rows — but the type sizes are spelled out rather than taken from the theme.
// The theme's body size is tuned for a 640pt console window, not for a card that
// floats over a 100pt campfire; leaving them themed made the card overflow its
// own box.
const (
	hoverCardWidth  = 216
	hoverCardPad    = 13
	hoverCardRowH   = 19
	hoverCardRadius = 12

	// These are logical pixels. C# used point sizes (8pt, 19pt, 8.6pt,
	// 7.4pt), which convert to approximately 10.7, 25.3, 11.5 and 9.9px
	// at 96 DPI. The rounded values below preserve the same visual hierarchy
	// while avoiding fractional glyph boxes in go-gui.
	hoverCardTitleSize  = 11
	hoverCardBigSize    = 25
	hoverCardRowSize    = 12
	hoverCardFooterSize = 10

	hoverCardTitleH  = 15
	hoverCardBigH    = 30
	hoverCardBigGap  = 10
	hoverCardRowsGap = 6
	hoverCardFooterH = 17
)

// hoverCardHeight follows the C# painter's exact vertical rhythm:
//
//	pad + title(15) + big(30) + gap(10) + rows + rowsGap(6)
//	    + footer(17) + pad
//
// The empty state uses a two-line body in place of rows + rowsGap.
func hoverCardHeight(model HoverModel) float32 {
	head := float32(hoverCardPad) + hoverCardTitleH + hoverCardBigH + hoverCardBigGap
	foot := float32(hoverCardFooterH + hoverCardPad)
	if len(model.Rows) == 0 {
		return head + hoverCardRowH*2 + foot
	}
	return head + float32(len(model.Rows)*hoverCardRowH+hoverCardRowsGap) + foot
}

// HoverCardView builds the hover summary.
//
// This is a view rather than a painted bitmap, which is the one place the Go
// port deliberately differs in mechanism from the C# build's HoverCardPainter:
// go-gui rasterises text itself, so handing it a view is both less code and the
// only way the card inherits the toolkit's fonts and scaling.
func HoverCardView(model HoverModel) []gui.View {
	title := gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		Spacing: gui.SomeF(6),
		Padding: gui.NoPadding,
		Content: []gui.View{
			gui.Text(gui.TextCfg{
				Text:      core.T("hover.today"),
				TextStyle: cardStyle(gui.TextAlignLeft, hoverCardTitleSize, 150),
				Sizing:    gui.FillFit,
			}),
			rateView(model),
		},
	})

	bigStyle := cardStyle(gui.TextAlignLeft, hoverCardBigSize, 245)
	bigStyle.Typeface = glyph.TypefaceBold
	big := gui.Text(gui.TextCfg{
		Text:      core.Compact(int64(model.TodayTokens)),
		TextStyle: bigStyle,
		Sizing:    gui.FillFit,
	})

	body := []gui.View{title, big, hoverSpacer(hoverCardBigGap)}

	if len(model.Rows) == 0 {
		body = append(body, gui.Text(gui.TextCfg{
			Text:      core.T("hover.none"),
			TextStyle: cardStyle(gui.TextAlignLeft, hoverCardRowSize, 110),
			Mode:      gui.TextModeWrap,
			Sizing:    gui.FillFit,
		}))
	} else {
		rows := make([]gui.View, 0, len(model.Rows))
		for _, row := range model.Rows {
			rows = append(rows, hoverCardRow(row))
		}
		body = append(body,
			gui.Column(gui.ContainerCfg{
				Sizing:  gui.FillFit,
				Spacing: gui.SomeF(0),
				Padding: gui.NoPadding,
				Content: rows,
			}),
			hoverSpacer(hoverCardRowsGap),
		)
	}

	if model.HasUpdated {
		body = append(body, gui.Text(gui.TextCfg{
			Text:      core.T("hover.updated") + " " + model.UpdatedAt.Format("15:04"),
			TextStyle: cardStyle(gui.TextAlignLeft, hoverCardFooterSize, 110),
			Sizing:    gui.FillFit,
		}))
	}

	return []gui.View{
		gui.Column(gui.ContainerCfg{
			ID:      "hovercard.root",
			Sizing:  gui.FillFill,
			Padding: gui.PadAll(hoverCardPad),
			Spacing: gui.SomeF(0),
			Radius:  gui.SomeF(hoverCardRadius),
			// C# uses Color.FromArgb(236, 30, 26, 24). Keep the same
			// warm-black glass, with a tiny extra density for the GL surface
			// so it reads as the same dark HUD over bright desktops.
			Color:       gui.RGBA(26, 23, 21, 242),
			ColorBorder: gui.RGBA(255, 184, 92, 46),
			SizeBorder:  gui.SomeF(1),
			Content:     body,
		}),
	}
}

// hoverSpacer is an invisible fixed-height layout item. The C# painter used
// explicit 10px and 6px gaps; leaving those to theme spacing made the Go card
// visibly denser and also changed its total height.
func hoverSpacer(height float32) gui.View {
	return gui.Rectangle(gui.RectangleCfg{
		Width:  1,
		Height: height,
		Sizing: gui.FixedFit,
		Color:  gui.ColorTransparent,
	})
}

// cardStyle is a card text style: a fixed size, an explicit alignment and a
// translucent white matching the C# painter. Passing gray as an opaque RGB
// value was subtly wrong: C# uses white with alpha 150/245/110 over the glass
// surface, not opaque grey 150/245/110.
func cardStyle(align gui.TextAlignment, size float32, alpha uint8) gui.TextStyle {
	return gui.TextStyle{
		Family: "Segoe UI",
		Size:   size,
		Align:  align,
		Color:  gui.RGBA(255, 255, 255, alpha),
	}
}

// rateView is the live tok/s readout, right-aligned in the header. It is absent
// rather than zero when nothing is burning: "0.0 tok/s" reads as a measurement,
// and the honest answer is that there is no fire.
func rateView(model HoverModel) gui.View {
	text := ""
	if model.ShowLiveRate && model.TokensPerSecond > 0 {
		text = format1(model.TokensPerSecond) + " " + core.T("hover.tokensS")
	}
	return gui.Text(gui.TextCfg{
		Text:      text,
		TextStyle: cardStyle(gui.TextAlignRight, hoverCardFooterSize, 110),
		Sizing:    gui.FitFit,
	})
}

func hoverCardRow(row HoverRow) gui.View {
	accent := fire.SourceFlameColors.Accent(row.Source)

	name := row.Source.DisplayName()
	if row.Estimated {
		name += " · " + core.T("hover.estimate")
	}

	// The dot is a glyph rather than a shape.
	//
	// A Rectangle in a Row does not share the text baseline — go-gui aligns
	// child boxes, and a fixed 8px box centres to a different line than a 12px
	// run of text, which left the dot floating above its own label. A "●" in
	// the source colour rides the same baseline as the name beside it and needs
	// no alignment gymnastics. It also means the whole row is text, which is
	// what makes the columns line up at all.
	dotStyle := cardStyle(gui.TextAlignLeft, hoverCardRowSize, 255)
	dotStyle.Color = gui.RGB(byte(accent[0]*255), byte(accent[1]*255), byte(accent[2]*255))

	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFixed,
		Height:  hoverCardRowH,
		Spacing: gui.SomeF(6),
		Padding: gui.NoPadding,
		Content: []gui.View{
			gui.Text(gui.TextCfg{Text: "●", TextStyle: dotStyle, Sizing: gui.FitFit}),
			gui.Text(gui.TextCfg{
				Text:      name,
				TextStyle: cardStyle(gui.TextAlignLeft, hoverCardRowSize, 150),
				Sizing:    gui.FillFit,
			}),
			gui.Text(gui.TextCfg{
				Text:      core.Compact(int64(row.Tokens)),
				TextStyle: cardStyle(gui.TextAlignRight, hoverCardRowSize, 245),
				Sizing:    gui.FitFit,
			}),
		},
	})
}

// HoverWindowTitle identifies the hover card's own window. It must not collide
// with FlameWindowTitle: both are located by exact title match.
const HoverWindowTitle = "CodingFire hover"

// hoverCardState carries the model into the card's window.
//
// Visible is separate from the model because the card's window is created at
// startup and lives for the whole session — go-gui has no show/hide API, so the
// card is parked off-screen instead of being destroyed and recreated on every
// hover. While Visible is false the view renders nothing at all, which keeps the
// window's first frames from flashing an empty card at whatever corner the
// toolkit happened to pick.
type hoverCardState struct {
	Model   HoverModel
	Visible bool
}

func hoverCardWindowView(w *gui.Window) gui.View {
	s := gui.State[hoverCardState](w)
	if !s.Visible {
		// An explicit transparent fill rather than an unset colour: an unset
		// container colour falls back to the theme's surface, which would paint
		// a solid rectangle wherever the parked card happens to be.
		return gui.Column(gui.ContainerCfg{
			ID:      "hovercard.hidden",
			Sizing:  gui.FillFill,
			Padding: gui.NoPadding,
			Color:   gui.ColorTransparent,
		})
	}
	// No wrapping container: a plain container would add the theme's default
	// padding, which insets the card from the window edge and makes the sample
	// a picture of a card floating in a box rather than of the card itself.
	views := HoverCardView(s.Model)
	return views[0]
}

// RenderHoverCardPNG renders the hover card offscreen, for --render.
//
// It goes through the toolkit's software backend rather than a bespoke painter,
// so the sample is the same view the app shows — a sample drawn by separate code
// would be a picture of a card, not of this card.
func RenderHoverCardPNG(model HoverModel, path string, scale float32) error {
	w := gui.NewWindow(gui.WindowCfg{
		State:       &hoverCardState{Model: model, Visible: true},
		Title:       "hover card",
		Width:       hoverCardWidth,
		Height:      int(hoverCardHeight(model)),
		Decorations: gui.DecorationNone,
		BgColor:     gui.RGBA(26, 22, 20, 255),
		OnInit: func(w *gui.Window) {
			w.SetView(hoverCardWindowView)
		},
	})
	return soft.RenderToPNG(w, scale, path)
}

// format1 renders a float with one decimal, matching the C# "0.0" format.
func format1(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	scaled := int(v*10 + 0.5)
	whole := scaled / 10
	frac := scaled % 10
	s := itoaInt(whole) + "." + itoaInt(frac)
	if neg {
		return "-" + s
	}
	return s
}

func itoaInt(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
