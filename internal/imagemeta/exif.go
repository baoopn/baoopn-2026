package imagemeta

import (
	"bytes"
	"encoding/binary"
)

// Orientation returns the EXIF orientation (1-8) stored in a JPEG (APP1),
// PNG (eXIf), WebP (EXIF chunk) or TIFF file; 1 when there is none or it is
// out of range. HEIC always reports 1: its decoder applies the transforms.
func Orientation(data []byte, ctype string) int {
	var tiff []byte
	switch ctype {
	case JPEG:
		tiff = jpegExif(data)
	case PNG:
		tiff = pngExif(data)
	case WebP:
		tiff = webpChunk(data, "EXIF")
		tiff = bytes.TrimPrefix(tiff, []byte("Exif\x00\x00"))
	case TIFF:
		tiff = data
	}
	if o := tiffOrientation(tiff); o >= 1 && o <= 8 {
		return o
	}
	return 1
}

// jpegExif returns the TIFF block of the first Exif APP1 segment.
func jpegExif(d []byte) []byte {
	if len(d) < 4 || d[0] != 0xFF || d[1] != 0xD8 {
		return nil
	}
	for i := 2; i+4 <= len(d); {
		if d[i] != 0xFF {
			return nil
		}
		marker := d[i+1]
		switch {
		case marker == 0xFF: // fill byte
			i++
			continue
		case marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7):
			i += 2
			continue
		case marker == 0xDA || marker == 0xD9: // start of scan / end: no more metadata
			return nil
		}
		n := int(binary.BigEndian.Uint16(d[i+2:]))
		if n < 2 || i+2+n > len(d) {
			return nil
		}
		seg := d[i+4 : i+2+n]
		if marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return seg[6:]
		}
		i += 2 + n
	}
	return nil
}

// pngExif returns the eXIf chunk's data.
func pngExif(d []byte) []byte {
	if len(d) < 8 || string(d[:8]) != "\x89PNG\r\n\x1a\n" {
		return nil
	}
	for i := 8; i+12 <= len(d); {
		n := int(binary.BigEndian.Uint32(d[i:]))
		typ := string(d[i+4 : i+8])
		if n < 0 || i+12+n > len(d) {
			return nil
		}
		if typ == "eXIf" {
			return d[i+8 : i+8+n]
		}
		if typ == "IEND" {
			return nil
		}
		i += 12 + n
	}
	return nil
}

// webpChunk returns the data of the first RIFF chunk with the given FourCC.
func webpChunk(d []byte, fourcc string) []byte {
	if len(d) < 12 || string(d[:4]) != "RIFF" || string(d[8:12]) != "WEBP" {
		return nil
	}
	for i := 12; i+8 <= len(d); {
		n := int(binary.LittleEndian.Uint32(d[i+4:]))
		if n < 0 || i+8+n > len(d) {
			return nil
		}
		if string(d[i:i+4]) == fourcc {
			return d[i+8 : i+8+n]
		}
		i += 8 + n + n&1
	}
	return nil
}

// webpAnimated reports the animation flag of the VP8X header.
func webpAnimated(d []byte) bool {
	x := webpChunk(d, "VP8X")
	return len(x) >= 1 && x[0]&0x02 != 0
}

// tiffOrientation reads tag 0x0112 from IFD0 of a TIFF structure.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	if bo.Uint16(t[2:]) != 42 {
		return 0
	}
	off := int(bo.Uint32(t[4:]))
	if off < 8 || off+2 > len(t) {
		return 0
	}
	n := int(bo.Uint16(t[off:]))
	for k := range n {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			return 0
		}
		if bo.Uint16(t[e:]) == 0x0112 && bo.Uint16(t[e+2:]) == 3 { // SHORT
			return int(bo.Uint16(t[e+8:]))
		}
	}
	return 0
}

// gifFrames counts the image descriptors of a GIF without decoding them
// (stops counting at 2).
func gifFrames(d []byte) int {
	if len(d) < 13 || string(d[:3]) != "GIF" {
		return 0
	}
	i := 13
	if d[10]&0x80 != 0 {
		i += 3 << (int(d[10]&0x07) + 1)
	}
	skipSub := func(i int) int { // data sub-blocks until the 0 terminator
		for i < len(d) {
			n := int(d[i])
			i++
			if n == 0 {
				return i
			}
			i += n
		}
		return len(d)
	}
	frames := 0
	for i < len(d) && frames < 2 {
		switch d[i] {
		case 0x21: // extension: label, sub-blocks
			i = skipSub(i + 2)
		case 0x2C: // image descriptor
			if i+10 > len(d) {
				return frames
			}
			frames++
			flags := d[i+9]
			i += 10
			if flags&0x80 != 0 {
				i += 3 << (int(flags&0x07) + 1)
			}
			i = skipSub(i + 1) // LZW minimum code size, then data
		default: // 0x3B trailer or garbage
			return frames
		}
	}
	return frames
}
