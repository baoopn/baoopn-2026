// Package brand serves the site's brand assets (favicons, apple touch icon,
// wordmark, monograms) from the media bucket and lets the admin replace them.
//
// Files are content-addressed (brand/<stem>.<16 hex>.<ext>) and never go
// through the media table or the WebP queue. The defaults are embedded and
// uploaded at startup (Ensure); overrides and retired keys live in the
// settings document "brand". A retired file is deleted 7 days after it was
// replaced, so pages and caches that still name it keep working meanwhile.
package brand

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/logging"
	"github.com/baoopn/baoopn-2026/internal/media"
	"github.com/baoopn/baoopn-2026/internal/settings"
)

// Key is the settings key of the State document.
const Key = "brand"

// retireAfter is how long a replaced file stays in storage.
const retireAfter = 7 * 24 * time.Hour

// Files stores brand files. *media.Store implements it.
type Files interface {
	PutBrandFile(ctx context.Context, key, ctype string, data []byte) error
	BrandFileExists(ctx context.Context, key string) (bool, error)
	RemoveBrandFile(ctx context.Context, key string) error
	URL(key string) string
}

// Custom is an uploaded override of one slot.
type Custom struct {
	Key         string    `json:"key"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	Original    string    `json:"original"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Retired is a replaced file waiting for deletion.
type Retired struct {
	Key       string    `json:"key"`
	RetiredAt time.Time `json:"retired_at"`
}

// State is the settings document: only overrides and retired keys.
type State struct {
	Custom  map[string]Custom `json:"custom"`
	Retired []Retired         `json:"retired"`
}

func (s State) clone() State {
	c := State{Custom: maps.Clone(s.Custom), Retired: append([]Retired(nil), s.Retired...)}
	if c.Custom == nil {
		c.Custom = map[string]Custom{}
	}
	return c
}

// Deps are the store's dependencies.
type Deps struct {
	DB    *pgxpool.Pool
	Log   *slog.Logger
	Files Files
	// Now is the clock (tests); nil means time.Now.
	Now func() time.Time
}

// defaultAsset is an embedded default file.
type defaultAsset struct {
	key  string
	data []byte
	info Info
}

var (
	// ErrUnknownSlot is returned for a slot name that does not exist.
	ErrUnknownSlot = errors.New("brand: unknown slot")
	// ErrStorage wraps a failed write to the file storage.
	ErrStorage = errors.New("brand: storage unavailable")
)

// Store reads and writes the brand state, cached in memory (one API instance).
type Store struct {
	log      *slog.Logger
	files    Files
	now      func() time.Time
	defaults map[string]defaultAsset

	// mu serializes writers (Replace, Reset, Ensure, prune). Readers never
	// take it: they read snap, an immutable copy published after each
	// successful database write (copy-on-write).
	mu   sync.Mutex
	snap atomic.Pointer[State]
	load func(ctx context.Context) (State, error)
	save func(ctx context.Context, st State) error
}

// New returns the store.
func New(d Deps) *Store {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &Store{log: d.Log, files: d.Files, now: d.Now, defaults: map[string]defaultAsset{}}
	for _, def := range slots {
		data, err := defaultFile(def)
		if err != nil {
			panic(fmt.Sprintf("brand: embedded default %s: %v", def.File, err)) // build error
		}
		info, err := validate(def, data)
		if err != nil {
			d.Log.Error("brand: embedded default fails validation", slog.String("slot", def.Slot), logging.Err(err))
		}
		s.defaults[def.Slot] = defaultAsset{key: keyFor(def, data), data: data, info: info}
	}
	s.load = func(ctx context.Context) (State, error) {
		var st State
		_, err := settings.Get(ctx, d.DB, Key, &st)
		return st, err
	}
	s.save = func(ctx context.Context, st State) error { return settings.Put(ctx, d.DB, Key, st) }
	return s
}

// loadState reads the document from the database, dropping anything a
// hand edit broke.
func (s *Store) loadState(ctx context.Context) (State, error) {
	st, err := s.load(ctx)
	if err != nil {
		return State{}, err
	}
	st = st.clone()
	for slot, c := range st.Custom {
		if def, ok := slotByName(slot); !ok || !media.BrandKeyRe.MatchString(c.Key) || c.ContentType != def.Type {
			delete(st.Custom, slot)
		}
	}
	return st, nil
}

// stateLocked returns a mutable copy of the state; s.mu must be held.
func (s *Store) stateLocked(ctx context.Context) (State, error) {
	if p := s.snap.Load(); p != nil {
		return p.clone(), nil
	}
	st, err := s.loadState(ctx)
	if err != nil {
		return State{}, err
	}
	s.snap.Store(&st)
	return st.clone(), nil
}

// putLocked saves st and publishes it; s.mu must be held. A failed save
// (one transaction) leaves the database and the snapshot as they were.
// If the commit succeeded but its ack was lost, the snapshot is stale until
// the next successful write or a restart reloads it from the database.
func (s *Store) putLocked(ctx context.Context, st State) error {
	if err := s.save(ctx, st); err != nil {
		return err
	}
	pub := st.clone()
	s.snap.Store(&pub)
	return nil
}

// state returns the current state without waiting for writers; on a
// database error the defaults (an empty state) and the error. The result
// must not be modified.
func (s *Store) state(ctx context.Context) (State, error) {
	if p := s.snap.Load(); p != nil {
		return *p, nil
	}
	st, err := s.loadState(ctx)
	if err != nil {
		return State{Custom: map[string]Custom{}}, err
	}
	// Only fill an empty snapshot: a writer's newer one always wins, and a
	// writer publishes only states it saved.
	s.snap.CompareAndSwap(nil, &st)
	return *s.snap.Load(), nil
}

// protected reports whether key is a slot's current file or a default.
func (s *Store) protected(st State, key string) bool {
	for _, def := range slots {
		if s.defaults[def.Slot].key == key {
			return true
		}
		if c, ok := st.Custom[def.Slot]; ok && c.Key == key {
			return true
		}
	}
	return false
}

// retire moves slot's override (if any) to the retired list.
func (s *Store) retire(st *State, slot string, now time.Time) {
	if c, ok := st.Custom[slot]; ok {
		st.Retired = append(st.Retired, Retired{Key: c.Key, RetiredAt: now.UTC()})
		delete(st.Custom, slot)
	}
}

// Replace validates data and makes it slot's current file. Bytes identical
// to the current file change nothing.
func (s *Store) Replace(ctx context.Context, slot, original string, data []byte) (AdminAsset, error) {
	def, ok := slotByName(slot)
	if !ok {
		return AdminAsset{}, ErrUnknownSlot
	}
	info, err := validate(def, data)
	if err != nil {
		return AdminAsset{}, err
	}
	key := keyFor(def, data)
	if key == s.defaults[slot].key {
		return s.Reset(ctx, slot) // the default's own bytes: back to the default
	}

	s.mu.Lock()
	st, err := s.stateLocked(ctx)
	if err != nil {
		s.mu.Unlock()
		return AdminAsset{}, err
	}
	if s.currentKey(st, def) == key {
		s.mu.Unlock()
		return s.admin(st, def), nil
	}
	if err := s.files.PutBrandFile(ctx, key, def.Type, data); err != nil {
		s.mu.Unlock()
		return AdminAsset{}, fmt.Errorf("%w: %w", ErrStorage, err)
	}
	now := s.now()
	next := st.clone()
	s.retire(&next, slot, now)
	next.Custom[slot] = Custom{Key: key, ContentType: def.Type, SizeBytes: int64(len(data)),
		Width: info.Width, Height: info.Height, Original: cleanOriginal(original), UpdatedAt: now.UTC().Truncate(time.Second)}
	next.Retired = dropKey(next.Retired, key) // uploaded again: no longer retired
	if err := s.putLocked(ctx, next); err != nil {
		if !s.protected(st, key) && !hasKey(st.Retired, key) {
			if rerr := s.files.RemoveBrandFile(context.WithoutCancel(ctx), key); rerr != nil {
				s.log.Warn("brand: remove unsaved upload", slog.String("key", key), logging.Err(rerr))
			}
		}
		s.mu.Unlock()
		return AdminAsset{}, err
	}
	out := s.admin(next, def)
	s.mu.Unlock()
	s.prune(ctx)
	return out, nil
}

// Reset puts slot back to its default. It is idempotent.
func (s *Store) Reset(ctx context.Context, slot string) (AdminAsset, error) {
	def, ok := slotByName(slot)
	if !ok {
		return AdminAsset{}, ErrUnknownSlot
	}
	s.mu.Lock()
	st, err := s.stateLocked(ctx)
	if err != nil {
		s.mu.Unlock()
		return AdminAsset{}, err
	}
	if _, custom := st.Custom[slot]; !custom {
		s.mu.Unlock()
		return s.admin(st, def), nil
	}
	next := st.clone()
	s.retire(&next, slot, s.now())
	if err := s.putLocked(ctx, next); err != nil {
		s.mu.Unlock()
		return AdminAsset{}, err
	}
	out := s.admin(next, def)
	s.mu.Unlock()
	s.prune(ctx)
	return out, nil
}

// Ensure uploads missing default files, then drops overrides whose file is
// gone (moving their keys to retired), then prunes. Overrides are only
// dropped when every default was found or uploaded in this run: that proves
// the storage is reachable and is the right one, so a "not found" from a
// wrong or broken bucket cannot wipe them. Errors are logged; the caller
// never fails on them.
func (s *Store) Ensure(ctx context.Context) {
	storageOK := true
	for _, def := range slots {
		d := s.defaults[def.Slot]
		ok, err := s.files.BrandFileExists(ctx, d.key)
		if err != nil {
			s.log.Warn("brand: check default", slog.String("key", d.key), logging.Err(err))
			storageOK = false
			continue
		}
		if !ok {
			if err := s.files.PutBrandFile(ctx, d.key, def.Type, d.data); err != nil {
				s.log.Warn("brand: upload default", slog.String("key", d.key), logging.Err(err))
				storageOK = false
			} else {
				s.log.Info("brand: uploaded default", slog.String("key", d.key))
			}
		}
	}
	if !storageOK {
		s.log.Warn("brand: storage not confirmed; overrides left as they are")
	}
	s.mu.Lock()
	st, err := s.stateLocked(ctx)
	if err != nil {
		s.mu.Unlock()
		s.log.Warn("brand: read state", logging.Err(err))
		return
	}
	next, changed := st.clone(), false
	for _, def := range slots {
		c, ok := st.Custom[def.Slot]
		if !ok || !storageOK {
			continue
		}
		exists, err := s.files.BrandFileExists(ctx, c.Key)
		if err != nil {
			s.log.Warn("brand: check custom file", slog.String("key", c.Key), logging.Err(err))
			continue
		}
		if !exists {
			s.log.Warn("brand: custom file missing, using the default", slog.String("slot", def.Slot),
				slog.String("key", c.Key))
			s.retire(&next, def.Slot, s.now())
			changed = true
		}
	}
	if changed {
		if err := s.putLocked(ctx, next); err != nil {
			s.log.Warn("brand: save state", logging.Err(err))
		}
	}
	s.mu.Unlock()
	s.prune(ctx)
}

// prune deletes retired files older than retireAfter. It never deletes a
// current or default file; only a delete that worked (or found nothing)
// drops the entry.
func (s *Store) prune(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.stateLocked(ctx)
	if err != nil {
		s.log.Warn("brand: prune: read state", logging.Err(err))
		return
	}
	now := s.now()
	var keep []Retired
	changed := false
	for _, r := range st.Retired {
		switch {
		case s.protected(st, r.Key) || !media.BrandKeyRe.MatchString(r.Key):
			changed = true // in use again (or junk): forget it, never delete
		case now.Sub(r.RetiredAt) < retireAfter:
			keep = append(keep, r)
		default:
			if err := s.files.RemoveBrandFile(ctx, r.Key); err != nil {
				s.log.Warn("brand: delete retired file", slog.String("key", r.Key), logging.Err(err))
				keep = append(keep, r)
				continue
			}
			s.log.Info("brand: deleted retired file", slog.String("key", r.Key))
			changed = true
		}
	}
	if !changed {
		return
	}
	next := st.clone()
	next.Retired = keep
	if err := s.putLocked(ctx, next); err != nil {
		s.log.Warn("brand: prune: save state", logging.Err(err))
	}
}

func (s *Store) currentKey(st State, def slotDef) string {
	if c, ok := st.Custom[def.Slot]; ok {
		return c.Key
	}
	return s.defaults[def.Slot].key
}

func hasKey(rs []Retired, key string) bool {
	for _, r := range rs {
		if r.Key == key {
			return true
		}
	}
	return false
}

func dropKey(rs []Retired, key string) []Retired {
	out := rs[:0:0]
	for _, r := range rs {
		if r.Key != key {
			out = append(out, r)
		}
	}
	return out
}
