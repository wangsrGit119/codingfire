//go:build windows

package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/go-gui-org/go-gui/gui"
	"github.com/wangsrGit119/codingfire/internal/fire"
	"golang.org/x/sys/windows"
)

// Opt-in whole-process comparison. All writes use a temporary data directory;
// the probe does not change autostart or show any window/tray. Run in separate
// fresh processes with CODINGFIRE_OVERLAY_BACKEND=gl and =bitmap.
func TestOverlayMemoryProbe(t *testing.T) {
	if os.Getenv("CODINGFIRE_MEMORY_PROBE") != "1" {
		t.Skip("opt-in memory probe")
	}
	dir := t.TempDir()
	for _, name := range []string{"usage.ndjson", "cursors.json", "meta.json"} {
		if body, err := os.ReadFile(filepath.Join(os.Getenv("APPDATA"), "CodingFireGo", name)); err == nil {
			if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("CODINGFIRE_DATA_DIR", dir)
	readMemory := func(stage string) {
		var counters struct {
			Size, Faults                                                                                          uint32
			PeakWorkingSet, WorkingSet, PeakPaged, Paged, PeakNonPaged, NonPaged, Pagefile, PeakPagefile, Private uintptr
		}
		counters.Size = uint32(unsafe.Sizeof(counters))
		getMemory := windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
		ok, _, err := getMemory.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
		if ok == 0 {
			t.Error(err)
			return
		}
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		out, _ := json.Marshal(map[string]any{"stage": stage, "workingSetMiB": float64(counters.WorkingSet) / (1 << 20), "privateMiB": float64(counters.Private) / (1 << 20), "heapMiB": float64(mem.HeapAlloc) / (1 << 20), "heapInuseMiB": float64(mem.HeapInuse) / (1 << 20), "goSysMiB": float64(mem.Sys) / (1 << 20), "gc": mem.NumGC})
		t.Log(string(out))
	}
	readMemory("process-start")
	a := newApp(false)
	a.probeHidden = true
	preview := fire.PreviewRoar
	a.Fire.PreviewStyle = &preview
	readMemory("store-loaded")
	go func() {
		time.Sleep(8 * time.Second)
		readMemory("flame")
		a.showHover()
		time.Sleep(2 * time.Second)
		readMemory("hover")
		a.parkHover()
		a.OpenConsole(ConsoleTabSettings)
		time.Sleep(3 * time.Second)
		readMemory("console")
		a.mu.Lock()
		console := a.console
		a.mu.Unlock()
		if console != nil {
			console.QueueCommand(func(w *gui.Window) { gui.DispatchCloseRequest(w) })
		}
		time.Sleep(3 * time.Second)
		readMemory("console-closed")
		a.gw.QueueCommand(func(*gui.Window) { a.shutdown() })
	}()
	a.Run()
}
