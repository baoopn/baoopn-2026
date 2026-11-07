The API is a Go service built on chi, a small router that sits on top of `net/http`. This part covers how the routes are laid out, how the data is stored, how search works across two languages, how Redis caching stays correct, and how OpenAPI keeps the three apps in step.

## Contract first

The HTTP contract is one file: `apps/api/openapi.yaml` (OpenAPI 3.1). Every change starts there. Then I run one command:

```bash
pnpm gen:api   # openapi-typescript ../../apps/api/openapi.yaml -o src/schema.d.ts
```

That writes TypeScript types into `packages/api-types`. Both SvelteKit apps import from it and call the API through `openapi-fetch`, so a path, a query param or a field name that does not exist is a type error.

The Go side is written by hand against the same file. To stop the two from drifting, CI regenerates the types and fails if anything changed:

```yaml
- name: API types are in sync with openapi.yaml
  run: pnpm gen:api && git diff --exit-code packages/api-types
```

A few rules hold across the whole contract. Success responses are `{ "data": ..., "meta": {...} }`. Errors are RFC 9457 problem details (`application/problem+json`) with a request id. Money is integer cents. Text that shoppers read is `{en, vi}`, and `en` is required.

## Routing with chi

The router is one function in `internal/server/server.go`. Middleware runs in a fixed order: request id, real client IP, access log, panic recovery, security headers, CORS, a timeout, then a global rate limit.

Public routes and admin routes split cleanly. Here is a trimmed view:

```go
r.Route("/v1", func(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(httpx.PublicCache(60 * time.Second))
		catalogCache := func(params ...string) func(http.Handler) http.Handler {
			return d.Cache.Middleware(redisx.NSCatalog, 5*time.Minute, params...)
		}
		r.With(catalogCache()).Get("/categories", w(c.HandlePublicCategories))
		r.With(catalogCache("q", "featured", "inStock", "minPrice", "maxPrice",
			"sort", "locale", "page", "pageSize", "category")).Get("/products", w(c.HandlePublicProducts))
		r.With(catalogCache()).Get("/products/{slug}", w(c.HandlePublicProduct))
	})
	r.With(limit("views", 30, time.Minute)).Post("/products/{id}/views", w(c.HandleRecordView))

	r.Route("/admin", func(r chi.Router) {
		// Limited before authentication, so floods of bad tokens are throttled too.
		r.Use(httpx.NoStore, limit("admin", 600, time.Minute), d.Auth.Handler)
		can := auth.Require
		r.With(can(auth.ProductsWrite)).Put("/products/{id}", w(c.HandleUpdateProduct))
		r.With(can(auth.ProductsDelete)).Delete("/products/{id}", w(c.HandleDeleteProduct))
		// ...
	})
})
```

I like that the security rules are visible in the routing table. You can read one file and see which permission guards which route, and which limit applies.

Handlers return an `error` instead of writing errors themselves. `httpx.Wrap` turns that error into a problem response. Internal errors are logged with the request id and never sent to the client.

### Strict JSON

Every admin body goes through `httpx.DecodeJSON`. It caps the body at 1 MiB, rejects unknown fields, rejects a second JSON value after the first, and runs struct validation:

```go
dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
dec.DisallowUnknownFields()
if err := dec.Decode(dst); err != nil {
	return decodeError(err)
}
if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
	return BadRequest("request body must contain a single JSON object")
}
return Validate(dst)
```

`DisallowUnknownFields` is the direct fix for the old mass-assignment bug. If the DTO has no `role` field, a body with `role` is a 400, not a silent write.

## The data model

MongoDB has one database with a collection per concept: `products`, `categories`, `orders`, `store_info`, `audit_log`, `image_jobs`, and a few for analytics and job leases.

A product looks like this in Go (trimmed):

```go
type LocalizedText struct {
	EN string `bson:"en" json:"en"`
	VI string `bson:"vi,omitempty" json:"vi,omitempty"`
}

type Product struct {
	ID              bson.ObjectID `bson:"_id"`
	Slug            string        `bson:"slug"`
	Status          string        `bson:"status"` // draft, published, archived
	Name            LocalizedText `bson:"name"`
	Description     LocalizedText `bson:"description"`
	PriceCents      *int64        `bson:"priceCents"`
	DiscountPercent int           `bson:"discountPercent"`
	FinalPriceCents *int64        `bson:"finalPriceCents"`
	Stock           int           `bson:"stock"`
	Images          []Image       `bson:"images"`
	// ...
}
```

Some choices worth calling out:

- **Prices are cents, and can be null.** The old system stored dollars as floats and used `price <= 0` to mean "contact for price". Now `priceCents: null` says that on purpose.
- **The final price is stored.** The server computes `finalPriceCents` from the price and discount and saves it, so the catalogue can filter and sort by the price people see.
- **Products have a `version`.** It goes up on every edit and every stock change. An admin save based on old data gets a 409 instead of overwriting live stock.
- **Images are an ordered list, max 10, first one is the cover.** Each has smaller copies for responsive loading.

Indexes are declared in code and created at startup. Every product sort has an index that ends in the sort keys plus `_id` as a tie-breaker, so a page of results reads its rows straight from the index.

## Search in two languages

The old search was an unescaped regex over a few fields. The new one has to handle Vietnamese, where "Gốm Sứ" and "gom su" should match.

Each product stores `searchWords`: the distinct, accent-folded words of its English and Vietnamese name and description. The folding lives in `internal/platform/textx`:

```go
// Fold lowercases s and removes diacritics, so "Gốm Sứ" becomes "gom su".
// Vietnamese đ/Đ is not a combining sequence and is mapped explicitly.
func Fold(s string) string {
	s = strings.NewReplacer("đ", "d", "Đ", "D").Replace(s)
	out, _, err := transform.String(transform.Chain(norm.NFD, stripMarks, norm.NFC), s)
	if err != nil {
		out = s
	}
	return strings.ToLower(strings.Join(strings.Fields(out), " "))
}
```

`NFD` splits each letter from its accent marks, `stripMarks` drops the marks, and `NFC` puts the rest back together. The letter đ needs its own rule because it is a separate letter, not d plus a mark.

A query is folded and split the same way, and each term must be the start of some word:

```go
func regexpPrefix(term string) bson.Regex {
	return bson.Regex{Pattern: "^" + regexp.QuoteMeta(term)}
}
```

An anchored, case-sensitive prefix regex is a range scan on a multikey index, so search stays fast as the catalogue grows. The trade-off is that mid-word matches are not found: "ote" will not match "tote". For a shop catalogue, I think prefix matching is the right call.

## Redis caching that stays correct

Public catalogue reads are cached in Redis for five minutes. The hard part of any cache is invalidation, and I wanted it to be simple and safe.

Each cache namespace has a version number. The cache key includes it. Any write to the catalogue (a product edit, an image upload, a stock change from an order) bumps the version:

```go
// A write bumps its namespace version, which instantly orphans every cached
// entry of that namespace (they then expire by TTL), so invalidation never
// needs KEYS/SCAN.
func (c *Cache) Invalidate(ctx context.Context, namespaces ...string) {
	pipe := c.rdb.Pipeline()
	for _, ns := range namespaces {
		pipe.Incr(ctx, c.versionKey(ns))
	}
	// ...
}
```

Old entries are never read again and age out on their own. There is no need to find and delete keys.

Two more details make the cache hard to abuse. First, the key uses only the query params that an endpoint reads. A request with any other param skips the cache (`X-Cache: BYPASS`), so random junk params cannot fill Redis. Second, misses go through `singleflight`. If fifty requests miss the same key at once, the handler runs once and the rest share the result (`X-Cache: SHARED`). That matters right after an invalidation.

Every Redis use fails open. If Redis is down, the cache steps aside and the rate limiter falls back to in-memory counters. The site gets slower, but it stays up.

## Rate limits

Rate limits use `httprate` with a Redis counter, so all API replicas share them. They are keyed on the real client IP, which the API resolves only through a list of trusted proxies. Each sensitive route has its own budget: 30 view pings a minute, 5 orders per 10 minutes, 10 order lookups per 15 minutes, 120 uploads a minute.

Next, part 3 looks at who can call the admin routes and how photos get from a phone to the CDN.
