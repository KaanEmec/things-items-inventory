package photos

import (
	"bytes"
	"encoding/binary"
	"io"
)

// orientationOf returns the EXIF orientation (1..8) of an image file, or 1
// when there is none or it cannot be read. It parses only what it needs: the
// JPEG APP1 segment, the PNG eXIf chunk or the WebP EXIF chunk.
func orientationOf(r io.ReadSeeker, mime string) int {
	var tiff []byte
	switch mime {
	case "image/jpeg":
		tiff = jpegExif(r)
	case "image/png":
		tiff = pngExif(r)
	case "image/webp":
		tiff = webpExif(r)
	}
	if o := tiffOrientation(tiff); o >= 1 && o <= 8 {
		return o
	}
	return 1
}

const maxExifSize = 1 << 20

func jpegExif(r io.ReadSeeker) []byte {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	var soi [2]byte
	if _, err := io.ReadFull(r, soi[:]); err != nil || soi != [2]byte{0xFF, 0xD8} {
		return nil
	}
	for i := 0; i < 64; i++ { // metadata segments only; bounded
		var h [4]byte
		if _, err := io.ReadFull(r, h[:2]); err != nil || h[0] != 0xFF {
			return nil
		}
		m := h[1]
		for m == 0xFF { // fill bytes
			if _, err := io.ReadFull(r, h[1:2]); err != nil {
				return nil
			}
			m = h[1]
		}
		if m == 0xDA || m == 0xD9 { // start of scan / end of image
			return nil
		}
		if m == 0x01 || (m >= 0xD0 && m <= 0xD7) { // no length
			continue
		}
		if _, err := io.ReadFull(r, h[2:4]); err != nil {
			return nil
		}
		n := int(binary.BigEndian.Uint16(h[2:4])) - 2
		if n < 0 {
			return nil
		}
		if m != 0xE1 {
			if _, err := r.Seek(int64(n), io.SeekCurrent); err != nil {
				return nil
			}
			continue
		}
		seg := make([]byte, n)
		if _, err := io.ReadFull(r, seg); err != nil {
			return nil
		}
		if bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return seg[6:]
		}
	}
	return nil
}

func pngExif(r io.ReadSeeker) []byte {
	if _, err := r.Seek(8, io.SeekStart); err != nil {
		return nil
	}
	for i := 0; i < 64; i++ {
		var h [8]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return nil
		}
		n := int64(binary.BigEndian.Uint32(h[:4]))
		switch string(h[4:]) {
		case "eXIf":
			if n > maxExifSize {
				return nil
			}
			b := make([]byte, n)
			if _, err := io.ReadFull(r, b); err != nil {
				return nil
			}
			return b
		case "IDAT", "IEND":
			return nil
		}
		if _, err := r.Seek(n+4, io.SeekCurrent); err != nil { // data + CRC
			return nil
		}
	}
	return nil
}

func webpExif(r io.ReadSeeker) []byte {
	if _, err := r.Seek(12, io.SeekStart); err != nil {
		return nil
	}
	for i := 0; i < 64; i++ {
		var h [8]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return nil
		}
		n := int64(binary.LittleEndian.Uint32(h[4:]))
		if string(h[:4]) == "EXIF" {
			if n > maxExifSize {
				return nil
			}
			b := make([]byte, n)
			if _, err := io.ReadFull(r, b); err != nil {
				return nil
			}
			return bytes.TrimPrefix(b, []byte("Exif\x00\x00"))
		}
		if _, err := r.Seek(n+n%2, io.SeekCurrent); err != nil { // chunks are padded to even size
			return nil
		}
	}
	return nil
}

// tiffOrientation reads tag 0x0112 from the first IFD of a TIFF block.
func tiffOrientation(b []byte) int {
	if len(b) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(b[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	if bo.Uint16(b[2:4]) != 42 {
		return 0
	}
	off := int(bo.Uint32(b[4:8]))
	if off < 8 || off+2 > len(b) {
		return 0
	}
	count := int(bo.Uint16(b[off : off+2]))
	for i := 0; i < count; i++ {
		e := off + 2 + i*12
		if e+12 > len(b) {
			return 0
		}
		if bo.Uint16(b[e:e+2]) == 0x0112 {
			return int(bo.Uint16(b[e+8 : e+10]))
		}
	}
	return 0
}
