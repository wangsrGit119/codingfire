//go:build linux || darwin || windows

package glyph

import "testing"

func TestReleaseFaceCachePreservesActiveFaces(t *testing.T) {
	c := newFaceLRU(8, 1024)
	face := &cachedFace{}
	c.add("font", face, 100)
	active, ok := c.get("font")
	if !ok {
		t.Fatal("missing face")
	}
	if got := c.release(); got != 100 {
		t.Fatalf("released %d bytes", got)
	}
	if _, ok := c.get("font"); ok {
		t.Fatal("cache retained face")
	}
	if active != face || c.used != 0 || c.ll.Len() != 0 {
		t.Fatal("bad cache reset")
	}
	c.add("next", face, 200)
	if _, ok := c.get("next"); !ok {
		t.Fatal("cache cannot be reused")
	}
}
