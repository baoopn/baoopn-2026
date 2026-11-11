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

/** Plate behind a project logo: the slug's pastel tile, or dark ink. Same in both themes. */
export type ProjectLogoBg = 'tile' | 'ink';

export interface ProjectSummary {
	slug: string;
	title: string;
	summary: string;
	tech: string[]; // admin tech list, else GitHub topics/language
	period: string;
	cover_url: string;
	logo_url: string; // '' = none; shown when there is no cover
	logo_bg: ProjectLogoBg;
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
	logo_url: string;
	logo_bg: ProjectLogoBg;
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
		| 'logo_url'
		| 'logo_bg'
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

/** 202 from POST /projects/sync: the sync outlived the request and goes on in the background. */
export interface SyncRunning {
	status: 'running';
}

export type SyncResponse = SyncResult | SyncRunning;

// ── Blog ────────────────────────────────────────────────────────────────────
// A post is one entry in the blog list and has 1–20 ordered parts. Readers
// move through parts at /blog/<slug> (part 1) and /blog/<slug>/<n>. Every post
// is either a tech post or a life post (/blog?category=tech|life).

export type PostCategory = 'tech' | 'life';

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
	category: PostCategory;
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
	category: PostCategory;
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
	category: PostCategory; // the API treats a missing or empty value as 'tech'
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

export type MediaStatus = 'ready' | 'processing' | 'failed';

export interface Media {
	id: number;
	url: string; // MEDIA_PUBLIC_URL + "/<filename>" (or "/media/<filename>" without a public bucket)
	filename: string;
	original: string;
	content_type: string; // "image/webp" while processing
	size_bytes: number; // 0 while processing
	status: MediaStatus; // processing: poll GET /api/admin/media/{id}
	error: string; // why it failed, or why the original was published instead ("" when fine)
	converted: boolean; // true for WebP outputs; false for fallbacks, SVG, PDF and other as-is files
	created_at: string;
}

// Orphan cleanup (GET/POST /api/admin/media/orphans). References are matched
// by the file's 24-hex stem across posts, post parts, projects, settings and
// thread messages (sent replies).
export type OrphanKind = 'row' | 'object' | 'local' | 'local_copy' | 'original' | 'temp';

export interface OrphanCandidate {
	key: string; // "<kind>:<name>", what POST takes
	kind: OrphanKind;
	name: string; // file name, bucket key, or the original's stem
	media_id?: number; // row, local_copy
	url?: string; // row: the file's URL
	original?: string;
	content_type?: string;
	size_bytes: number;
	created_at?: string;
	modified_at: string; // row: last used (touched_at)
	age_seconds: number;
	reason: string;
	eligible: boolean;
	blocked: '' | 'grace' | 'disabled' | 'truncated'; // truncated: bucket listing cut short, rows kept
	eligible_at?: string; // blocked = grace
}

export interface OrphanRun {
	at: string;
	trigger: 'auto' | 'manual';
	deleted: number;
	bytes: number;
	errors: number;
}

export interface OrphanPlan {
	enabled: boolean; // false: MEDIA_ORPHAN_GRACE=0
	auto: boolean; // daily cleanup
	grace_seconds: number;
	max_per_pass: number;
	storage: 'bucket' | 'disk';
	generated_at: string;
	truncated: boolean; // bucket listing stopped at 10 000 keys: no object deletions
	candidates: OrphanCandidate[]; // oldest first
	missing: { stem: string; where: string }[]; // referenced, but no row or file
	kept: {
		referenced: number;
		processing: number;
		in_grace: number;
		retained_original: number;
		foreign: number;
		foreign_bytes: number;
	};
	totals: {
		candidates: number;
		eligible: number;
		bytes: number;
		eligible_bytes: number;
		would_delete: number; // min(eligible, max_per_pass)
	};
	last_run: OrphanRun | null;
}

export interface OrphanResult {
	deleted: {
		rows: number;
		objects: number;
		local: number;
		local_copies: number;
		originals: number;
		temp: number;
	};
	bytes: number;
	already_gone: number;
	skipped: { key: string; why: string }[];
	errors: number;
	capped: boolean;
	remaining: number;
	reason?: 'brake';
}

export interface OrphanSettings {
	auto: boolean;
	last_run: OrphanRun | null;
	last_auto_at?: string;
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

export type PlaylistPeriod = 'week' | 'month' | 'year';

export interface PlaylistTrack {
	position: number;
	plays: number;
	track: SpotifyTrack;
}

export interface TopPlaylist {
	period: PlaylistPeriod;
	/** RFC 3339 UTC, inclusive. */
	period_start: string;
	/** RFC 3339 UTC, exclusive. */
	period_end: string;
	/** IANA zone the period was cut in. */
	time_zone: string;
	/** "Week of Nov 2, 2026" */
	label: string;
	/** "Top 50 · Week of Nov 2, 2026" */
	name: string;
	/** ^[A-Za-z0-9]+$ */
	spotify_id: string;
	/** Starts with https://open.spotify.com/ */
	spotify_url: string;
	track_count: number;
	total_plays: number;
	/** RFC 3339 UTC. */
	published_at: string;
	/** Newest entry of each period only; [] otherwise. */
	tracks: PlaylistTrack[];
}

export interface TopPlaylists {
	time_zone: string;
	/** Newest first, always arrays. */
	periods: { week: TopPlaylist[]; month: TopPlaylist[]; year: TopPlaylist[] };
}

// Brand assets. Two brands share one system:
// - baoopn: GET /api/v1/brand, /api/admin/brand; settings key "brand"; files
//   under brand/<stem>.<hash16>.<ext>.
// - listening (listening.baoopn.com): GET /api/v1/brand/listening,
//   /api/admin/brand/listening; settings key "brand_listening"; files under
//   brand/listening/<stem>.<hash16>.<ext>.
// The admin can replace each slot of either brand.
export type BrandName = 'baoopn' | 'listening';

export type BrandSlot =
	| 'favicon_ico'
	| 'favicon_svg'
	| 'apple_touch_icon'
	| 'wordmark_primary'
	| 'wordmark_inverse'
	| 'wordmark_navy'
	| 'wordmark_cream'
	| 'monogram_primary'
	| 'monogram_inverse'
	| 'monogram_navy'
	| 'monogram_cream'
	| 'email_logo';

type ListeningIconName =
	| 'headphones'
	| 'now_playing'
	| 'now_playing_static'
	| 'paused'
	| 'playlist'
	| 'track';

export type ListeningBrandSlot =
	| 'favicon_ico'
	| 'favicon_svg'
	| 'apple_touch_icon'
	| 'favicon_16'
	| 'favicon_32'
	| 'favicon_48'
	| 'icon_192'
	| 'icon_512'
	| 'icon_maskable_512'
	| `mark_${'primary' | 'inverse' | 'navy' | 'cream'}`
	| `wordmark_${'primary' | 'inverse' | 'navy' | 'cream'}`
	| `icon_${ListeningIconName}_${'navy' | 'cream'}`
	| 'og_image'
	| 'email_logo';

/** Display group of a slot on the admin page. */
export type BrandGroup = 'favicons' | 'logos' | 'icons' | 'social' | 'email';

export interface BrandAsset<S extends string = BrandSlot> {
	slot: S;
	url: string;
	content_type: string;
	width: number;
	height: number;
	custom: boolean;
}

export interface Brand<S extends string = BrandSlot> {
	assets: Record<S, BrandAsset<S>>;
}

export type ListeningBrand = Brand<ListeningBrandSlot>;

export interface AdminBrandAsset<S extends string = BrandSlot | ListeningBrandSlot>
	extends BrandAsset<S> {
	label: string;
	kind: 'icon' | 'logo' | 'image';
	group: BrandGroup;
	size_bytes: number;
	/** Uploaded filename; "" for the default. */
	original: string;
	/** RFC 3339 UTC; null for the default. */
	updated_at: string | null;
	default_url: string;
	/** Values for <input type=file accept>. */
	accept: string[];
	/** "PNG, exactly 180×180 px" */
	rule: string;
	/** Which backgrounds to preview it on. */
	preview: 'light' | 'dark' | 'both';
}

// Credit line for client sites (GET /api/v1/credit, /api/admin/credit).
// The public route sends CORS headers only to the listed origins. The line is
// prefix + linked logo + suffix, set separately for each logo variant; a client
// site picks the variant with ?logo=wordmark|mark.
export type CreditLogoKind = 'wordmark' | 'mark';
/** color: filled tile. mono: glyph only, navy on light pages, cream on dark. */
export type CreditLogoStyle = 'color' | 'mono';

export interface CreditImage {
	/** Absolute URL. */
	url: string;
	width: number;
	height: number;
	content_type: string;
}

export interface CreditLogo {
	kind: CreditLogoKind;
	style: CreditLogoStyle;
	light: CreditImage;
	dark: CreditImage;
}

export interface Credit {
	/** @deprecated same as prefix */
	text: string;
	prefix: string;
	suffix: string;
	url: string;
	alt: string;
	logo: CreditLogo;
	/** RFC 3339 UTC; null until the first save. */
	updated_at: string | null;
}

/** One logo variant's line: prefix and suffix are 0 to 80 characters. */
export interface CreditVariant {
	prefix: string;
	suffix: string;
	style: CreditLogoStyle;
}

export interface AdminCredit {
	url: string;
	/** Default variant, used when a site does not add ?logo=. */
	logo: CreditLogoKind;
	variants: Record<CreditLogoKind, CreditVariant>;
	/** Normalised origins, for example https://example.com. */
	origins: string[];
	updated_at: string | null;
	/** Absolute URL of the public endpoint. */
	endpoint: string;
	/** logos[kind][style], all four, for the previews. */
	logos: Record<CreditLogoKind, Record<CreditLogoStyle, CreditLogo>>;
	limits: { prefix_max: number; suffix_max: number; url_max: number; origins_max: number };
}

export interface CreditInput {
	url: string;
	logo: CreditLogoKind;
	variants: Record<CreditLogoKind, CreditVariant>;
	origins: string[];
}

// Off-site backups (GET /api/admin/backups). The backup sidecar writes the
// status rows; the API reads them and stores only the settings doc and
// queued run requests. Timestamps are RFC 3339 UTC. No secret values, only
// whether each one is set.
export type BackupHealth =
	| 'ok'
	| 'warning'
	| 'stale'
	| 'waiting'
	| 'paused'
	| 'not_configured'
	| 'offline'
	| 'not_reporting';

export interface BackupSetting<T> {
	/** override ?? env */
	value: T | null;
	/** The sidecar's env default; null when the sidecar has not reported. */
	env: T | null;
	/** The stored admin override, if valid. */
	override: T | null;
	source: 'env' | 'override';
}

export type BackupRunKind = 'scheduled' | 'startup' | 'retry' | 'manual' | 'cli';
export type BackupRunStatus = 'queued' | 'running' | 'ok' | 'failed';

export interface BackupRun {
	id: number;
	kind: BackupRunKind;
	status: BackupRunStatus;
	requested_at: string | null;
	started_at: string | null;
	finished_at: string | null;
	name: string | null;
	size_bytes: number | null;
	sha256: string | null;
	tables: number | null;
	goose_version: number | null;
	pruned: number | null;
	prune_error: string | null;
	error: string | null;
}

export interface BackupAgent {
	version: string;
	started_at: string;
	last_seen_at: string;
	online: boolean;
	state: 'idle' | 'waiting' | 'running' | 'paused';
	idle_reason: string | null;
	next_run_at: string | null;
	settings_applied_at: string | null;
}

export interface BackupStatus {
	health: BackupHealth;
	checked_at: string;
	agent: BackupAgent | null;
	destination: {
		kind: 's3' | 'rclone' | null;
		s3_provider: string | null;
		s3_bucket: string | null;
		prefix: string | null;
	};
	secrets: {
		age_recipient: boolean;
		s3_endpoint: boolean;
		s3_access_key_id: boolean;
		s3_secret_access_key: boolean;
		ping_url: boolean;
	};
	settings: {
		backup_time: BackupSetting<string>;
		keep_days: BackupSetting<number>;
		keep_min: BackupSetting<number>;
		max_age_hours: BackupSetting<number>;
		retry_minutes: number | null;
		on_start: boolean | null;
		paused: boolean;
		paused_at: string | null;
		updated_at: string | null;
		/** Saved settings the sidecar has not applied yet. */
		pending: boolean;
	};
	last_success_at: string | null;
	last_run: BackupRun | null;
	active_run: BackupRun | null;
	/** Newest 20 by id. */
	runs: BackupRun[];
}

/** PUT /api/admin/backups/settings. null = use the env default. */
export interface BackupSettingsInput {
	backup_time: string | null;
	keep_days: number | null;
	keep_min: number | null;
	max_age_hours: number | null;
}
