package gui

import "testing"

func TestDynamicImageReusesBufferAndOwnsCopy(t *testing.T) {
	const key = "test/dynamic"
	defer DropImage(key)
	pixels := []byte{10, 20, 30, 255}
	src := UpdateImage(key, 1, 1, pixels)
	_, _, first, ok := LookupDynamicImage(src)
	if !ok {
		t.Fatal("dynamic image missing")
	}
	pixels[0] = 99
	if first[0] != 10 {
		t.Fatal("registry aliases caller buffer")
	}
	for i := 0; i < 300; i++ {
		if UpdateImage(key, 1, 1, pixels) != src {
			t.Fatal("source key changed")
		}
	}
	w, h, next, ok := LookupImage(src)
	if !ok || w != 1 || h != 1 || next[0] != 99 || &first[0] != &next[0] {
		t.Fatal("buffer not updated in place")
	}
	if allocs := testing.AllocsPerRun(100, func() { UpdateImage(key, 1, 1, pixels) }); allocs != 0 {
		t.Fatalf("update allocations = %v", allocs)
	}
	if UpdateImage(key, 2, 1, make([]byte, 8)) != src {
		t.Fatal("resize changed source key")
	}
	w, h, next, ok = LookupDynamicImage(src)
	if !ok || w != 2 || h != 1 || len(next) != 8 {
		t.Fatal("resize not applied")
	}
	DropImage(key)
	if _, _, _, ok := LookupDynamicImage(src); ok {
		t.Fatal("dropped image retained")
	}
}

func TestDynamicImageDoesNotChangeStaticImageContract(t *testing.T) {
	const key = "test/static"
	defer DropImage(key)
	src := UseImage(key, 1, 1, []byte{1, 2, 3, 255})
	UseImage(key, 1, 1, []byte{99, 2, 3, 255})
	if _, _, _, ok := LookupDynamicImage(src); ok {
		t.Fatal("static image classified as dynamic")
	}
	_, _, pixels, _ := LookupImage(src)
	if pixels[0] != 1 {
		t.Fatal("static content changed")
	}
	for _, dims := range [][2]int{{0, 1}, {-1, 1}, {4097, 1}, {2, 2}} {
		if UpdateImage("invalid", dims[0], dims[1], []byte{1, 2, 3, 4}) != "" {
			t.Fatal("invalid image accepted")
		}
	}
}
