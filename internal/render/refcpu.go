package render

import (
	"image"
	"image/color"
)

// bgRGBA matches gmath.Hex(0x10141C) (the job background).
var bgRGBA = color.RGBA{R: 0x10, G: 0x14, B: 0x1C, A: 255}

// imageNewRGBAColor builds an opaque RGBA color.
func imageNewRGBAColor(r, g, b, a uint8) color.RGBA {
	return color.RGBA{R: r, G: g, B: b, A: a}
}

// unusedImage keeps the image import honest for the reference rasterizer.
var _ image.Image = (image.Image)(nil)
