The admin dashboard is where staff run the store. It is the second SvelteKit app, with Svelte 5, Tailwind 4, Paraglide (the dashboard is bilingual too), `svelte-clerk` for sign-in and TanStack Query for data. This part covers how it works and how I moved the data over from the old system.

## A client app behind a server guard

Every dashboard page renders in the browser (`ssr = false`). The SvelteKit server does two small jobs: it runs Clerk's handler, and it guards every route except sign-in. A visitor with no session gets a 303 to `/sign-in?redirect_url=<path>`.

That redirect target is a classic open-redirect hole, so it goes through a validator in both the hook and the sign-in page. It must be a same-origin path with a single leading `/`, and no `//`, backslashes, schemes or control characters.

The old admin only protected its pages in the browser. Now there is a server guard in the SvelteKit hook, and every API route checks auth on its own as well. Either layer alone would stop an outsider.

Once signed in, the layout calls `GET /v1/admin/me`. A 403 shows a "No access, ask an administrator to invite you" page. The returned permission list drives a `session.can(permission)` helper, which hides menu items and buttons a user cannot use. That is only for comfort. The API decides.

The pages are: overview, products, categories, orders, analytics, store info, staff and the audit log.

## TanStack Query for every read

I used TanStack Query for all admin data. The main reason is that staff work in more than one tab, leave tabs open for hours, and edit the same products as their coworkers. I wanted data that refreshes on its own and a clear rule for what to refetch after a write.

Every query key comes from one factory. The keys form a tree, so I can invalidate as wide or as narrow as I need:

```ts
export const keys = {
	products: {
		all: ['products'] as const,
		lists: () => ['products', 'list'] as const,
		list: (params: ProductListParams = {}) =>
			['products', 'list', normalizeParams(params)] as const,
		// Under "list" so everything that refreshes product lists refreshes the counts too.
		stats: (params: ProductStatsParams = {}) =>
			['products', 'list', 'stats', normalizeParams(params)] as const,
		detail: (id: string) => ['products', 'detail', id] as const
	},
	// orders, staff, uploads, analytics, audit ...
};
```

The param types come straight from the generated OpenAPI types, so a filter the API does not accept is a type error. `normalizeParams` drops empty values, so `{ q: '' }` and `{}` share one cache entry.

Cached data shows at once and is checked again on mount, focus and reconnect. Orders poll every 30 seconds and the analytics report every 60 seconds, but only while the tab is visible.

### Keeping tabs in sync

Two small additions in the shared query client made a big difference. Every successful mutation marks the audit log as stale, since every write is audited. And every mutation is broadcast to the user's other tabs through a `BroadcastChannel`:

```ts
mutationCache: new MutationCache({
	onSuccess: () => {
		void client.invalidateQueries({ queryKey: keys.audit.all });
		// Tell the admin's other open tabs that data changed.
		sync?.postMessage(SYNC_MESSAGE);
	}
})
```

The other tabs refetch their active queries. A tab restored from the browser's back/forward cache also refetches, since it may hold old data. All cached data is cleared on sign-out.

## Edits that do not overwrite each other

In part 2 I mentioned that each product has a `version`. The product form uses it. When a save succeeds, the API returns the product with its new version, and the form stores it in the cache. In-flight fetches of the same product are cancelled first, so an older response cannot land on top of it.

The tricky case is a product that changes while someone is editing it. A coworker saves, or an order takes stock. The form handles that with one effect:

```ts
$effect(() => {
	const next = product;
	const isDirty = dirty;
	if (!next || next.version === baseVersion) return;
	untrack(() => {
		if (JSON.stringify(toValues(next)) === snapshot) {
			// Only non-form fields changed (e.g. images): adopt the new version.
			baseVersion = next.version;
		} else if (!isDirty) {
			// Changed elsewhere and nothing to lose: show the latest values.
			resetTo(next);
		}
		// Otherwise keep the old version so saving surfaces a 409 instead of overwriting.
	});
});
```

If the user has unsaved edits, the form keeps the old version on purpose. Saving then gets a 409, and the form offers to discard the local edits and reload. Nobody overwrites stock that changed under them without being told.

The form also shows a live preview of the final price. It mirrors the API's rounding (half-up), so what staff see is what shoppers will see.

## Uploads in the dashboard

Image uploads use Uppy, three files at a time. Each file is one request. On a 429 or 5xx the client retries after the server's `Retry-After`. While the worker processes a file, the dashboard polls the job every second and shows the stage and percent. Thumbnails in the dashboard use the smallest image copy that fits.

## Errors as plain text

API errors are RFC 9457 problem details. The admin parses them into an `ApiError` with a status, a title, a detail and a list of field errors. Field errors land on the matching form field. The detail (for example "cannot demote the last admin") shows in a toast as plain text, never as HTML.

## Moving the data over

The old system held real products, photos and store info. I wrote a one-off Go command, `cmd/migrate`, to bring them across. It uses the same catalogue service as the API, so every imported record passes the same rules as an edit made in the dashboard.

It follows three rules.

**It only reads the old system.** It reads the old database and downloads images from the old public CDN. It never writes to either.

**A plain run writes nothing.** Without `-apply`, it reads everything, checks every image and prints a report.

**It is safe to run again.** Categories and products keep their old ids, so a second run cannot duplicate them. By default, records that already exist are kept as they are, because after go-live they hold admin edits, new images and stock changes. An `-overwrite` flag replaces them, for use before go-live only.

```bash
go run ./cmd/migrate                     # dry run
go run ./cmd/migrate -apply              # import; existing records are kept
go run ./cmd/migrate -apply -overwrite   # before go-live only
```

### Mapping old fields

Most of the work was in the mapping. Some examples:

| Old | New |
|---|---|
| `name` / `nameVI` | `name.en` / `name.vi` |
| `price` (float dollars, `<= 0` meant "contact") | `priceCents` (int) or `null` |
| `discount` | `discountPercent`, clamped to 0 to 90 |
| `stock` | `stock`, clamped to 0 or more |
| `thumbnail` + `images[]` | one ordered `images[]`, cover first |
| `embedding`, `normalized*` | dropped; search fields recomputed |

The price rule in code:

```go
func dollarsToCents(v float64) *int64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	c := int64(math.Round(v * 100))
	return &c
}
```

Every old image is downloaded, run through the same processing as a new upload (decode, fix orientation, re-encode to WebP) and stored under a new random key. A `migration_media` collection remembers which old images are done, so a re-run does not upload them again.

Some things I chose not to move. Staff accounts stay behind, because passwords cannot move to Clerk safely; I invited each person instead. Refresh tokens and API keys stay behind. Raw page views stay behind too, because they held IP addresses. The total view count on each product does carry over.

Next, the last part covers deploys, CI and what I learned.
