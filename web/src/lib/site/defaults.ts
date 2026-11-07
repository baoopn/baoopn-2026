import type { Profile } from '#lib/api/types.js';

/** Used when the API is down or returns an incomplete profile. */
export const DEFAULT_PROFILE: Profile = {
	name: 'Bao Nguyen',
	headline: 'Full-stack developer',
	location: '',
	email: 'me@baoopn.com',
	avatar_url: '',
	cover_url: '',
	resume_url: '',
	available_for_work: false,
	bio_md: '',
	bio_html: '',
	socials: [
		{ label: 'GitHub', url: 'https://github.com/baoopn', icon: 'github', handle: '@baoopn' },
		{ label: 'LinkedIn', url: 'https://www.linkedin.com/in/baoopn', icon: 'linkedin' },
		{ label: 'Instagram', url: 'https://www.instagram.com/baoongisoo', icon: 'instagram' }
	],
	skills: [],
	experience: [],
	seo: { title: '', description: '', og_image: '' }
};

export function withDefaults(p: Partial<Profile> | null | undefined): Profile {
	if (!p) return DEFAULT_PROFILE;
	return {
		...DEFAULT_PROFILE,
		...p,
		name: p.name || DEFAULT_PROFILE.name,
		headline: p.headline || DEFAULT_PROFILE.headline,
		socials: p.socials ?? DEFAULT_PROFILE.socials,
		skills: p.skills ?? [],
		experience: p.experience ?? [],
		seo: { ...DEFAULT_PROFILE.seo, ...(p.seo ?? {}) }
	};
}

export const NAV = [
	{ href: '/', label: 'Home' },
	{ href: '/projects', label: 'Projects' },
	{ href: '/blog', label: 'Blog' },
	{ href: '/about', label: 'About' },
	{ href: '/contact', label: 'Contact' }
] as const;

export const SITE_DESCRIPTION =
	'Bao Nguyen is a full-stack developer. Projects, writing and what he is listening to.';
