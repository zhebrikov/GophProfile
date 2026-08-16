package services

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"sync"
)

var (
	placeholderOnce sync.Once
	placeholderPNG  []byte
)

func DefaultPlaceholder() ([]byte, string) {
	placeholderOnce.Do(func() {
		img := image.NewRGBA(image.Rect(0, 0, 300, 300))
		bg := color.RGBA{R: 22, G: 48, B: 43, A: 255}
		fg := color.RGBA{R: 61, G: 207, B: 154, A: 255}
		for y := 0; y < 300; y++ {
			for x := 0; x < 300; x++ {
				img.Set(x, y, bg)
			}
		}
		// simple circle-ish avatar mark
		cx, cy, r := 150, 150, 70
		for y := 0; y < 300; y++ {
			for x := 0; x < 300; x++ {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy <= r*r {
					img.Set(x, y, fg)
				}
			}
		}
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		placeholderPNG = buf.Bytes()
	})
	return placeholderPNG, "image/png"
}
