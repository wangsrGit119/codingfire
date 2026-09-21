//
//  main.go — CodingFire
//
//  Entry point. Besides the normal tray launch there are two headless modes, so
//  the thing can be verified without anyone watching a screen:
//
//    --dump [file]     scan the local logs and write a text report
//    --render [dir]    render each fire tier to PNG, to check pixel appearance
//
//  Both are the same shape as the C# build's, and --dump is deliberately
//  byte-compatible with it: the report exists to be pasted into an issue, and a
//  report whose format changed between builds is worse than useless.
//

package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/wangsrGit119/codingfire/internal/core"
	"github.com/wangsrGit119/codingfire/internal/data"
	"github.com/wangsrGit119/codingfire/internal/ui"
)

// singleInstanceName is the mutex guarding the tray.
//
// Deliberately different from the C# build's "CodingFire.SingleInstance": the
// two builds are meant to coexist, and a shared name would make the Go build
// exit silently whenever the C# one happened to be running — which reads as a
// crash, not as a guard.
const singleInstanceName = "CodingFire.Go.SingleInstance"

// dumpScanTimeout caps the initial baseline scan. Sources grew to include
// SQLite databases and recursive log trees, so the C# build's 60s is kept
// rather than shortened.
const dumpScanTimeout = 60 * time.Second

func main() {
	os.Exit(run())
}

func run() (code int) {
	// Headless modes have nowhere to show a dialog, so a panic has to land in a
	// file next to the executable.
	defer func() {
		if r := recover(); r != nil {
			writeCrash(fmt.Sprintf("panic: %v\n\n%s", r, debug.Stack()))
			code = 1
		}
	}()

	mode, target := parseArgs(os.Args[1:])

	switch mode {
	case modeDump:
		return dumpReport(target)
	case modeRender:
		return renderPreviews(target)
	default:
		return runGUI()
	}
}

type runMode int

const (
	modeGUI runMode = iota
	modeDump
	modeRender
)

// parseArgs accepts --dump/--render in any position, and treats the first
// non-flag argument as the target path.
func parseArgs(args []string) (runMode, string) {
	mode := modeGUI
	target := ""
	for _, a := range args {
		switch {
		case strings.EqualFold(a, "--dump"):
			mode = modeDump
		case strings.EqualFold(a, "--render"):
			mode = modeRender
		case !strings.HasPrefix(a, "--"):
			target = a
		}
	}
	return mode, target
}

func runGUI() int {
	release, alreadyRunning := singleInstance(singleInstanceName)
	if alreadyRunning {
		// A second launch is a no-op, not an error: this is what double-clicking
		// the exe twice does, and it must not put two campfires on the desktop.
		return 0
	}
	defer release()

	ui.NewApp().Run()
	return 0
}

// ---------------------------------------------------------------------------
// --dump
// ---------------------------------------------------------------------------

func dumpReport(target string) int {
	path := resolveDumpPath(target)

	var sb strings.Builder
	code := collectDump(&sb)

	// The report is written even when the scan failed: a dump containing an
	// ERROR line is far more useful than no file at all.
	if err := writeTextFile(path, sb.String()); err != nil {
		fmt.Fprintln(os.Stderr, "could not write the report:", err)
		return 1
	}
	if code == 0 {
		fmt.Println(path)
	}
	return code
}

// resolveDumpPath turns the CLI target into a file path.
//
// A directory target is a courtesy: writing a directory's path to
// File.WriteAllText produces an "access denied" that says nothing about the
// actual mistake.
func resolveDumpPath(target string) string {
	if target == "" {
		return "codingfire-dump.txt"
	}
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		return filepathJoin(target, "codingfire-dump.txt")
	}
	return target
}

func collectDump(sb *strings.Builder) (code int) {
	defer func() {
		if r := recover(); r != nil {
			sb.WriteString("ERROR: " + fmt.Sprint(r) + "\n")
			code = 1
		}
	}()

	// The report's own text — the header, the column names, "today total" — is
	// hardcoded English, so the language is pinned here too. Otherwise the
	// source display names would follow the OS language and produce an English
	// header over Chinese source names.
	core.SetLanguage(core.LangEnglish)

	store := data.NewUsageStore()
	store.Open()

	monitor := data.NewUsageMonitor(store)
	monitor.Start()

	deadline := time.Now().Add(dumpScanTimeout)
	for monitor.IsScanning() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	// Give the last batch of parses a moment to land after IsScanning clears.
	time.Sleep(600 * time.Millisecond)

	monitor.Stop()
	store.Flush()

	sb.WriteString(core.AppInfo.Name() + " " + core.Version + " — local usage report\n")
	sb.WriteString("time      : " + time.Now().Format("2006-01-02 15:04:05") + "\n")
	sb.WriteString("data dir  : " + core.AppPaths.DataDir() + "\n")
	sb.WriteString("\n")
	sb.WriteString("sources\n")
	sb.WriteString("  " + pad("source", nameCol) + pad("state", 14) + pad("today", 12) + "path\n")

	for _, st := range monitor.Statuses() {
		sb.WriteString("  " + pad(st.Source.DisplayName(), nameCol) +
			pad(st.State.RawName(), 14) +
			pad(core.Compact(int64(st.TodayTokens)), 12) +
			st.Detail + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString("today total : " + core.Grouped(int64(monitor.TodayTokens())) + "\n")
	sb.WriteString("by source   :\n")

	bySource := monitor.TodayBySource()
	for _, src := range core.UsageSourcesAll {
		if v := bySource[src]; v > 0 {
			sb.WriteString("  " + pad(src.DisplayName(), nameCol) + core.Grouped(int64(v)) + "\n")
		}
	}

	b := monitor.TodayBreakdown()
	sb.WriteString("breakdown   : input=" + core.Grouped(int64(derefInt(b.Input))) +
		" output=" + core.Grouped(int64(derefInt(b.Output))) +
		" cacheRead=" + core.Grouped(int64(derefInt(b.CacheRead))) +
		" cacheWrite=" + core.Grouped(int64(derefInt(b.CacheWrite))) + "\n")

	peakHour, peakVal := -1, 0
	for _, h := range monitor.TodayHourly() {
		if h.Tokens > peakVal {
			peakVal = h.Tokens
			peakHour = h.Hour
		}
	}
	if peakHour >= 0 {
		sb.WriteString("peak hour   : " + pad2(peakHour) + ":00 = " + core.Grouped(int64(peakVal)) + "\n")
	} else {
		sb.WriteString("peak hour   : —\n")
	}

	store.Close()
	return 0
}

// nameCol is the width of the report's source column.
//
// It has to hold the longest display name, "WorkBuddy (INTL)" at 16 characters —
// pad truncates rather than overflowing, so shrinking this silently mangles the
// names it was widened for.
const nameCol = 18

// pad right-pads to width, truncating to width-1 plus a space when the value is
// already too long. That keeps every column aligned instead of letting one long
// name shove the rest of the row sideways.
func pad(s string, width int) string {
	if len([]rune(s)) >= width {
		r := []rune(s)
		return string(r[:width-1]) + " "
	}
	return s + strings.Repeat(" ", width-len([]rune(s)))
}

func pad2(v int) string {
	if v < 10 {
		return "0" + itoa(v)
	}
	return itoa(v)
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func itoa(v int) string { return fmt.Sprintf("%d", v) }

// ---------------------------------------------------------------------------
// Crash log
// ---------------------------------------------------------------------------

func writeCrash(text string) {
	path := filepathJoin(core.AppPaths.DataDir(), "codingfire-crash.txt")
	body := time.Now().Format("2006-01-02 15:04:05") + "\n" + text
	if err := writeTextFile(path, body); err != nil {
		fmt.Fprintln(os.Stderr, "crash:", text)
	}
	fmt.Fprintln(os.Stderr, "crash log:", path)
}
