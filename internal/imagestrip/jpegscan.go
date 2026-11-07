package imagestrip

import (
	"bytes"
	"encoding/binary"
)

// jpegScans appends the scans of d, starting at the first SOS marker at i,
// to out and ends the result with EOI. It keeps the tables that may sit
// between progressive scans (DHT, DAC, DQT, DRI, DNL, SOFn) and drops every
// APPn and COM segment found after the first scan, whatever it holds: ICC,
// JFIF and Adobe are only valid before it. Bytes after EOI are never copied.
// A file that ends inside entropy-coded data is closed with EOI rather than
// rejected, since the browser shows what it can of it anyway.
func jpegScans(out, d []byte, i int) ([]byte, error) {
	for {
		// d[i] is 0xFF and d[i+1] is the marker code.
		m := d[i+1]
		switch {
		case m == 0xD9:
			return append(out, 0xFF, 0xD9), nil
		case m == 0xDA, m == 0xDB, m == 0xDD, m == 0xDC,
			m >= 0xC0 && m <= 0xCF && m != 0xC8, // SOFn, DHT, DAC
			m >= 0xE0 && m <= 0xEF, m == 0xFE:
		default:
			return nil, ErrMalformed
		}
		if i+4 > len(d) {
			return nil, ErrMalformed
		}
		n := int(binary.BigEndian.Uint16(d[i+2:]))
		if n < 2 || i+2+n > len(d) {
			return nil, ErrMalformed
		}
		end := i + 2 + n
		if m < 0xE0 { // APPn and COM are dropped
			out = append(out, d[i:end]...)
		}
		i = end
		if m == 0xDA {
			var dataEnd int
			dataEnd, i = entropyEnd(d, i)
			out = append(out, d[end:dataEnd]...)
			if i < 0 {
				return append(out, 0xFF, 0xD9), nil
			}
		} else if i+2 > len(d) || d[i] != 0xFF {
			return nil, ErrMalformed
		} else {
			i = skipFill(d, i)
			if i < 0 {
				return nil, ErrMalformed
			}
		}
	}
}

// entropyEnd walks entropy-coded data from i. It returns where the data
// ends and the index of the 0xFF that starts the next marker (fill bytes
// skipped), or -1 when the data runs to the end of d. FF00 (a stuffed byte)
// and RST0-RST7 belong to the data.
func entropyEnd(d []byte, i int) (dataEnd, marker int) {
	for {
		k := bytes.IndexByte(d[i:], 0xFF)
		if k < 0 {
			return len(d), -1
		}
		k += i
		if k+1 >= len(d) {
			return k, -1 // a lone trailing 0xFF is not data
		}
		c := d[k+1]
		if c == 0x00 || c >= 0xD0 && c <= 0xD7 {
			i = k + 2
			continue
		}
		return k, skipFill(d, k)
	}
}

// skipFill returns the index of the last 0xFF in the run that starts at i,
// so that d[result+1] is the marker code, or -1 if the run reaches the end.
func skipFill(d []byte, i int) int {
	for i+1 < len(d) && d[i+1] == 0xFF {
		i++
	}
	if i+1 >= len(d) {
		return -1
	}
	return i
}
