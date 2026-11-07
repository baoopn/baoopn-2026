package imagestrip

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdata/phone holds real libjpeg output (Pillow, 75x53, quality 92):
// baseline, restart markers after every MCU (rst) and every MCU row
// (rst_rows), progressive, and progressive with restart markers. Each is
// wrapped the way phone cameras write files: EXIF, XMP, ICC, MPF and a
// comment before the image, then after EOI a second JPEG plus an MP4
// (Pixel MVIMG) or a MotionPhoto_Data trailer (Samsung).

func appSeg(marker byte, body string) []byte {
	s := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(s[2:], uint16(len(body)+2))
	return append(s, body...)
}

func mp4Box(typ, payload string) string {
	n := make([]byte, 4)
	binary.BigEndian.PutUint32(n, uint32(8+len(payload)))
	return string(n) + typ + payload
}

var (
	fakeMP4 = mp4Box("ftyp", "isom\x00\x00\x02\x00isomiso2avc1mp41") +
		mp4Box("moov", mp4Box("mvhd", strings.Repeat("\x00", 100))+"SECRETVIDEOTRACK") +
		mp4Box("mdat", strings.Repeat("\x5a", 512)+"\xff\xd9\xff\xd8\xff\xe1")
	iccSeg  = appSeg(0xE2, "ICC_PROFILE\x00\x01\x01"+strings.Repeat("\x00", 128))
	phoneHd = bytes.Join([][]byte{
		appSeg(0xE1, "Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08GPSLatitudeSECRET-48.8584N"),
		appSeg(0xE1, "http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta GCamera:MicroVideo=\"1\" Camera:MotionPhoto=\"1\" SECRETXMP/>"),
		iccSeg,
		appSeg(0xE2, "MPF\x00MM\x00\x2a\x00\x00\x00\x08\x00\x03\xb0\x00SECRETMPF"),
		appSeg(0xFE, "SECRETCOMMENT"),
	}, nil)
)

// phone wraps a plain JPEG like a camera; style is "pixel" or "samsung".
func phone(plain []byte, style string) []byte {
	d := append([]byte{0xFF, 0xD8}, phoneHd...)
	d = append(d, plain[2:]...)
	if style == "pixel" {
		d = append(d, plain...) // MPF secondary image / gain map
		return append(d, fakeMP4...)
	}
	return append(d, "MotionPhoto_Data"+fakeMP4+"SEFHSECRETSEFT"...)
}

func samePixels(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			r1, g1, b1, a1 := a.At(x, y).RGBA()
			r2, g2, b2, a2 := b.At(x, y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				return false
			}
		}
	}
	return true
}

var phoneLeaks = []string{"ftyp", "moov", "mdat", "MPF\x00", "SECRET", "GCamera", "MotionPhoto", "xap/1.0", "SEFH"}

func checkClean(t *testing.T, out []byte, orientation int) {
	t.Helper()
	if !bytes.HasSuffix(out, []byte{0xFF, 0xD9}) {
		t.Errorf("output does not end at EOI: % x", out[max(0, len(out)-4):])
	}
	if n := bytes.Count(out, []byte{0xFF, 0xD8}); n != 1 {
		t.Errorf("SOI count = %d, want 1", n)
	}
	for _, s := range phoneLeaks {
		if bytes.Contains(out, []byte(s)) {
			t.Errorf("output still contains %q", s)
		}
	}
	if exif := bytes.Contains(out, []byte("Exif\x00\x00")); exif != (orientation > 1) {
		t.Errorf("Exif present = %v with orientation %d", exif, orientation)
	}
}

func readPlain(t *testing.T, name string) []byte {
	t.Helper()
	d, err := os.ReadFile(filepath.Join("testdata", "phone", name+".plain.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPhoneMotionPhotos(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "phone", "*.plain.jpg"))
	if len(files) != 5 {
		t.Fatalf("found %d fixtures in testdata/phone, want 5", len(files))
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".plain.jpg")
		plain := readPlain(t, name)
		// Go's decoder rejects libjpeg's progressive+restart files even
		// unmodified; for those the byte comparison is the check.
		want, decErr := jpeg.Decode(bytes.NewReader(plain))
		plainScans := plain[bytes.Index(plain, []byte{0xFF, 0xDA}):]
		for _, style := range []string{"plain", "pixel", "samsung"} {
			in := plain
			if style != "plain" {
				in = phone(plain, style)
			}
			for _, o := range []int{1, 6} {
				t.Run(name+"/"+style, func(t *testing.T) {
					out, err := JPEG(in, o)
					if err != nil {
						t.Fatalf("JPEG: %v", err)
					}
					checkClean(t, out, o)
					// From the first scan on, the output is exactly the encoder's.
					if !bytes.Equal(out[bytes.Index(out, []byte{0xFF, 0xDA}):], plainScans) {
						t.Error("scan data differs from the plain image")
					}
					if decErr == nil {
						got, err := jpeg.Decode(bytes.NewReader(out))
						if err != nil || !samePixels(got, want) {
							t.Errorf("decode: %v, or pixels differ from the plain image", err)
						}
					}
					// Only the kept ICC block and the orientation block may be added.
					if len(out) > len(plain)+len(iccSeg)+64 {
						t.Errorf("size %d, plain image is %d (input %d)", len(out), len(plain), len(in))
					}
				})
			}
		}
	}
}

// A cut anywhere must not panic. Inside the scan it closes with EOI; inside
// the trailer the output equals the strip of the whole file.
func TestPhoneMotionPhotoTruncated(t *testing.T) {
	in := phone(readPlain(t, "rst"), "pixel")
	full, err := JPEG(in, 1)
	if err != nil {
		t.Fatal(err)
	}
	eoi := bytes.Index(in, []byte{0xFF, 0xD9}) // the first EOI ends the primary image
	sos := bytes.Index(in, []byte{0xFF, 0xDA})
	sosLen := int(binary.BigEndian.Uint16(in[sos+2:]))
	for cut := 2; cut < len(in); cut++ {
		out, err := JPEG(in[:cut], 1)
		switch {
		case cut > eoi+1:
			if err != nil || !bytes.Equal(out, full) {
				t.Fatalf("cut %d in trailer: err %v, equal %v", cut, err, bytes.Equal(out, full))
			}
		case cut >= sos+2+sosLen:
			if err != nil {
				t.Fatalf("cut %d in scan: %v", cut, err)
			}
			checkClean(t, out, 1)
		}
	}
}

// Random corruption before EOI must never panic or copy the trailer through.
func TestPhoneMotionPhotoCorrupted(t *testing.T) {
	in := phone(readPlain(t, "progressive_rst"), "samsung")
	eoi := bytes.Index(in, []byte{0xFF, 0xD9})
	seed := uint32(1)
	for n := 0; n < 5000; n++ {
		d := append([]byte(nil), in...)
		for range 4 {
			seed = seed*1664525 + 1013904223
			d[2+int(seed>>8)%(eoi-2)] = byte(seed >> 3)
		}
		out, err := JPEG(d, 1)
		if err != nil {
			continue
		}
		if !bytes.HasSuffix(out, []byte{0xFF, 0xD9}) || bytes.Contains(out, []byte("MotionPhoto_Data")) || bytes.Contains(out, []byte("ftyp")) {
			t.Fatalf("iteration %d: corrupted input leaked past EOI", n)
		}
	}
}
