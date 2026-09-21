Based on go-glyph v1.25.2, with its original license retained.

`ReleaseFontCache` allows CodingFire to drop parsed font faces immediately
after its last text window closes. The upstream cache can otherwise
retain up to 384 MiB and waits five idle minutes before eviction. The native
fire and hover windows use no go-glyph text system. Existing draws keep their
own face references; cache eviction does not invalidate them.
