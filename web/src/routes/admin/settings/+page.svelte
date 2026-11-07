<script lang="ts">
	import { CircleCheck, CircleSlash, LoaderCircle, Save, Send } from '@lucide/svelte';
	import type { SiteSettings } from '#lib/api/types.js';
	import PageHeader from '#lib/admin/components/PageHeader.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import Switch from '#lib/admin/components/Switch.svelte';
	import { get, post, put } from '#lib/admin/api.js';
	import { guardUnsaved } from '#lib/admin/guard.svelte.js';
	import { auth } from '#lib/admin/auth.svelte.js';
	import { errorMessage } from '#lib/admin/format.js';
	import { toasts } from '#lib/admin/toast.svelte.js';

	let form = $state<SiteSettings | null>(null);
	let baseline = $state('');
	let loadError = $state<string | null>(null);
	let saving = $state(false);
	let testing = $state(false);
	let emailError = $state<string | null>(null);

	function set(s: SiteSettings) {
		form = { ...s };
		baseline = JSON.stringify(form);
	}

	async function load() {
		loadError = null;
		try {
			set(await get<SiteSettings>('/settings'));
		} catch (err) {
			loadError = errorMessage(err);
		}
	}
	void load();

	const dirty = $derived(!!form && JSON.stringify(form) !== baseline);
	guardUnsaved(() => dirty && !saving);

	async function save(e?: SubmitEvent) {
		e?.preventDefault();
		if (!form || saving) return;
		emailError =
			form.notify_email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.notify_email)
				? 'Enter a valid email address.'
				: null;
		if (emailError) {
			document.getElementById('s-notify')?.focus();
			return;
		}
		saving = true;
		try {
			set(await put<SiteSettings>('/settings', $state.snapshot(form)));
			toasts.success('Settings saved');
		} catch (err) {
			toasts.error('Could not save settings', errorMessage(err));
		} finally {
			saving = false;
		}
	}

	async function testEmail() {
		testing = true;
		try {
			const r = await post<{ sent_to: string }>('/settings/test-email');
			toasts.success('Test email sent', `Check ${r.sent_to}.`);
		} catch (err) {
			toasts.error('Test email failed', errorMessage(err));
		} finally {
			testing = false;
		}
	}

	const cfg = $derived(auth.config);
</script>

<PageHeader title="Settings" eyebrow="Site" description="Contact form, mail and replies.">
	{#snippet actions()}
		<button type="submit" form="settings-form" class="btn btn-primary" disabled={!dirty || saving}>
			{#if saving}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{:else}<Save
					class="size-4"
					aria-hidden="true"
				/>{/if}
			Save
		</button>
	{/snippet}
</PageHeader>

<div class="grid gap-10 lg:grid-cols-[minmax(0,1fr)_20rem]">
	{#if loadError}
		<ErrorState message={loadError} onretry={load} />
	{:else if !form}
		<div class="flex flex-col gap-4" aria-busy="true">
			<div class="skeleton h-12"></div>
			<div class="skeleton h-12"></div>
			<div class="skeleton h-40"></div>
			<span class="sr-only">Loading settings</span>
		</div>
	{:else}
		<form id="settings-form" class="flex flex-col gap-6" onsubmit={save} novalidate>
			<div>
				<label class="field-label" for="s-notify">Notification email</label>
				<input
					id="s-notify"
					class="input"
					type="email"
					bind:value={form.notify_email}
					oninput={() => (emailError = null)}
					aria-invalid={!!emailError}
					aria-describedby="s-notify-msg"
					autocomplete="email"
				/>
				{#if emailError}
					<p id="s-notify-msg" class="field-error" role="alert">{emailError}</p>
				{:else}
					<p id="s-notify-msg" class="hint">New contact messages are forwarded here.</p>
				{/if}
			</div>

			<div class="flex flex-col border-y border-rule py-2">
				<Switch bind:checked={form.contact_enabled} label="Contact form accepts messages" />
				<Switch
					bind:checked={form.auto_reply_enabled}
					label="Send visitors a copy that starts the email thread"
				/>
			</div>

			<div>
				<label class="field-label" for="s-auto">Auto-reply text</label>
				<textarea
					id="s-auto"
					class="input"
					rows="5"
					bind:value={form.auto_reply_text}
					disabled={!form.auto_reply_enabled}
					aria-describedby="s-auto-hint"></textarea>
				<p id="s-auto-hint" class="hint">Sent above a copy of the visitor's message.</p>
			</div>

			<div>
				<label class="field-label" for="s-sig">Reply signature</label>
				<textarea
					id="s-sig"
					class="input font-mono text-sm"
					rows="4"
					bind:value={form.signature}
					aria-describedby="s-sig-hint"></textarea>
				<p id="s-sig-hint" class="hint">Appended to every reply sent from the inbox.</p>
			</div>
			{#if dirty}<p class="text-warn font-mono text-xs" role="status">Unsaved changes</p>{/if}
		</form>
	{/if}

	<aside class="flex flex-col gap-5" aria-labelledby="mail-h">
		<section class="card p-4">
			<h2 id="mail-h" class="eyebrow mb-3">Mail status</h2>
			<ul class="flex flex-col gap-3 text-sm">
				{#each [{ on: cfg?.mail_enabled, label: 'Outgoing mail (SMTP)', off: 'Replies and notifications are disabled.' }, { on: cfg?.inbound_enabled, label: 'Inbound email replies', off: 'Email replies do not reach the inbox.' }] as row (row.label)}
					<li class="flex items-start gap-2">
						{#if row.on}
							<CircleCheck class="text-ok mt-0.5 size-4 shrink-0" aria-hidden="true" />
						{:else}
							<CircleSlash class="text-danger mt-0.5 size-4 shrink-0" aria-hidden="true" />
						{/if}
						<span>
							<span class="font-medium">{row.label}</span>
							<span class="block text-ink-soft">{row.on ? 'Configured' : `Off. ${row.off}`}</span>
						</span>
					</li>
				{/each}
			</ul>
			<button
				type="button"
				class="btn btn-secondary mt-4 w-full"
				onclick={testEmail}
				disabled={testing || !cfg?.mail_enabled}
				aria-describedby="test-hint"
			>
				{#if testing}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{:else}<Send
						class="size-4"
						aria-hidden="true"
					/>{/if}
				Send test email
			</button>
			<p id="test-hint" class="hint">
				{cfg?.mail_enabled
					? 'Sends a test message to the notification email.'
					: 'Configure SMTP on the API to enable this.'}
			</p>
		</section>
		<section class="card p-4 font-mono text-xs text-ink-soft">
			<h2 class="eyebrow mb-2">About</h2>
			<p>API version <span class="text-ink">{cfg?.version || '–'}</span></p>
			<p>GitHub user <span class="text-ink">{cfg?.github_user || '–'}</span></p>
		</section>
	</aside>
</div>
