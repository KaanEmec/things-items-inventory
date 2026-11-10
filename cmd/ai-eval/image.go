package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
)

// maxEdge is the long edge photos are scaled down to, like the app's AI-size
// derivative: enough for labels, far cheaper than a 12 MP original.
const maxEdge = 1600

// loadImage reads a JPEG or PNG and returns it as a JPEG no larger than
// maxEdge on its long edge. EXIF orientation is not applied; models read
// rotated photos well, but a sideways label photo is worth re-saving upright.
func loadImage(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("not a JPEG or PNG image: %v", err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, shrink(src, maxEdge), &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// shrink scales src down so its long edge is at most edge pixels, averaging
// the source pixels under each target pixel.
func shrink(src image.Image, edge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= edge && h <= edge {
		return src
	}
	var tw, th int
	if w >= h {
		tw, th = edge, max(1, h*edge/w)
	} else {
		tw, th = max(1, w*edge/h), edge
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := range th {
		y0, y1 := b.Min.Y+y*h/th, b.Min.Y+max((y+1)*h/th, y*h/th+1)
		for x := range tw {
			x0, x1 := b.Min.X+x*w/tw, b.Min.X+max((x+1)*w/tw, x*w/tw+1)
			var r, g, bl, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := src.At(sx, sy).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(bl/n>>8), 255
		}
	}
	return dst
}
