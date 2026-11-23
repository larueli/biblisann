package gc_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

func solid(c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 12))
	for x := range 8 {
		for y := range 12 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func redPNG() []byte  { return solid(color.RGBA{R: 200, A: 255}) }
func bluePNG() []byte { return solid(color.RGBA{B: 200, A: 255}) }
