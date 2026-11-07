<script lang="ts">
	import { GitBranch, LoaderCircle, Save } from '@lucide/svelte';
	import type { Profile } from '#lib/api/types.js';
	import PageHeader from '#lib/admin/components/PageHeader.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import MediaField from '#lib/admin/components/MediaField.svelte';
	import Switch from '#lib/admin/components/Switch.svelte';
	import Dialog from '#lib/admin/components/Dialog.svelte';
	import MarkdownEditor from '#lib/admin/editor/MarkdownEditor.svelte';
	import SocialsEditor from '#lib/admin/profile/SocialsEditor.svelte';
	import SkillsEditor from '#lib/admin/profile/SkillsEditor.svelte';
	import ExperienceEditor from '#lib/admin/profile/ExperienceEditor.svelte';
	import { get, post, put } from '#lib/admin/api.js';
	import { guardUnsaved } from '#lib/admin/guard.svelte.js';
	import { auth } from '#lib/admin/auth.svelte.js';
	import { errorMessage } from '#lib/admin/format.js';
	import ConfirmDialog from '#lib/admin/components/ConfirmDialog.svelte';
	import { toasts } from '#lib/admin/toast.svelte.js';
	import { uploads } from '#lib/admin/media/upload.svelte.js';

	let form = $state<Profile | null>(null);
	let baseline = $state('');
	let loadError = $state<string | null>(null);
	let saving = $state(false);
	let confirmUploads = $state(false);

	function set(p: Profile) {
		const f: Profile = {
			...p,
			socials: p.socials ?? [],
			skills: p.skills ?? [],
			experience: p.experience ?? [],
			seo: p.seo ?? { title: '', description: '', og_image: '' }
		};
		form = f;
		baseline = JSON.stringify(f);
	}

	async function load() {
		loadError = null;
		try {
			set(await get<Profile>('/profile'));
		} catch (err) {
			loadError = errorMessage(err);
		}
	}
	void load();

	const dirty = $derived(!!form && JSON.stringify(form) !== baseline);
	guardUnsaved(() => dirty && !saving);

	async function save(e?: SubmitEvent, force = false) {
		e?.preventDefault();
		if (!form || saving) return;
		if (!force && uploads.pending > 0) {
			confirmUploads = true;
			return;
		}
		saving = true;
		try {
			set(await put<Profile>('/profile', $state.snapshot(form)));
			toasts.success('Profile saved');
		} catch (err) {
			toasts.error('Could not save the profile', errorMessage(err));
		} finally {
			saving = false;
		}
	}

	function onkeydown(e: KeyboardEvent) {
		if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
			e.preventDefault();
			void save();
		}
	}

	// ---- import from GitHub (fills empty fields only, after confirmation) ----
	type Fill = { key: string; label: string; value: string; apply: (f: Profile) => void };
	let importing = $state(false);
	let fills = $state.raw<Fill[]>([]);
	let importOpen = $state(false);

	const TEXT: [keyof Profile, string][] = [
		['name', 'Name'],
		['headline', 'Headline'],
		['location', 'Location'],
		['email', 'Email'],
		['avatar_url', 'Avatar'],
		['cover_url', 'Cover'],
		['resume_url', 'Résumé'],
		['bio_md', 'Bio']
	];

	async function importGithub() {
		if (!form) return;
		importing = true;
		try {
			const gh = await post<Partial<Profile>>('/profile/github');
			const f = form;
			const out: Fill[] = [];
			for (const [k, label] of TEXT) {
				const v = gh[k];
				if (typeof v === 'string' && v.trim() && !String(f[k] ?? '').trim()) {
					out.push({ key: k, label, value: v, apply: (p) => Object.assign(p, { [k]: v }) });
				}
			}
			if (gh.socials?.length && f.socials.length === 0) {
				const s = gh.socials;
				out.push({
					key: 'socials',
					label: 'Links',
					value: s.map((x) => x.label || x.url).join(', '),
					apply: (p) => (p.socials = s)
				});
			}
			if (gh.skills?.length && f.skills.length === 0) {
				const s = gh.skills;
				out.push({
					key: 'skills',
					label: 'Skills',
					value: s.map((g) => g.group).join(', '),
					apply: (p) => (p.skills = s)
				});
			}
			fills = out;
			if (out.length === 0)
				toasts.info('Nothing to import', 'Every field GitHub has is already filled.');
			else importOpen = true;
		} catch (err) {
			toasts.error('GitHub import failed', errorMessage(err));
		} finally {
			importing = false;
		}
	}

	function applyImport() {
		if (!form) return;
		for (const f of fills) f.apply(form);
		importOpen = false;
		toasts.success('Imported from GitHub', 'Review the fields, then save.');
	}
</script>

<svelte:window {onkeydown} />

<PageHeader title="Profile" eyebrow="About page" description="Who Bao is, as the site tells it.">
	{#snippet actions()}
		<button
			type="button"
			class="btn btn-secondary"
			onclick={importGithub}
			disabled={!form || importing}
		>
			{#if importing}<LoaderCircle
					class="size-4 animate-spin"
					aria-hidden="true"
				/>{:else}<GitBranch class="size-4" aria-hidden="true" />{/if}
			Import from GitHub
		</button>
		<button type="submit" form="profile-form" class="btn btn-primary" disabled={!dirty || saving}>
			{#if saving}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{:else}<Save
					class="size-4"
					aria-hidden="true"
				/>{/if}
			Save
		</button>
	{/snippet}
</PageHeader>

{#if loadError}
	<ErrorState message={loadError} onretry={load} />
{:else if !form}
	<div class="flex flex-col gap-4" aria-busy="true">
		<div class="skeleton h-40"></div>
		<div class="skeleton h-72"></div>
		<span class="sr-only">Loading profile</span>
	</div>
{:else}
	<form id="profile-form" class="flex flex-col gap-10" onsubmit={(e) => save(e)}>
		{#if dirty}
			<p class="text-warn -mt-4 font-mono text-xs" role="status">Unsaved changes</p>
		{/if}

		<section class="grid gap-6 lg:grid-cols-[14rem_1fr]" aria-labelledby="p-basics">
			<h2 id="p-basics" class="font-display text-xl font-semibold">Basics</h2>
			<div class="grid gap-4 sm:grid-cols-2">
				<div>
					<label class="field-label" for="p-name">Name</label>
					<input id="p-name" class="input" bind:value={form.name} required autocomplete="name" />
				</div>
				<div>
					<label class="field-label" for="p-pronouns">Pronouns</label>
					<input id="p-pronouns" class="input" bind:value={form.pronouns} placeholder="he/him" />
				</div>
				<div class="sm:col-span-2">
					<label class="field-label" for="p-headline">Headline</label>
					<input id="p-headline" class="input" bind:value={form.headline} />
				</div>
				<div>
					<label class="field-label" for="p-location">Location</label>
					<input id="p-location" class="input" bind:value={form.location} />
				</div>
				<div>
					<label class="field-label" for="p-email">Public email</label>
					<input id="p-email" class="input" type="email" bind:value={form.email} />
				</div>
				<div class="sm:col-span-2">
					<Switch bind:checked={form.available_for_work} label="Available for work" />
				</div>
			</div>
		</section>

		<section class="grid gap-6 lg:grid-cols-[14rem_1fr]" aria-labelledby="p-images">
			<h2 id="p-images" class="font-display text-xl font-semibold">Images and files</h2>
			<div class="grid gap-5 sm:grid-cols-2">
				<MediaField id="p-avatar" label="Avatar" aspect="1:1" bind:value={form.avatar_url} />
				<MediaField id="p-cover" label="Cover" aspect="16:9" bind:value={form.cover_url} />
				<MediaField
					id="p-resume"
					label="Résumé (PDF)"
					accept="application/pdf"
					image={false}
					bind:value={form.resume_url}
				/>
			</div>
		</section>

		<section class="grid gap-6 lg:grid-cols-[14rem_1fr]" aria-labelledby="p-bio">
			<h2 id="p-bio" class="font-display text-xl font-semibold">Bio</h2>
			<MarkdownEditor bind:value={form.bio_md} label="Bio" height="22rem" />
		</section>

		<section class="grid gap-6 lg:grid-cols-[14rem_1fr]" aria-labelledby="p-socials">
			<h2 id="p-socials" class="font-display text-xl font-semibold">Links</h2>
			<SocialsEditor bind:items={form.socials} />
		</section>

		<section class="grid gap-6 lg:grid-cols-[14rem_1fr]" aria-labelledby="p-skills">
			<h2 id="p-skills" class="font-display text-xl font-semibold">Skills</h2>
			<SkillsEditor bind:items={form.skills} />
		</section>

		<section class="grid gap-6 lg:grid-cols-[14rem_1fr]" aria-labelledby="p-exp">
			<h2 id="p-exp" class="font-display text-xl font-semibold">Experience</h2>
			<ExperienceEditor bind:items={form.experience} />
		</section>

		<section class="grid gap-6 lg:grid-cols-[14rem_1fr]" aria-labelledby="p-seo">
			<h2 id="p-seo" class="font-display text-xl font-semibold">SEO</h2>
			<div class="flex flex-col gap-4">
				<div>
					<label class="field-label" for="p-seo-title">Site title</label>
					<input id="p-seo-title" class="input" bind:value={form.seo.title} maxlength="70" />
				</div>
				<div>
					<label class="field-label" for="p-seo-desc">Description</label>
					<textarea
						id="p-seo-desc"
						class="input"
						rows="2"
						maxlength="200"
						bind:value={form.seo.description}
						aria-describedby="p-seo-desc-hint"></textarea>
					<p id="p-seo-desc-hint" class="hint">{form.seo.description.length}/200 characters</p>
				</div>
				<MediaField
					id="p-og"
					label="Social preview image"
					aspect="16:9"
					bind:value={form.seo.og_image}
				/>
			</div>
		</section>
	</form>
{/if}

<Dialog
	bind:open={importOpen}
	title="Import from GitHub"
	description="Only empty fields are filled{auth.config?.github_user
		? ` from @${auth.config.github_user}`
		: ''}. Nothing is saved until you press Save."
>
	<ul class="flex flex-col gap-2">
		{#each fills as f (f.key)}
			<li class="border-b border-rule pb-2 last:border-0">
				<p class="eyebrow">{f.label}</p>
				<p class="text-sm break-words">
					{f.value.length > 160 ? f.value.slice(0, 160) + '…' : f.value}
				</p>
			</li>
		{/each}
	</ul>
	{#snippet footer()}
		<button type="button" class="btn btn-secondary" onclick={() => (importOpen = false)}
			>Cancel</button
		>
		<button type="button" class="btn btn-primary" onclick={applyImport}>
			Fill {fills.length}
			{fills.length === 1 ? 'field' : 'fields'}
		</button>
	{/snippet}
</Dialog>

<ConfirmDialog
	bind:open={confirmUploads}
	title="Uploads still in progress"
	message={uploads.message}
	confirmLabel="Save anyway"
	onconfirm={() => save(undefined, true)}
/>
