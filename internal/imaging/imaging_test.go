package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func pngOf(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: 128})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func TestProcessReencodesToJPEGAndResizes(t *testing.T) {
	res, err := Process(pngOf(1000, 2000))
	if err != nil {
		t.Fatal(err)
	}
	if res.Height != 800 || res.Width != 400 {
		t.Errorf("size %dx%d", res.Width, res.Height)
	}
	for name, data := range map[string][]byte{"full": res.Full, "thumb": res.Thumb} {
		if _, format, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || format != "jpeg" {
			t.Errorf("%s: format %q err %v", name, format, err)
		}
	}
	if cfg, _ := jpeg.DecodeConfig(bytes.NewReader(res.Thumb)); cfg.Height != 240 {
		t.Errorf("thumb height %d", cfg.Height)
	}
}

func TestProcessDoesNotUpscale(t *testing.T) {
	res, err := Process(pngOf(50, 100))
	if err != nil || res.Height != 100 {
		t.Errorf("%+v %v", res, err)
	}
}

func TestProcessRejects(t *testing.T) {
	huge := pngOf(9000, 2) // header claims > maxDimension
	for name, data := range map[string][]byte{
		"empty": nil, "text": []byte("<svg onload=alert(1)>"), "truncated": pngOf(10, 10)[:30],
		"too wide": huge, "too big": make([]byte, MaxInputBytes+1),
		"polyglot html": append([]byte("GIF89a"), []byte("<script>")...),
	} {
		if _, err := Process(data); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
