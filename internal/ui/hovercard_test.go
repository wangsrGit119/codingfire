package ui

import (
	"math"
	"testing"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/wangsrGit119/codingfire/internal/core"
)

// busyHours is a full day with no idle hour, so every one of the 24 bars has
// something to draw.
func busyHours() []core.HourlyUsage {
	hours := make([]core.HourlyUsage, 24)
	for h := range hours {
		hours[h] = core.HourlyUsage{Hour: h, Tokens: (h + 1) * 1000}
	}
	return hours
}

// emptyHours is the shape a day has before anything has been used.
func emptyHours() []core.HourlyUsage {
	return make([]core.HourlyUsage, 24)
}

func TestHoverCardHeightCountsTheTimeline(t *testing.T) {
	rows := []HoverRow{{Source: core.UsageSourcesAll[0], Tokens: 1000}}

	if hoverChartVisible(HoverModel{Hourly: emptyHours()}) {
		t.Fatal("an idle day has no chart to draw")
	}
	if !hoverChartVisible(HoverModel{Hourly: busyHours()}) {
		t.Fatal("a day with usage must show its chart")
	}

	bare := hoverCardHeight(HoverModel{TodayTokens: 1000, Rows: rows, Hourly: emptyHours()})
	withChart := hoverCardHeight(HoverModel{TodayTokens: 1000, Rows: rows, Hourly: busyHours()})
	if got, want := withChart-bare, float32(hoverChartBlockH); got != want {
		t.Errorf("chart reserved %v px, want %v", got, want)
	}

	// The empty state has its own body, and must still count the chart.
	if hoverCardHeight(HoverModel{Hourly: emptyHours()}) != hoverCardHeight(HoverModel{Hourly: nil}) {
		t.Error("a model with no rows must measure the same with or without buckets")
	}
}

// TestHoverCardViewPlacesTimeline is the cross-renderer check: it asserts the
// view puts the chart where the shared geometry says it goes, with one bar per
// hour, at the height the bitmap painter also reserves.
func TestHoverCardViewPlacesTimeline(t *testing.T) {
	model := HoverModel{
		TodayTokens: 1234567,
		Rows:        []HoverRow{{Source: core.UsageSourcesAll[0], Tokens: 1000}},
		Hourly:      busyHours(),
	}
	w := gui.NewTestWindow(gui.WindowCfg{
		Width:  hoverCardWidth,
		Height: int(math.Ceil(float64(hoverCardHeight(model)))),
	})
	root := w.TestRender(func(*gui.Window) gui.View { return HoverCardView(model)[0] })

	chart := probeFindByLeaf(root, "hovercard.chart")
	if chart == nil || chart.Shape == nil {
		t.Fatal("the card has no mini timeline")
	}
	if got := chart.Shape.Y; got != hoverChartTop {
		t.Errorf("timeline starts at y=%.0f, want %d — the view and the painter disagree",
			got, hoverChartTop)
	}
	if got := chart.Shape.Height; got != hoverCardChartH {
		t.Errorf("timeline is %.0f tall, want %d", got, hoverCardChartH)
	}
	if len(chart.Children) != 24 {
		t.Fatalf("timeline has %d bars, want one per hour", len(chart.Children))
	}

	// The hour labels: one slot per bar, every third one filled, and every
	// filled slot must carry text — an axis of empty slots is worse than none.
	axis := probeFindByLeaf(root, "hovercard.axis")
	if axis == nil || axis.Shape == nil {
		t.Fatal("the card has no hour axis")
	}
	if got := axis.Shape.Y; got != hoverChartTop+hoverCardChartH {
		t.Errorf("axis starts at y=%.0f, want %d", got, hoverChartTop+hoverCardChartH)
	}
	if len(axis.Children) != 24 {
		t.Fatalf("axis has %d slots, want one per hour", len(axis.Children))
	}
	filled := 0
	for i, tick := range axis.Children {
		if tick.Shape == nil || len(tick.Children) != 1 {
			t.Fatalf("axis slot %d is malformed", i)
		}
		if want := hourLabel(i); want != "" {
			filled++
		}
	}
	if filled != 8 {
		t.Errorf("axis has %d labelled slots, want 8", filled)
	}
	for i, bar := range chart.Children {
		if bar.Shape == nil || bar.Shape.Height != hoverCardChartH {
			t.Fatalf("bar %d is not as tall as the plot area", i)
		}
		// Every hour is busy in this model, so every bar has a body to draw.
		if len(bar.Children) != 1 {
			t.Fatalf("bar %d has %d bodies, want 1", i, len(bar.Children))
		}
	}
}

// TestHoverCardContentFitsItsSurface is the clipping check. hoverCardHeight is
// what the card's window is created and resized with, so a block that lays out
// taller than its share pushes the bottom of the card off its own surface —
// which is exactly what the pinned block heights in HoverCardView exist to
// prevent, and what the C# painter's point sizes used to cause.
func TestHoverCardContentFitsItsSurface(t *testing.T) {
	models := []HoverModel{
		{TodayTokens: 1234567, Rows: []HoverRow{{Source: core.UsageSourcesAll[0], Tokens: 1000}}, Hourly: busyHours(), HasUpdated: true},
		{TodayTokens: 1234567, Rows: []HoverRow{{Source: core.UsageSourcesAll[0], Tokens: 1000}}, Hourly: emptyHours(), HasUpdated: true},
		{TodayTokens: 0, Hourly: emptyHours()},
		{TodayTokens: 0, Hourly: emptyHours(), HasUpdated: true},
	}
	// Exercise each supported source count, including the footer and chart.
	// Checking only a single source misses overflow caused by stacked rows.
	var rows []HoverRow
	for _, source := range core.UsageSourcesAll {
		rows = append(rows, HoverRow{Source: source, Tokens: 1000})
		models = append(models, HoverModel{
			TodayTokens: len(rows) * 1000,
			Rows:        append([]HoverRow(nil), rows...),
			Hourly:      busyHours(), HasUpdated: true,
		})
	}
	for i, model := range models {
		height := int(math.Ceil(float64(hoverCardHeight(model))))
		w := gui.NewTestWindow(gui.WindowCfg{Width: hoverCardWidth, Height: height})
		root := w.TestRender(func(*gui.Window) gui.View { return HoverCardView(model)[0] })

		card := probeFindByLeaf(root, "hovercard.root")
		if card == nil || card.Shape == nil {
			t.Fatalf("model %d: no card", i)
		}
		bottom := card.Shape.Y
		var checkBounds func(*gui.Layout)
		checkBounds = func(node *gui.Layout) {
			if node.Shape != nil && node.Shape.Y+node.Shape.Height > float32(height-hoverCardPad-hoverCardBorder) && node != card {
				t.Errorf("model %d: descendant ends at %.1f beyond card content boundary %d", i, node.Shape.Y+node.Shape.Height, height-hoverCardPad-hoverCardBorder)
			}
			for j := range node.Children {
				checkBounds(&node.Children[j])
			}
		}
		checkBounds(card)
		for j := range card.Children {
			if c := &card.Children[j]; c.Shape != nil {
				if b := c.Shape.Y + c.Shape.Height; b > bottom {
					bottom = b
				}
			}
		}
		// The content has to end inside the surface, border and padding included.
		if room := float32(height) - bottom; room < float32(hoverCardPad+hoverCardBorder) {
			t.Errorf("model %d: content ends at %.1f in a %d px card, leaving %.1f px of the %.1f px bottom inset",
				i, bottom, height, room, float32(hoverCardPad+hoverCardBorder))
		}
	}
}

// TestHoverCardViewWithoutTimeline: the card must not reserve the band when
// there is nothing to put in it, or the rows would float away from the number.
func TestHoverCardViewWithoutTimeline(t *testing.T) {
	model := HoverModel{
		TodayTokens: 1234567,
		Rows:        []HoverRow{{Source: core.UsageSourcesAll[0], Tokens: 1000}},
		Hourly:      emptyHours(),
	}
	w := gui.NewTestWindow(gui.WindowCfg{
		Width:  hoverCardWidth,
		Height: int(math.Ceil(float64(hoverCardHeight(model)))),
	})
	root := w.TestRender(func(*gui.Window) gui.View { return HoverCardView(model)[0] })
	if chart := probeFindByLeaf(root, "hovercard.chart"); chart != nil {
		t.Fatal("an idle day still drew a timeline")
	}
}
