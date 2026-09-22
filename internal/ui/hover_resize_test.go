package ui

import (
	"testing"

	"github.com/go-gui-org/go-gui/gui"
	"github.com/wangsrGit119/codingfire/internal/core"
)

func TestUnfocusedHoverResizesWithContent(t *testing.T) {
	w := gui.NewTestWindow(gui.WindowCfg{Width: hoverCardWidth, Height: int(hoverCardHeight(HoverModel{}))})
	w.TestRender(func(*gui.Window) gui.View { return HoverCardView(HoverModel{})[0] })
	w.EventFn(&gui.Event{Type: gui.EventUnfocused})
	for _, count := range []int{2, 6, 1} {
		model := HoverModel{HasUpdated: true, Hourly: busyHours()}
		for i := 0; i < count; i++ {
			model.Rows = append(model.Rows, HoverRow{Source: core.UsageSourcesAll[i], Tokens: 1000})
		}
		height := int(hoverCardHeight(model))
		w.EventFn(&gui.Event{Type: gui.EventResized, WindowWidth: hoverCardWidth, WindowHeight: height})
		if width, got := w.WindowSize(); width != hoverCardWidth || got != height {
			t.Fatalf("%d apps: unfocused window stayed at %dx%d, want %dx%d", count, width, got, hoverCardWidth, height)
		}
		root := w.TestRender(func(*gui.Window) gui.View { return HoverCardView(model)[0] })
		card := probeFindByLeaf(root, "hovercard.root")
		if card == nil {
			t.Fatal("missing card")
		}
		for _, child := range card.Children {
			if child.Shape != nil && child.Shape.Y+child.Shape.Height > float32(height-hoverCardBottomPad-hoverCardBorder) {
				t.Fatalf("%d apps: content exceeds resized window", count)
			}
		}
	}
}

func TestHoverFooterStaysAtBottom(t *testing.T) {
	model := HoverModel{HasUpdated: true, Rows: []HoverRow{{Source: core.Codex, Tokens: 1000}}}
	for _, extra := range []int{0, 60} {
		height := int(hoverCardHeight(model)) + extra
		w := gui.NewTestWindow(gui.WindowCfg{Width: hoverCardWidth, Height: height})
		root := w.TestRender(func(*gui.Window) gui.View { return HoverCardView(model)[0] })
		footer := probeFindByLeaf(root, "hovercard.footer")
		if footer == nil {
			t.Fatal("missing footer")
		}
		gap := float32(height) - footer.Shape.Y - footer.Shape.Height
		if gap != hoverCardBottomPad+hoverCardBorder {
			t.Fatalf("extra height %d: footer gap %v", extra, gap)
		}
	}
}
