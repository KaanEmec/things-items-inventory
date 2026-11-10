package photos

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // register the PNG decoder
	"io"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register the WebP decoder
)

// Decoded size limits, checked from the header before any pixel is decoded.
const (
	maxPixels = 64_000_000 // a 64-megapixel sensor is the most a phone makes
	maxSide   = 16384
)

var (
	errUnsupported = errors.New("unsupported image type")
	errCorrupt     = errors.New("not a readable image")
	errTooBig      = errors.New("image dimensions too large")
)

// Derivative sizes: name, longest side, JPEG quality.
var sizes = []struct {
	name    string
	side    int
	quality int
}{
	{"screen", 1600, 85},
	{"ai", 1568, 85},
	{"thumb", 320, 80},
}

// sniff decides the type from the first bytes, never from a name or a
// declared content type.
func sniff(head []byte) (mime, ext string, ok bool) {
	switch {
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg", "jpg", true
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", "png", true
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "image/webp", "webp", true
	}
	return "", "", false
}

// checkImage reads the dimensions from the header, refuses a decompression
// bomb, then decodes once to prove the file is really an image. It returns
// the stored width and height, as displayed (EXIF orientation applied).
func checkImage(rs io.ReadSeeker, mime string) (w, h, orient int, err error) {
	if _, err = rs.Seek(0, io.SeekStart); err != nil {
		return
	}
	cfg, _, err := image.DecodeConfig(rs)
	if err != nil {
		return 0, 0, 0, errCorrupt
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxSide || cfg.Height > maxSide ||
		cfg.Width*cfg.Height > maxPixels {
		return 0, 0, 0, errTooBig
	}
	if _, err = rs.Seek(0, io.SeekStart); err != nil {
		return
	}
	if _, _, err = image.Decode(rs); err != nil {
		return 0, 0, 0, errCorrupt
	}
	orient = orientationOf(rs, mime)
	w, h = cfg.Width, cfg.Height
	if orient >= 5 {
		w, h = h, w
	}
	return w, h, orient, nil
}

// render scales src so its longest side is at most side (never enlarging),
// applies the EXIF orientation and encodes a JPEG. A fresh JPEG carries no
// EXIF, GPS or other metadata. Transparency is flattened onto white.
func render(src image.Image, orient, side, quality int) ([]byte, image.Image, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, nil, errCorrupt
	}
	dw, dh := w, h
	if m := max(w, h); m > side {
		dw, dh = max(1, w*side/m), max(1, h*side/m)
	}
	scaled := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.Draw(scaled, scaled.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, b, xdraw.Over, nil)
	out := orientRGBA(scaled, orient)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: quality}); err != nil {
		return nil, nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), out, nil
}

// orientRGBA turns an image upright for EXIF orientation 1..8.
func orientRGBA(src *image.RGBA, orient int) *image.RGBA {
	if orient < 2 || orient > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if orient >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch orient {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			si := src.PixOffset(sx, sy)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
