package brand

import (
	"context"

	"github.com/go-chi/chi/v5"
)

// Module holds the store of each brand and mounts their routes.
type Module struct {
	Main      *Store // baoopn: /brand
	Listening *Store // listening sub-brand: /brand/listening
}

// NewModule returns the stores of both brands.
func NewModule(d Deps) *Module {
	return &Module{Main: NewBrand(d, Baoopn), Listening: NewBrand(d, Listening)}
}

// Ensure uploads both brands' defaults (baoopn first). It only logs.
func (m *Module) Ensure(ctx context.Context) {
	m.Main.Ensure(ctx)
	m.Listening.Ensure(ctx)
}

// Public mounts GET and OPTIONS (CORS preflight) on /brand, /brand/{slot},
// /brand/listening and /brand/listening/{slot}. Every response carries
// Vary: Origin, and a listed origin (Deps.CORSOrigins) gets CORS headers.
// chi matches the static "listening" segment before {slot}, and no baoopn
// slot is named "listening".
func (m *Module) Public(r chi.Router) {
	m.Main.mountPublic(r)
	m.Listening.mountPublic(r)
}

// Admin mounts the list, PUT and DELETE routes of both brands.
func (m *Module) Admin(r chi.Router) {
	m.Main.mountAdmin(r)
	m.Listening.mountAdmin(r)
}

// Hooks mounts nothing.
func (m *Module) Hooks(chi.Router) {}
