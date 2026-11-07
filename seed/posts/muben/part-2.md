The public site has one job: show the brand and publish the journal. It has to look calm, load fast and read well to a search engine in two languages. This part covers how I built the look, the motion, and the SEO.

## Design tokens

The brand is black and white, but pure `#000` on pure `#fff` looks harsh on a screen. I chose a warm near-black and an off-white instead. They live as Tailwind CSS v4 theme tokens in `app.css`:

```css
@theme {
  --color-ink: #0b0b0a;
  --color-ink-soft: #161513;
  --color-paper: #f2f1ec;
  --color-paper-dim: #94908a;

  --font-sans: 'Helvetica Neue', Arial, sans-serif;
  --font-mono: ui-monospace, 'SFMono-Regular', 'Menlo', monospace;
  /* ... */
  --ease-muben: cubic-bezier(0.22, 1, 0.36, 1);
}
```

Four colors and one easing curve cover the whole site. Classes like `bg-ink` and `text-paper-dim` come straight from these tokens. There is no color picked by hand inside a component.

The buttons follow the brand references: outline boxes, no fill, wide letter spacing and a small glyph in front, like `+ EXPLORE`. One `Button` component renders an `<a>` when it has an `href` and a `<button>` when it does not. The glyph slides a few pixels on hover.

## The logo is generated

The logo has three parts: the MUBEN wordmark, the word CURATED and the Japanese line. The source artwork ships MUBEN and the Japanese line as outlines, but CURATED was still live text in a font.

I did not want the site to load a web font for one word. So a Python script, `build-logos.py`, reads the brand SVGs, uses fontTools to turn the CURATED text into paths, and writes the final SVG files and two Svelte components. Nobody edits those outputs by hand. A CI job runs the script again and fails if the result differs from what is in the repo.

## Motion without a flash

The site uses soft motion: blocks fade in, rise and lose a blur as you scroll. Motion like this has two classic problems. Content can flash before the script hides it. And with JavaScript off, hidden content may never appear, which also hurts crawlers.

I solved both with one inline script in `app.html` that runs before first paint:

```html
<script>
  document.documentElement.classList.add('js');
</script>
```

Every hidden start state in the CSS needs that class, and every rule sits inside `prefers-reduced-motion: no-preference`:

```css
@media (prefers-reduced-motion: no-preference) {
  :root.js [data-reveal] {
    opacity: 0;
    transform: translateY(1.75rem);
    filter: blur(6px);
    transition: opacity 0.8s var(--ease-muben) /* ... */;
  }
  :root.js [data-reveal].in-view {
    opacity: 1;
    transform: none;
    filter: none;
  }
}
```

A small Svelte action adds `in-view` through an `IntersectionObserver`. Without JavaScript, or with reduced motion turned on, the content is visible from the start.

### The Safari bug

My first version put `reveal` on the whole article body. It broke in two ways. The reader never saw the effect, because the body is thousands of pixels tall and it animated once, when its top edge came in. Worse, the blur turned the body into one huge layer. Safari cannot draw a layer past its texture limit, so the text went blank or flickered on scroll.

The fix was a second action, `revealBlocks`. It animates each top-level block of the post (each paragraph, heading or image) on its own, with a shorter rise and no blur. Each animated layer stays about one screen tall. The effect now happens where the reader is looking, and the Safari bug is gone.

![A blog post page mid-scroll, with paragraphs fading in one at a time](screenshot:public-post)

## Two languages, checked by the compiler

UI text (nav labels, button text, form labels) lives in two catalogs, `en.ts` and `vi.ts`. English is the source of truth, and TypeScript makes sure Vietnamese matches it:

```ts
export type Messages = typeof en;

// Assigning to Record<Locale, Messages> makes TypeScript enforce that vi.ts
// matches en.ts exactly: a missing/extra key fails the build here.
const catalogs: Record<Locale, Messages> = { en, vi };
```

If I add a key to English and forget Vietnamese, the build fails. A param matcher on the `[lang=lang]` route accepts `en` and `vi` and nothing else, so `/fr/blogs` is a 404 instead of a half-rendered page. A server hook writes the active language into `<html lang>`.

## SEO

Because pages render on the server, view-source shows the full post. There is no client fetch before the content appears. On top of that:

- A `Seo` component sets the title, description, canonical URL, OpenGraph and Twitter tags. It also sets `og:locale` (`en_US` or `vi_VN`) and the other language as an alternate.
- The language layout adds `hreflang` links, so search engines treat the EN and VI pages as translations, not duplicates.
- Each post page emits `BlogPosting` JSON-LD with the publish and modified dates.
- `/sitemap.xml` is built per request from the live post list. Every URL carries alternates for both languages and an `x-default` that points to English. Post `<lastmod>` comes from `updatedAt`, so an edited post looks edited to a crawler.
- `robots.txt` blocks `/_admin`, which is a client app with nothing to index.

## Speed and failure

Public pages send `Cache-Control: public, max-age=0, s-maxage=60`. Browsers always check back, but the CDN can serve a page for a minute. Behind that, the Go API caches its JSON in Redis for five minutes and clears it on publish.

The public API client is built to fail soft. If the API is down, the blog list comes back empty and the home page shows its plain hero. The site stays up with less on it.

One detail here cost me time. SvelteKit passes a `fetch` into `load` functions, and that `fetch` follows browser CORS rules even on the server. The API lives on a different origin, so every server-side read failed the CORS check and fell back to empty data. Nothing crashed, the pages were just blank. The fix was to call the platform `fetch` for these reads. CORS is a browser rule. A server-to-server call does not need it.

![The Shop coming-soon page with the silver ring accent](screenshot:public-shop)
