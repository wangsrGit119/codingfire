//go:build !windows

package ui

import "image"

type nativeHoverPainter struct{}

func (*nativeHoverPainter) close()                               {}
func (*nativeHoverPainter) draw(HoverModel, float64) image.Image { return nil }
