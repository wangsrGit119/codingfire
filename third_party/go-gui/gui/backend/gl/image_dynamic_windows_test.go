//go:build windows

package gl

import (
	"bytes"
	"os"
	"runtime"
	"testing"
	"unsafe"

	"github.com/go-gui-org/go-gui/gui"
	gogl "github.com/go-gui-org/go-gui/gui/backend/internal/glbind"
)

// Opt in on a Windows desktop with OpenGL. The native test window stays hidden
// throughout, and no application settings, tray icons or log scanners run.
func TestDynamicTextureGPU(t *testing.T) {
	if os.Getenv("CODINGFIRE_GL_TEST") != "1" {
		t.Skip("set CODINGFIRE_GL_TEST=1 to test the real GPU")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hInst, _, _ := pGetModuleHandleW.Call(0)
	registerClass(hInst)
	hwnd, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 16, 16, 0, 0, hInst, 0)
	if hwnd == 0 {
		t.Fatal(err)
	}
	b := &Backend{}
	b.plat.hwnd = hwnd
	registerWindow(hwnd, b)
	defer b.plat.destroy()
	b.plat.hdc, _, _ = pGetDC.Call(hwnd)
	if b.plat.hdc == 0 {
		t.Fatal("GetDC failed")
	}
	b.plat.hglrc, err = createContext(b.plat.hdc)
	if err != nil {
		t.Fatal(err)
	}
	if err := gogl.InitWithProcAddrFunc(glProc); err != nil {
		t.Fatal(err)
	}
	b.initCaches(gui.WindowCfg{})
	defer b.textures.DestroyAll()
	const key = "test/gpu-dynamic"
	defer gui.DropImage(key)
	pixels := []byte{1, 2, 3, 255}
	src := gui.UpdateImage(key, 1, 1, pixels)
	first, ok := b.resolveImageTexture(src)
	if !ok || first.id == 0 {
		t.Fatal("first upload failed")
	}
	for i := 0; i < 300; i++ {
		pixels[0] = byte(i)
		gui.UpdateImage(key, 1, 1, pixels)
		tex, ok := b.resolveImageTexture(src)
		if !ok || tex.id != first.id || b.textures.Len() != 1 {
			t.Fatal("animation allocated additional textures")
		}
	}
	read := func(tex glTexture, want []byte) {
		t.Helper()
		got := make([]byte, len(want))
		gogl.BindTexture(gogl.TEXTURE_2D, tex.id)
		p := opengl32.NewProc("glGetTexImage")
		p.Call(uintptr(gogl.TEXTURE_2D), 0, uintptr(gogl.RGBA), uintptr(gogl.UNSIGNED_BYTE), uintptr(unsafe.Pointer(&got[0])))
		gogl.BindTexture(gogl.TEXTURE_2D, 0)
		if !bytes.Equal(got, want) {
			t.Fatalf("GPU pixels = %v, want %v", got, want)
		}
	}
	read(first, pixels)
	pixels = []byte{9, 8, 7, 255, 6, 5, 4, 255}
	gui.UpdateImage(key, 2, 1, pixels)
	resized, ok := b.resolveImageTexture(src)
	if !ok || resized.w != 2 || b.textures.Len() != 1 {
		t.Fatal("texture resize failed")
	}
	read(resized, pixels)
}
