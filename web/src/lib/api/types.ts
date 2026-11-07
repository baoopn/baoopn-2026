/**
 * API contract shared by the Go API and the web app (see docs/api.md).
 * Envelopes: success `{ data, meta? }`, error `{ error: { code, message } }`.
 * Timestamps are RFC 3339 strings (UTC).
 */

export interface ApiError {
	code: string;
	message: string;
}

export interface PageMeta {
	total: number;
	page: number;
	limit: number;
}

// ── Profile ─────────────────────────────────────────────────────────────────

export interface SocialLink {
	label: string; // "GitHub"
	url: string;
	/** icon key: github | linkedin | instagram | email | x | youtube | website | rss | spotify */
	icon: string;
	handle?: string; // "@baoopn"
}

export interface SkillGroup {
	group: string; // "Frontend"
	items: string[];
}

export interface ExperienceItem {
	role: string;
	org: string;
	url?: string;
	location?: string;
	start: string; // "2024-03" (YYYY-MM)
	end?: string; // empty = present
	summary_md?: string;
	summary_html?: string; // read-only, rendered by the API
}

export interface Profile {
	name: string;
	headline: string;
	location: string;
	pronouns?: string;
	email: string;
	avatar_url: string;
	cover_url: string;
	resume_url: string;
	available_for_work: boolean;
	bio_md: string;
	bio_html: string; // read-only, rendered by the API
	socials: SocialLink[];
	skills: SkillGroup[];
	experience: ExperienceItem[];
	seo: { title: string; description: string; og_image: string };
	updated_at?: string;
}

// ── Projects ────────────────────────────────────────────────────────────────

export interface ProjectSummary {
	slug: string;
	title: string;
	summary: string;
	tech: string[]; // admin tech list, else GitHub topics/language
	period: string;
	cover_url: string;
	demo_url: string;
	repo_url: string;
	language: string;
	stars: number;
	forks: number;
	archived: boolean;
	featured: boolean;
	pushed_at: string | null;
	post_slug: string;
}

export interface ProjectDetail extends ProjectSummary {
	body_html: string; // case study, or the GitHub README when there is none
	body_source: 'case_study' | 'readme' | 'none';
	languages: Record<string, number>; // bytes per language
	topics: string[];
	created_at: string | null;
}

export interface AdminProject {
	id: number;
	slug: string;
	github_id: number | null;
	repo_full_name: string;
	repo_url: string;
	gh_homepage: string;
	gh_description: string;
	gh_language: string;
	gh_languages: Record<string, number>;
	gh_topics: string[];
	gh_stars: number;
	gh_forks: number;
	gh_archived: boolean;
	gh_fork: boolean;
	gh_private: boolean;
	gh_pushed_at: string | null;
	synced_at: string | null;
	title: string;
	summary: string;
	body_md: string;
	tech: string[];
	period: string;
	cover_url: string;
	demo_url: string;
	post_slug: string;
	visible: boolean;
	featured: boolean;
	sort_order: number;
	views_30d: number;
	created_at: string;
	updated_at: string;
}

/** PATCH /api/admin/projects/{id} — every field optional. */
export type ProjectPatch = Partial<
	Pick<
		AdminProject,
		| 'slug'
		| 'title'
		| 'summary'
		| 'body_md'
		| 'tech'
		| 'period'
		| 'cover_url'
		| 'demo_url'
		| 'post_slug'
		| 'visible'
		| 'featured'
		| 'sort_order'
	>
>;

export interface SyncResult {
	added: number;
	updated: number;
	total: number;
	synced_at: string;
}

// ── Blog ────────────────────────────────────────────────────────────────────
// A post is one entry in the blog list and has 1–20 ordered parts. Readers
// move through parts at /blog/<slug> (part 1) and /blog/<slug>/<n>.

export interface TocItem {
	id: string;
	text: string;
	level: number; // 2..4
}

export interface PostSummary {
	slug: string;
	title: string;
	excerpt: string;
	cover_url: string;
	tags: string[];
	part_count: number;
	published_at: string;
	updated_at: string;
	reading_minutes: number; // all parts
	views: number; // all parts
}

/** A part reference for navigation. Empty title = show "Part <no>". */
export interface PostPartRef {
	no: number; // 1-based
	title: string;
}

export interface PostPart extends PostPartRef {
	content_html: string;
	toc: TocItem[];
	reading_minutes: number;
}

/** GET /api/v1/posts/{slug}?part=N (default 1; 404 when N is out of range). */
export interface PostDetail extends PostSummary {
	parts: PostPartRef[]; // every part in order
	part: PostPart; // the requested part
	prev: PostPartRef | null; // previous part of this post
	next: PostPartRef | null; // next part of this post
}

export type PostState = 'draft' | 'scheduled' | 'published';

export interface AdminPostPart {
	no: number;
	title: string;
	content_md: string;
	reading_minutes: number;
}

export interface AdminPost {
	id: number;
	slug: string;
	title: string;
	excerpt: string;
	cover_url: string;
	tags: string[];
	status: 'draft' | 'published';
	state: PostState; // derived: published + future published_at = scheduled
	published_at: string | null;
	reading_minutes: number; // all parts
	views: number;
	parts: AdminPostPart[]; // in order, no = 1..n
	created_at: string;
	updated_at: string;
}

/** POST/PUT /api/admin/posts body. Parts are saved in array order. */
export interface PostInput {
	slug?: string; // generated from title when empty
	title: string;
	excerpt: string; // empty = generated from part 1
	cover_url: string;
	tags: string[];
	status: 'draft' | 'published';
	published_at: string | null; // null + published = now
	parts: { title: string; content_md: string }[]; // 1–20 parts
}

export interface PreviewResult {
	html: string;
	toc: TocItem[];
	reading_minutes: number;
}

export interface TagCount {
	tag: string;
	count: number;
}

// ── Media ───────────────────────────────────────────────────────────────────

export interface Media {
	id: number;
	url: string; // "/media/<filename>"
	filename: string;
	original: string;
	content_type: string;
	size_bytes: number;
	created_at: string;
}

// ── Contact & threads ───────────────────────────────────────────────────────

export interface ContactInput {
	name: string; // 1–100
	email: string;
	subject: string; // 0–150 (default "Message from <name>")
	message: string; // 10–5000
	website: string; // honeypot, must be empty
	started_at: number; // epoch ms the form was rendered (min 3 s)
	turnstile_token?: string;
}

export type ThreadStatus = 'open' | 'archived' | 'spam';

export interface ThreadSummary {
	id: number;
	name: string;
	email: string;
	subject: string;
	status: ThreadStatus;
	unread: boolean;
	last_message_at: string;
	last_direction: 'inbound' | 'outbound';
	last_preview: string;
	message_count: number;
	created_at: string;
}

export interface ThreadMessage {
	id: number;
	direction: 'inbound' | 'outbound';
	source: 'form' | 'email' | 'admin';
	body: string; // plain text, quoted history stripped
	full_body: string; // full text when something was stripped, else ""
	status: 'received' | 'sending' | 'sent' | 'failed';
	error: string;
	from_address: string;
	other_sender: boolean; // inbound from an address other than the thread's
	attachments_dropped: number;
	created_at: string;
	sent_at: string | null;
}

export interface ThreadDetail {
	thread: ThreadSummary;
	messages: ThreadMessage[];
	reply_enabled: boolean; // false when SMTP is not configured
}

// ── Analytics ───────────────────────────────────────────────────────────────

export type AnalyticsRange = '24h' | '7d' | '30d' | '90d';

export interface Breakdown {
	key: string;
	views: number;
	visitors?: number;
	avg_visible_ms?: number;
}

export interface AnalyticsReport {
	range: AnalyticsRange;
	bucket: 'hour' | 'day';
	time_zone: string;
	totals: {
		views: number;
		visitors: number;
		avg_visible_ms: number;
		live_now: number;
		views_prev: number; // same-length previous period, for deltas
		visitors_prev: number;
	};
	timeseries: { t: string; views: number; visitors: number }[];
	paths: Breakdown[];
	referrers: Breakdown[];
	devices: Breakdown[];
	browsers: Breakdown[];
	countries: Breakdown[];
}

/** POST /api/v1/events (sendBeacon). */
export interface BeaconEvent {
	type: 'pageview' | 'engagement';
	path: string;
	referrer?: string;
	width?: number;
	visible_ms?: number;
}

// ── Admin misc ──────────────────────────────────────────────────────────────

export interface AdminConfig {
	clerk_publishable_key: string;
	version: string;
	mail_enabled: boolean;
	inbound_enabled: boolean;
	github_user: string;
}

export interface AdminUser {
	id: string;
	email: string;
	name: string;
	image_url: string;
	role?: string;
}

export interface Overview {
	unread_threads: number;
	open_threads: number;
	posts: { published: number; drafts: number; scheduled: number };
	projects: { visible: number; total: number };
	views_7d: number;
	visitors_7d: number;
	live_now: number;
	last_sync_at: string | null;
	recent_threads: ThreadSummary[];
	top_paths_7d: Breakdown[];
}

export interface SiteSettings {
	notify_email: string;
	contact_enabled: boolean;
	auto_reply_enabled: boolean; // send the visitor a copy that starts the email thread
	auto_reply_text: string;
	signature: string; // appended to admin replies
}

// ── Listening (spotify-tracker proxy) ──────────────────────────────────────

export interface SpotifyArtist {
	id: string;
	name: string;
	external_url?: string;
	image_url?: string;
}

export interface SpotifyTrack {
	id: string;
	name: string;
	duration_ms: number;
	explicit: boolean;
	external_url?: string;
	album?: { id: string; name: string; cover_image_url?: string; external_url?: string };
	artists: SpotifyArtist[];
}

export interface NowPlaying {
	is_playing: boolean;
	progress_ms: number | null;
	fetched_at: string;
	track: SpotifyTrack | null;
}

export interface RecentPlay {
	id: string;
	played_at: string;
	track: SpotifyTrack;
}

export interface TopArtist {
	artist: SpotifyArtist;
	plays: number;
	last_played: string;
}
