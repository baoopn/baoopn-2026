The storefront is what shoppers see. It is a SvelteKit 2 app with Svelte 5 runes, Tailwind 4 and Paraglide for translations. It has no cart, checkout or login. Its job is to show the catalogue well, in two languages, and to be easy for search engines to read.

## Server rendering

Every page renders on the server. The `load` functions call the Go API over the internal network, so the HTML that leaves the server already holds the product names, prices and links. That gives real titles, descriptions and structured data for search engines, with no client-side fetch waterfall.

All API calls go through one typed client in `src/lib/server/api.ts`, built with `openapi-fetch` and the generated types from part 2. Two details there matter more than they look.

First, each call has a five-second timeout. A slow API should give a clean error page, not a hung request.

Second, the client forwards the visitor's IP. Without it, every server-rendered request would look like it came from the storefront container, and all shoppers would share one rate-limit bucket:

```ts
fetch: (request: Request) => {
	// Forward the visitor's address so the API rate-limits per visitor rather than
	// treating every SSR request as coming from this container. The API only trusts
	// this header from addresses listed in its TRUSTED_PROXIES.
	const clientAddress = requestContext()?.clientAddress;
	if (clientAddress) request.headers.set('x-forwarded-for', clientAddress);
	return fetch(request, { signal: AbortSignal.timeout(TIMEOUT_MS) });
},
```

Errors are mapped in one place. An API 404 becomes a SvelteKit 404. Anything else (network error, timeout, 5xx) becomes a generic 503. Upstream details are logged on the server and never shown to the shopper.

## Two languages with Paraglide

Paraglide compiles translation files into small, typed functions. All UI copy lives in `messages/en.json` and `messages/vi.json` as flat keys.

Every page lives under a locale prefix: `/en/products/...` and `/vi/products/...`. The route files in `src/routes` have no prefix. A `reroute` hook strips it before SvelteKit matches the route:

```ts
import type { Reroute } from '@sveltejs/kit';
import { deLocalizeUrl } from '$lib/paraglide/runtime';

export const reroute: Reroute = (request) => deLocalizeUrl(request.url).pathname;
```

So one route file serves both languages, and Paraglide's middleware sets the locale for the request.

A URL with no prefix, including `/`, gets a 307 redirect. The target locale comes from the cookie that the language switcher sets, then the browser's `Accept-Language`, then English. I wrote this redirect myself instead of using Paraglide's built-in one, because I wanted it to apply to every GET, not only to page loads. Crawlers and `curl` get a proper redirect too.

```ts
const decision = await shouldRedirect({ request: event.request });
if (decision.shouldRedirect && decision.redirectUrl) {
	// Canonical URLs have no trailing slash (`/en`, not `/en/`).
	const target = decision.redirectUrl.pathname.replace(/(.)\/+$/, '$1');
	return new Response(null, {
		status: 307,
		headers: { Location: target + search, Vary: 'Accept-Language, Cookie', 'Cache-Control': 'private, no-store' }
	});
}
```

Content from the API is already bilingual (`{en, vi}`), so the page picks the right field. Prices use `Intl.NumberFormat` with the locale and CAD.

![The product list in Vietnamese](screenshot:storefront-products-vi)

### SEO for two languages

Each page links its other language with `hreflang` tags, plus an `x-default` that points to English. Pages also emit schema.org JSON-LD: the shop as a `Store` with its hours and contact details, and each product with its price. `/sitemap.xml` and `/robots.txt` are the only paths without a locale prefix.

## Search and listing pages

The search page reads its state from the URL: query, category, sort, price range and page. Every value is checked and clamped before it reaches the API:

```ts
const state: SearchState = {
	q: parseQuery(params.get('q')),
	category: isSlug(rawCategory) ? rawCategory : '',
	sort: parseSort(params.get('sort')),
	price: parsePriceRange(params.get('price')),
	page: parsePage(params.get('page'))
};

if (!pageWithinListingLimit(state.page, PAGE_SIZE)) error(404, 'Not found');
```

Queries are capped at 100 characters. Sort and price range must be one of a known list. Pages past 10,000 rows return a 404, which matches the API's own paging limit. Since the state lives in the URL, a search result can be shared or bookmarked, and the back button works as expected.

Because the API folds accents, a shopper can type "gom" and find products named "Gốm". That works the same from the English and Vietnamese pages.

![Search results for a Vietnamese query typed without accents](screenshot:storefront-search-vi)

## Images

Each product image comes with smaller copies at 400, 800 and 1600 px, made by the image worker. The storefront turns them into a `srcset`, so a phone downloads a small file and a wide screen gets a big one:

```ts
export function srcset(img: ResponsiveImage): string | undefined {
	const copies = img.variants ?? [];
	if (copies.length === 0 || !img.width) return undefined;
	return [...copies.map((v) => `${v.url} ${v.width}w`), `${img.url} ${img.width}w`].join(', ');
}
```

The product grid tells the browser how wide each card is at each breakpoint (`25vw`, `33vw` or `50vw`), and the browser picks the file. Product pages have a gallery and a lightbox that use the same helper.

![A product page with its image gallery](screenshot:storefront-product-en)

## Caching on the storefront

I thought about putting a CDN cache in front of the HTML. I decided against it. Every HTML response carries its own CSP nonce (a random value that lets SvelteKit's inline startup script run). A shared cache would serve one visitor's nonce to another, which breaks the point of it. So pages are `private, max-age=60, stale-while-revalidate=600`, and the speed comes from the API's Redis cache instead.

Categories are the one thing the storefront keeps in memory itself. They change rarely and are the same for everyone, so they are held for a minute. A failed fetch is not cached, so the next request tries again.

## A strict CSP

The storefront sets a Content Security Policy with no `unsafe-inline` for scripts or styles. That meant giving up Svelte transitions and inline `style` attributes, which both need inline styles. I think the trade was worth it for a public site.

Origins for the API and the image CDN are baked into the CSP at build time, because SvelteKit's CSP config is static. The deploy workflow checks that those values are set before it builds, so a broken policy cannot ship by accident.

Product descriptions are plain text, rendered with `whitespace-pre-line` to keep line breaks. `{@html}` is banned by an ESLint rule, so API data can never be injected as markup.

## Analytics without cookies

The storefront sends a small page-view beacon after each navigation. It sends only the path and, on the first page of a visit, the referrer. It stores nothing in the browser, and it sends nothing at all if the browser has Do Not Track or Global Privacy Control turned on:

```ts
export function optedOut(nav: Navigator = navigator): boolean {
	const n = nav as Navigator & { globalPrivacyControl?: boolean };
	return n.doNotTrack === '1' || n.globalPrivacyControl === true;
}
```

The API counts unique visitors with a keyed, day-scoped hash in a Redis HyperLogLog (a structure that estimates how many distinct items it has seen). Only the daily count reaches MongoDB. No IPs, user agents or cookies are stored.

Next, part 5 covers the other SvelteKit app: the staff dashboard, and how the old data moved in.
