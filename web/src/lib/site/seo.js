// SEO helpers shared by the server hook and the public pages: which assets to
// preload, which paths get an X-Robots-Tag, and schema.org JSON-LD nodes.
// Plain JS (with JSDoc types) so the node test runner can import it.

/**
 * Preload only what the first paint needs. Fontsource ships one woff2 per
 * unicode range (cyrillic, greek, vietnamese, latin-ext, latin); the browser
 * fetches the others on demand through unicode-range, but a preload ignores
 * that and downloads all of them. So only the Latin fonts are preloaded.
 * @param {{ type: string, path: string }} asset
 */
export function preloadAsset({ type, path }) {
	if (type === 'font') return /-latin-wght-normal\.[\w-]+\.woff2$/.test(path);
	return type === 'js' || type === 'css';
}

/**
 * X-Robots-Tag for a path, or null. The admin app must never show up in search,
 * even when someone links to it.
 * @param {string} pathname
 */
export function robotsHeader(pathname) {
	return pathname === '/admin' || pathname.startsWith('/admin/') ? 'noindex, nofollow' : null;
}

/** @param {string} origin */
export const personId = (origin) => `${origin}/#person`;
/** @param {string} origin */
export const websiteId = (origin) => `${origin}/#website`;

/**
 * A reference to the site owner that still reads on its own: Google parses
 * each page alone, and a bare @id names a node that may only be on the home page.
 * @param {string} origin
 * @param {string} name
 */
export function personRef(origin, name) {
	return { '@type': 'Person', '@id': personId(origin), name, url: origin + '/' };
}

/** Comma list without case-only repeats ("Docker", "docker"). @param {string[] | undefined} list */
function keywordList(list) {
	const seen = new Set();
	const out = (list ?? []).filter((k) => {
		const key = k.trim().toLowerCase();
		if (!key || seen.has(key)) return false;
		seen.add(key);
		return true;
	});
	return out.length ? out.join(', ') : undefined;
}

/** @param {unknown} v */
const nonEmpty = (v) => (Array.isArray(v) ? (v.length ? v : undefined) : v || undefined);

/**
 * Profile links usable as schema.org sameAs: absolute http(s) URLs only.
 * @param {Array<{ url: string }>} socials
 */
export function sameAs(socials) {
	return socials
		.map((s) => {
			try {
				const u = new URL(s.url);
				return u.protocol === 'https:' || u.protocol === 'http:' ? u.href : undefined;
			} catch {
				return undefined;
			}
		})
		.filter((u) => u !== undefined);
}

/**
 * The site owner. Every other node points at it by @id.
 * @param {string} origin
 * @param {{ name: string, headline?: string, image?: string, email?: string, location?: string, sameAs?: string[] }} p
 */
export function personNode(origin, p) {
	return {
		'@type': 'Person',
		'@id': personId(origin),
		name: p.name,
		jobTitle: p.headline || undefined,
		url: origin + '/',
		image: p.image || undefined,
		email: p.email ? `mailto:${p.email}` : undefined,
		address: p.location ? { '@type': 'PostalAddress', addressLocality: p.location } : undefined,
		sameAs: nonEmpty(p.sameAs)
	};
}

/**
 * Google shows `name` as the site name in results, so keep it short.
 * @param {string} origin
 * @param {{ name: string, alternateName?: string, description?: string }} p
 */
export function websiteNode(origin, p) {
	return {
		'@type': 'WebSite',
		'@id': websiteId(origin),
		url: origin + '/',
		name: p.name,
		alternateName: p.alternateName && p.alternateName !== p.name ? p.alternateName : undefined,
		description: p.description || undefined,
		inLanguage: 'en',
		publisher: { '@id': personId(origin) }
	};
}

/** Wraps nodes in one JSON-LD document. @param {object[]} nodes */
export function graph(nodes) {
	return { '@context': 'https://schema.org', '@graph': nodes };
}

/**
 * Home > ... trail. Each crumb is [name, path].
 * @param {string} origin
 * @param {Array<[string, string]>} crumbs
 */
export function breadcrumbs(origin, crumbs) {
	return {
		'@type': 'BreadcrumbList',
		itemListElement: [['Home', '/'], ...crumbs].map(([name, path], i) => ({
			'@type': 'ListItem',
			position: i + 1,
			name,
			item: origin + path
		}))
	};
}

/**
 * One blog post page (a single part of a multi-part post).
 * @param {string} origin
 * @param {{
 *   url: string, headline: string, description?: string, image?: string,
 *   published?: string, modified?: string, keywords?: string[], author: string,
 *   series?: { url: string, headline: string },
 *   parts?: Array<{ position: number, name: string, url: string }>
 * }} p
 */
export function blogPostingNode(origin, p) {
	return {
		'@type': 'BlogPosting',
		'@id': `${p.url}#post`,
		headline: p.headline.length > 110 ? p.headline.slice(0, 109).trimEnd() + '…' : p.headline,
		description: p.description || undefined,
		image: p.image || undefined,
		datePublished: p.published || undefined,
		dateModified: p.modified || p.published || undefined,
		keywords: keywordList(p.keywords),
		inLanguage: 'en',
		url: p.url,
		mainEntityOfPage: p.url,
		author: personRef(origin, p.author),
		publisher: personRef(origin, p.author),
		isPartOf: p.series
			? { '@type': 'BlogPosting', '@id': `${p.series.url}#post`, headline: p.series.headline, url: p.series.url }
			: { '@type': 'Blog', '@id': `${origin}/blog#blog`, url: origin + '/blog' },
		hasPart: p.parts?.length
			? p.parts.map((x) => ({ '@type': 'WebPageElement', position: x.position, name: x.name, url: x.url }))
			: undefined
	};
}

/** The blog index. @param {string} origin @param {{ name: string, author: string, description?: string }} p */
export function blogNode(origin, p) {
	return {
		'@type': 'Blog',
		'@id': `${origin}/blog#blog`,
		url: origin + '/blog',
		name: p.name,
		description: p.description || undefined,
		inLanguage: 'en',
		author: personRef(origin, p.author),
		isPartOf: { '@type': 'WebSite', '@id': websiteId(origin), url: origin + '/' }
	};
}

/**
 * A project page: SoftwareSourceCode when it has a public repo, else CreativeWork.
 * @param {string} origin
 * @param {{
 *   url: string, name: string, author: string, description?: string, image?: string,
 *   repo?: string, languages?: string[], keywords?: string[], created?: string | null,
 *   modified?: string | null
 * }} p
 */
export function projectNode(origin, p) {
	return {
		'@type': p.repo ? 'SoftwareSourceCode' : 'CreativeWork',
		'@id': `${p.url}#project`,
		name: p.name,
		description: p.description || undefined,
		image: p.image || undefined,
		url: p.url,
		mainEntityOfPage: p.url,
		codeRepository: p.repo || undefined,
		programmingLanguage: p.repo ? nonEmpty(p.languages) : undefined,
		keywords: keywordList(p.keywords),
		dateCreated: p.created || undefined,
		dateModified: p.modified || undefined,
		author: personRef(origin, p.author)
	};
}

/**
 * The About page.
 * @param {string} origin
 * @param {object} person a personNode
 * @param {string | undefined} modified
 */
export function profilePageNode(origin, person, modified) {
	return {
		'@type': 'ProfilePage',
		url: origin + '/about',
		dateModified: modified || undefined,
		isPartOf: { '@type': 'WebSite', '@id': websiteId(origin), url: origin + '/' },
		mainEntity: person
	};
}
