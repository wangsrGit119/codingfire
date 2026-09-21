package ui

// Diagnostic probes for cmd/ctprobe, which renders an overlay or a console tab
// headlessly so the layout can be measured without a screen. Not part of the
// shipped binary.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend/soft"
)

// ProbeConsoleLayout dumps the geometry of every console tab's scroll area at
// the given window size.
//
// The scrollbar question is a measurement, not a matter of taste: go-gui shows
// a vertical bar when the content is taller than the viewport, so what matters
// is the last child's bottom edge against the container's height. Printing the
// numbers makes it possible to size the window from evidence instead of
// widening it until it looks better.
func ProbeConsoleLayout(width, height int, minWidth float32) string {
	// Zero means "the size the app actually opens at", so the probe cannot
	// drift away from OpenConsole.
	if width <= 0 {
		width = consoleWindowW
	}
	if height <= 0 {
		height = consoleWindowH
	}

	a := newApp(false)
	a.Monitor.Start()
	deadline := time.Now().Add(30 * time.Second)
	for a.Monitor.IsScanning() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond)

	var b strings.Builder
	fmt.Fprintf(&b, "window %dx%d\n", width, height)

	for _, id := range consoleTabIDs {
		w := gui.NewWindow(gui.WindowCfg{
			State:  &consoleState{App: a, Tab: id},
			Title:  "console layout probe",
			Width:  width,
			Height: height,
		})
		root := w.TestRender(consoleView)

		ly := probeFindByLeaf(root, "console."+id+".scroll")
		if ly == nil {
			fmt.Fprintf(&b, "%s: scroll container not found\n", id)
			continue
		}
		sh := ly.Shape
		fmt.Fprintf(&b, "\n%s  viewport x=%.0f y=%.0f w=%.0f h=%.0f  children=%d\n",
			id, sh.X, sh.Y, sh.Width, sh.Height, len(ly.Children))

		bottom := sh.Y
		for i := range ly.Children {
			c := &ly.Children[i]
			if c.Shape == nil {
				continue
			}
			cs := c.Shape
			if cs.Y+cs.Height > bottom {
				bottom = cs.Y + cs.Height
			}
			fmt.Fprintf(&b, "  [%d] %-28s x=%7.1f y=%7.1f w=%7.1f h=%7.1f\n",
				i, probeChildName(cs.ID), cs.X, cs.Y, cs.Width, cs.Height)
		}
		// go-gui paints a scrollbar's thumb ColorTransparent when there is
		// nothing to scroll, so the thumb's colour is the same test the engine
		// uses. Reading it beats inferring the answer from geometry.
		fmt.Fprintf(&b, "  h-scrollbar %s   v-scrollbar %s\n",
			probeBarState(ly, len(ly.Children)-2), probeBarState(ly, len(ly.Children)-1))

		overflow := bottom - (sh.Y + sh.Height)
		verdict := "fits"
		if overflow > 0.5 {
			verdict = fmt.Sprintf("OVERFLOWS by %.0f", overflow)
		}
		fmt.Fprintf(&b, "  -> content bottom %.0f vs viewport bottom %.0f: %s\n",
			bottom, sh.Y+sh.Height, verdict)

		// The horizontal bar is the one the user actually meets on a fixed-width
		// window, so name the nodes responsible instead of leaving the reader to
		// bisect the tree.
		fmt.Fprintf(&b, "  nodes wider than %.0f px (viewport is %.0f):\n", minWidth, sh.Width)
		for _, wid := range probeWidest(ly, minWidth, 12) {
			fmt.Fprintf(&b, "    %7.1f px  %s\n", wid.width, wid.chain)
		}
	}
	return b.String()
}

// probeBarState reports whether the nth child of a scroll container is a
// visible scrollbar. A scrollable appends its two bars last, horizontal before
// vertical, so the caller passes len-2 and len-1.
func probeBarState(scroll *gui.Layout, index int) string {
	if index < 0 || index >= len(scroll.Children) {
		return "n/a"
	}
	bar := &scroll.Children[index]
	if bar.Shape == nil || len(bar.Children) == 0 {
		return "n/a"
	}
	// thumbIndex is 0 in go-gui; the thumb is the bar's only painted child.
	thumb := &bar.Children[0]
	if thumb.Shape == nil {
		return "n/a"
	}
	if thumb.Shape.Color == gui.ColorTransparent {
		return fmt.Sprintf("HIDDEN  (track %.0f x %.0f, thumb h %.0f)",
			bar.Shape.Width, bar.Shape.Height, thumb.Shape.Height)
	}
	return fmt.Sprintf("SHOWN   (track %.0f x %.0f, thumb h %.0f)",
		bar.Shape.Width, bar.Shape.Height, thumb.Shape.Height)
}

type probeWide struct {
	width float32
	chain string
}

// probeWidest collects the nodes inside root that are wider than limit, widest
// first. The chain is the ancestor IDs joined with ">", which is what makes the
// result actionable: it names the container to go and fix.
func probeWidest(root *gui.Layout, limit float32, top int) []probeWide {
	var found []probeWide
	var walk func(l *gui.Layout, chain string)
	walk = func(l *gui.Layout, chain string) {
		if l == nil || l.Shape == nil {
			return
		}
		here := chain
		if l.Shape.ID != "" {
			if here == "" {
				here = l.Shape.ID
			} else {
				here = here + " > " + l.Shape.ID
			}
		}
		if l.Shape.Width > limit {
			label := here
			if label == "" {
				label = "(no id)"
			}
			found = append(found, probeWide{l.Shape.Width, label})
		}
		for i := range l.Children {
			walk(&l.Children[i], here)
		}
	}
	walk(root, "")
	sort.SliceStable(found, func(i, j int) bool { return found[i].width > found[j].width })
	if len(found) > top {
		found = found[:top]
	}
	return found
}

// probeChildName keeps the dump readable when a node has no ID.
func probeChildName(id string) string {
	if id == "" {
		return "(no id)"
	}
	return id
}

// probeFindByLeaf returns the first node whose leaf ID equals name.
//
// Layout.FindByID resolves against the *effective* ID, which is the leaf joined
// to every ID-bearing ancestor — a tab's content ends up something like
// console.root.console.tabs.console.stats.scroll, so the leaf alone does not
// find it. Matching the leaf is what the builder actually wrote, and the leaf
// names in this console are unique.
func probeFindByLeaf(root *gui.Layout, name string) *gui.Layout {
	if root == nil || root.Shape == nil {
		return nil
	}
	if root.Shape.ID == name {
		return root
	}
	for i := range root.Children {
		if found := probeFindByLeaf(&root.Children[i], name); found != nil {
			return found
		}
	}
	return nil
}

// ProbeConsolePNG renders one console tab headlessly so the layout can be
// inspected without a screen.
//
// newApp(false), not NewApp(): the probe must not push the autostart setting
// into the platform's login entry, which a plain NewApp does on every call. A
// diagnostic run that quietly repoints the user's login item at a throwaway
// probe binary is a bug, not a side effect.
func ProbeConsolePNG(tab int, path string, scale float32) error {
	a := newApp(false)
	a.Monitor.Start()
	deadline := time.Now().Add(30 * time.Second)
	for a.Monitor.IsScanning() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond)

	if tab < 0 || tab >= len(consoleTabIDs) {
		tab = ConsoleTabStats
	}

	w := gui.NewWindow(gui.WindowCfg{
		State:  &consoleState{App: a, Tab: consoleTabIDs[tab]},
		Title:  "console probe",
		Width:  consoleWindowW,
		Height: consoleWindowH,
		OnInit: func(w *gui.Window) { w.SetView(consoleView) },
	})
	return soft.RenderToPNG(w, scale, path)
}

// ProbeStatsInputs summarises what the stats tab is about to draw, and likewise
// leaves the login entry alone.
func ProbeStatsInputs() string {
	a := newApp(false)
	a.Monitor.Start()
	deadline := time.Now().Add(30 * time.Second)
	for a.Monitor.IsScanning() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond)

	h := a.Monitor.TodayHourly()
	nonzero := 0
	for _, v := range h {
		if v.Tokens > 0 {
			nonzero++
		}
	}
	b := a.Monitor.TodayBreakdown()

	// The model list is the point of this probe when the question is whether
	// model names survive the whole trip from log to console.
	models := a.Monitor.TodayByModel()
	names := make([]string, 0, len(models))
	for name, tokens := range models {
		names = append(names, name+"="+itoaInt(tokens))
	}
	sort.Strings(names)

	return "models=" + itoaInt(len(models)) + " [" + strings.Join(names, " ") + "]" +
		" total=" + itoaInt(a.Monitor.TodayTokens()) +
		" hours=" + itoaInt(len(h)) + " nonZeroHours=" + itoaInt(nonzero) +
		" hourMax=" + itoaInt(hourMax(h)) + " peak=" + itoaInt(peakHour(h)) +
		" input=" + itoaInt(derefInt(b.Input)) + " output=" + itoaInt(derefInt(b.Output)) +
		" cacheRead=" + itoaInt(derefInt(b.CacheRead)) + " cacheWrite=" + itoaInt(derefInt(b.CacheWrite)) +
		" sources=" + itoaInt(len(a.Monitor.TodayBySource()))
}
