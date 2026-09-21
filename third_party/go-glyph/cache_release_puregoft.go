//go:build linux || darwin || windows

package glyph

// ReleaseFontCache drops the process-wide parsed-face cache after the last
// text window closes. Active fonts retain their own references, so eviction
// does not invalidate a concurrent draw. New draws can parse faces again.
// This releases references, not OS working-set pages; the host controls GC.
func ReleaseFontCache() int64 {
	return faceCache.release()
}

func (c *faceLRU) release() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	freed := c.used
	clear(c.items)
	c.ll.Init()
	c.used = 0
	return freed
}
