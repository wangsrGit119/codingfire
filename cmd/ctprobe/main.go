package main

// Diagnostic entry point: render one console tab headlessly, without showing a
// window. Used to check the console layout and to regenerate the render samples
// under perf-artifacts/. Not part of the shipped binary.

import (
	"fmt"
	"os"
	"strconv"

	"github.com/wangsrGit119/codingfire/internal/ui"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "inputs" {
		fmt.Println(ui.ProbeStatsInputs())
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "layout" {
		width, height := 0, 0
		minWidth := float32(769)
		if len(os.Args) >= 4 {
			w, err1 := strconv.Atoi(os.Args[2])
			h, err2 := strconv.Atoi(os.Args[3])
			if err1 != nil || err2 != nil {
				fmt.Fprintln(os.Stderr, "bad width/height")
				os.Exit(2)
			}
			width, height = w, h
		}
		if len(os.Args) >= 5 {
			if m, err := strconv.Atoi(os.Args[4]); err == nil {
				minWidth = float32(m)
			}
		}
		fmt.Print(ui.ProbeConsoleLayout(width, height, minWidth))
		return
	}
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: ctprobe <tab-index> <out.png> | ctprobe inputs | ctprobe layout [w h [min-width]]")
		os.Exit(2)
	}
	tab, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad tab index:", err)
		os.Exit(2)
	}
	if err := ui.ProbeConsolePNG(tab, os.Args[2], 1); err != nil {
		fmt.Fprintln(os.Stderr, "probe failed:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", os.Args[2])
}
