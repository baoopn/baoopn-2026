# Credit line for client sites

Client sites built by baoopn can show a small credit in their footer. The line
has three parts: a prefix, the baoopn logo, and a suffix. The logo links to
baoopn.com, and the suffix sits inside that link, next to the logo. For
example: "Designed & built by [logo]" or "by [logo] baoopn". The site loads the
current credit from `GET https://baoopn.com/api/v1/credit`, so a change to the
text, link or brand logo reaches every client site on its own.
[api.md](api.md#credit) lists the fields and the CORS rules.

## Logo variants

The credit has two variants, **Wordmark** and **Mark**. The admin page sets
each one on its own card:

- **Prefix**: the text before the logo, up to 80 characters on one line. It
  can be empty.
- **Style**: **Color** shows the filled tile. **Mono** shows the glyph only,
  navy on light pages and cream on dark ones.
- **Suffix**: the text after the logo, up to 80 characters on one line. It can
  be empty.

Each card shows the line on a light and a dark background as you type.
**Default logo** picks the variant a site gets when it does not ask for one.

## Add a client site

1. Open **Credit** in the admin dashboard (`/admin/credit`).
2. Type the client site's origin in **Allowed client origins**, for example
   `https://example.com`, and press **Add**. An origin is the scheme and host
   with an optional port, and no path. Plain `http` works only for
   `localhost`, `127.0.0.1` and `[::1]`.
3. Press **Save**.

The browser blocks the request from any site that is not in the list. That
site keeps the static line from the snippet.

## The snippet

Paste this into the client site's footer. The admin page shows the same
snippet with a **Copy** button.

```html
<p id="baoopn-credit" class="baoopn-credit">Designed &amp; built by <a href="https://baoopn.com">baoopn</a></p>
<script type="module">
	const fallback = document.getElementById('baoopn-credit');
	try {
		if (!fallback) throw new Error('no credit line');
		const res = await fetch("https://baoopn.com/api/v1/credit");
		if (!res.ok) throw new Error('credit unavailable');
		const { data } = await res.json();
		if (!/^https:\/\//.test(data.url)) throw new Error('bad link');
		const { light, dark } = data.logo;
		const width = (img) => Math.round((20 * img.width) / (img.height || 1));
		const source = document.createElement('source');
		source.media = '(prefers-color-scheme: dark)';
		source.srcset = dark.url;
		source.width = width(dark);
		source.height = 20;
		const img = document.createElement('img');
		img.src = light.url;
		img.alt = data.alt;
		img.width = width(light);
		img.height = 20;
		const picture = document.createElement('picture');
		picture.append(source, img);
		const link = document.createElement('a');
		link.href = data.url;
		link.rel = 'noopener';
		link.append(picture);
		if (data.suffix) link.append(document.createTextNode(' ' + data.suffix));
		const line = document.createElement('p');
		line.id = 'baoopn-credit';
		line.className = 'baoopn-credit';
		if (data.prefix) line.append(document.createTextNode(data.prefix + ' '));
		line.append(link);
		fallback.replaceWith(line);
	} catch {
		// Keep the static line.
	}
</script>
```

The first line works without JavaScript. The script fetches the credit and
swaps in the prefix, logo and suffix. An empty prefix or suffix adds no text.
It builds every element with DOM methods and never parses
HTML from the response. If the request fails for any reason, the static line
stays.

## Framework components

The admin page also gives the credit as a component for sites built with a
framework. Pick **React**, **Vue** or **Svelte** in the **Framework** switch
above the snippet:

- **React**: `BaoopnCredit.tsx`, a function component for React 18 or 19. It
  starts with `'use client';`, so it also works in the Next.js App Router.
- **Vue**: `BaoopnCredit.vue`, a Vue 3 component with `<script setup>`.
- **Svelte**: `BaoopnCredit.svelte`, a Svelte 5 component with runes.

Save the code under that file name and render the component in the site's
footer. Each one shows the same static line until the credit loads, keeps it
if the request fails, and accepts only an `https` link. The framework escapes
all text, so no component renders HTML from the response. The **Logo** and
**Style** switches add the same `?logo=` and `?color=` query to every
framework, so copy the code from the admin page after you pick them. The
`connect-src` and `img-src` rules below apply to the components too. They need
no inline script, so `script-src` does not change.

## Content Security Policy

If the client site sets a Content Security Policy, it must allow the script
and both hosts:

- `script-src` must allow the inline module script. A policy such as
  `script-src 'self'` blocks it, and the static line stays. Add a nonce to the
  script tag, or allow a hash of the exact script. You can also save the script
  body as a `.js` file on the client site and load it with
  `<script type="module" src="...">`.
- `connect-src` must allow `https://baoopn.com`, where the script fetches the
  credit.
- `img-src` must allow the host that serves the logo, which is the media host
  (for example `https://media.baoopn.com`). The admin page names the exact host.

## Pick the logo and style for one site

A site asks for one variant by adding `?logo=wordmark` or `?logo=mark` to the
fetch URL in the snippet, for example
`fetch("https://baoopn.com/api/v1/credit?logo=mark")`. It then gets that
variant's prefix, suffix and style. Without `?logo=`, or with any other value
(the match is exact and lowercase), it gets the default variant. The
**Wordmark** and **Mark** switch above the snippet on the admin page adds the
query for you.

A site can also override the variant's style with `?color=color` (the filled
tile) or `?color=mono` (the glyph only, navy on light pages and cream on dark
ones). The two params work together, for example
`fetch("https://baoopn.com/api/v1/credit?logo=mark&color=mono")`. Without
`?color=`, or with any other value (the match is exact and lowercase), the site
gets the style saved on the variant. The override applies to that request only
and never changes the saved settings. `logo.style` in the response names the
style the site got. On the admin page, the **Style** switch above the snippet
adds `&color=` for **Color** or **Mono**, and adds nothing for **Saved**.

The response still has a `text` field with the same value as `prefix`. A
snippet copied before the prefix and suffix existed reads `text`, so it still
shows the prefix and the logo, but it never shows the suffix. To show a suffix,
a site needs the new snippet or one of the framework components. `text` is
deprecated; new code reads `prefix` and `suffix`.

## Caching and limits

The API marks the response `public, max-age=300`, so a browser reuses it for
up to 5 minutes. A saved change reaches client sites within that time.

The endpoint shares the public rate limit, which counts requests per IP. A
client site that renders on its server should not fetch the credit on every
request. Cache it there for a few minutes, or let the browser fetch it with
the snippet.
