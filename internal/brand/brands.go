package brand

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/baoopn/baoopn-2026/internal/media"
)

// Brand names.
const (
	Baoopn    = "baoopn"
	Listening = "listening"
)

// ListeningKey is the settings key of the listening brand's State document.
const ListeningKey = "brand_listening"

// brandDef is one brand: its settings document, key prefix, embedded
// defaults folder, route and slot table.
type brandDef struct {
	Name        string
	SettingsKey string
	KeyPrefix   string // brand/ or brand/listening/
	DefaultsDir string // folder in defaultsFS
	Route       string // mount path under /api/v1 and /api/admin
	Slots       []slotDef
}

var (
	baoopnBrand    = &brandDef{Baoopn, Key, "brand/", "defaults", "/brand", slots}
	listeningBrand = &brandDef{Listening, ListeningKey, "brand/listening/", "defaults/listening", "/brand/listening", listeningSlots}
	brands         = []*brandDef{baoopnBrand, listeningBrand}
)

func brandByName(name string) (*brandDef, bool) {
	for _, b := range brands {
		if b.Name == name {
			return b, true
		}
	}
	return nil, false
}

// slot returns the slot named name.
func (b *brandDef) slot(name string) (slotDef, bool) {
	for _, d := range b.Slots {
		if d.Slot == name {
			return d, true
		}
	}
	return slotDef{}, false
}

// keyFor is the content-addressed key of data in slot d.
func (b *brandDef) keyFor(d slotDef, data []byte) string {
	sum := sha256.Sum256(data)
	return b.KeyPrefix + d.Stem + "." + hex.EncodeToString(sum[:])[:16] + "." + d.Ext
}

// owns reports whether key is a valid brand file key of this brand (and
// not of another brand in a subfolder).
func (b *brandDef) owns(key string) bool {
	rest, ok := strings.CutPrefix(key, b.KeyPrefix)
	return ok && media.BrandKeyRe.MatchString(key) && !strings.Contains(rest, "/")
}

// defaultFile returns the embedded default bytes of slot d.
func (b *brandDef) defaultFile(d slotDef) ([]byte, error) {
	return defaultsFS.ReadFile(b.DefaultsDir + "/" + d.File)
}

// check returns the first broken invariant of the brand table, or nil.
func (b *brandDef) check() error {
	names, files := map[string]bool{}, map[string]bool{}
	group := 0
	for _, d := range b.Slots {
		if b == baoopnBrand && d.Slot == Listening {
			return fmt.Errorf("baoopn slot %q clashes with the listening route", d.Slot)
		}
		if names[d.Slot] {
			return fmt.Errorf("duplicate slot %q", d.Slot)
		}
		names[d.Slot] = true
		if files[d.Stem+"."+d.Ext] {
			return fmt.Errorf("duplicate stem %s.%s", d.Stem, d.Ext)
		}
		files[d.Stem+"."+d.Ext] = true
		g := slices.Index(groupOrder, d.Group)
		if g < 0 {
			return fmt.Errorf("slot %q: unknown group %q", d.Slot, d.Group)
		}
		if g < group {
			return fmt.Errorf("slot %q: group %q out of order", d.Slot, d.Group)
		}
		group = g
		if !media.BrandKeyRe.MatchString(b.KeyPrefix + d.Stem + ".0123456789abcdef." + d.Ext) {
			return fmt.Errorf("slot %q: stem %q makes an invalid key", d.Slot, d.Stem)
		}
	}
	return nil
}

func init() {
	for _, b := range brands {
		if err := b.check(); err != nil {
			panic("brand: " + b.Name + ": " + err.Error())
		}
	}
}
