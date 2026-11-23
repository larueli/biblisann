// Package imaging re-encodes untrusted images. Nothing fetched or uploaded is ever served as-is:
// the output is always a freshly encoded JPEG of bounded size.
package imaging

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// Limits protect against decompression bombs.
const (
	MaxInputBytes = 5 << 20
	maxDimension  = 8000
	maxPixels     = 6_000_000
	fullHeight    = 800
	thumbHeight   = 240
)

// ErrUnsupported is returned for undecodable or oversized images.
var ErrUnsupported = errors.New("unsupported or oversized image")

// Result holds the re-encoded images.
type Result struct {
	Full          []byte
	Thumb         []byte
	Width, Height int
}

// Process decodes JPEG, PNG or GIF data and returns a full-size and a thumbnail JPEG.
func Process(data []byte) (*Result, error) {
	if len(data) == 0 || len(data) > MaxInputBytes {
		return nil, ErrUnsupported
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "gif") {
		return nil, ErrUnsupported
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxDimension || cfg.Height > maxDimension ||
		cfg.Width*cfg.Height > maxPixels {
		return nil, ErrUnsupported
	}
	var src image.Image
	switch format {
	case "jpeg":
		src, err = jpeg.Decode(bytes.NewReader(data))
	case "png":
		src, err = png.Decode(bytes.NewReader(data))
	case "gif":
		src, err = gif.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, ErrUnsupported
	}
	flat := flatten(src)
	full, w, h, err := encode(flat, fullHeight, 85)
	if err != nil {
		return nil, err
	}
	thumb, _, _, err := encode(flat, thumbHeight, 80)
	if err != nil {
		return nil, err
	}
	return &Result{Full: full, Thumb: thumb, Width: w, Height: h}, nil
}

// flatten draws src over white so transparency does not become black in JPEG.
func flatten(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}

func encode(src *image.RGBA, maxH, quality int) ([]byte, int, int, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.Image(src)
	if h > maxH {
		w = max(1, w*maxH/h)
		h = maxH
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
		out = dst
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: quality}); err != nil {
		return nil, 0, 0, fmt.Errorf("imaging: encode: %w", err)
	}
	return buf.Bytes(), w, h, nil
}
