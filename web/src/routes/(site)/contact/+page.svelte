<script lang="ts">
	import { tick } from 'svelte';
	import Send from '@lucide/svelte/icons/send';
	import Check from '@lucide/svelte/icons/check';
	import CircleAlert from '@lucide/svelte/icons/circle-alert';
	import Seo from '#lib/site/components/Seo.svelte';
	import SocialIcon from '#lib/site/components/SocialIcon.svelte';
	import { api, ApiRequestError } from '#lib/api/client.js';
	import { prettyUrl, safeUrl } from '#lib/site/format.js';
	import type { ContactInput } from '#lib/api/types.js';
	import { turnstile, resetTurnstile } from '#lib/site/turnstile.js';
	import { PUBLIC_TURNSTILE_SITE_KEY } from '$app/env/public';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const LIMITS = { name: 100, subject: 150, messageMin: 10, message: 5000 };
	const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

	let form = $state({ name: '', email: '', subject: '', message: '', website: '' });
	// Render time (re-evaluated on hydration, so it is the browser's clock).
	let startedAt = $state(Date.now());
	let status = $state<'idle' | 'sending' | 'sent' | 'error'>('idle');
	let serverError = $state('');
	let submitted = $state(false);
	let touched = $state<Record<string, boolean>>({});
	// Cloudflare Turnstile token (only when a site key is configured).
	let captchaToken = $state('');
	const captchaOn = !!PUBLIC_TURNSTILE_SITE_KEY;

	const errors = $derived({
		name: !form.name.trim()
			? 'Please tell me your name.'
			: form.name.trim().length > LIMITS.name
				? `Keep it under ${LIMITS.name} characters.`
				: '',
		email: !EMAIL.test(form.email.trim()) ? 'That email address doesn’t look right.' : '',
		subject: form.subject.length > LIMITS.subject ? `Keep it under ${LIMITS.subject} characters.` : '',
		message:
			form.message.trim().length < LIMITS.messageMin
				? `A few more words, please (at least ${LIMITS.messageMin} characters).`
				: form.message.length > LIMITS.message
					? `That’s a lot! Keep it under ${LIMITS.message} characters.`
					: ''
	});
	type Field = keyof typeof errors;
	const show = (f: Field) => (submitted || touched[f]) && errors[f] ? errors[f] : '';
	const valid = $derived(Object.values(errors).every((e) => !e));

	const contacts = $derived([
		...(data.profile.email
			? [{ label: 'Email', icon: 'email', href: `mailto:${data.profile.email}`, text: data.profile.email }]
			: []),
		...data.profile.socials
			.filter((s) => s.icon !== 'email')
			.map((s) => ({ label: s.label, icon: s.icon, href: safeUrl(s.url), text: s.handle || prettyUrl(s.url) }))
			.filter((s): s is typeof s & { href: string } => !!s.href)
	]);

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		submitted = true;
		if (!valid) {
			const first = (Object.keys(errors) as Field[]).find((f) => errors[f]);
			if (first) document.getElementById(`c-${first}`)?.focus();
			return;
		}
		if (captchaOn && !captchaToken) {
			status = 'error';
			serverError = 'Please complete the verification above the Send button, then send again.';
			await tick();
			document.getElementById('form-error')?.focus();
			return;
		}
		status = 'sending';
		serverError = '';
		const body: ContactInput = {
			name: form.name.trim(),
			email: form.email.trim(),
			subject: form.subject.trim(),
			message: form.message.trim(),
			website: form.website,
			started_at: startedAt,
			...(captchaOn ? { turnstile_token: captchaToken } : {})
		};
		try {
			await api<{ ok: boolean }>('/api/v1/contact', { method: 'POST', body });
			status = 'sent';
		} catch (err) {
			status = 'error';
			// A Turnstile token is single-use: get a fresh one for the retry.
			if (captchaOn) {
				captchaToken = '';
				resetTurnstile();
			}
			serverError =
				err instanceof ApiRequestError && (err.status === 422 || err.status === 429)
					? err.message
					: 'Your message didn’t go through. Please try again, or email me directly.';
			await tick();
			document.getElementById('form-error')?.focus();
		}
	}

	function again() {
		form = { name: '', email: '', subject: '', message: '', website: '' };
		touched = {};
		submitted = false;
		captchaToken = '';
		status = 'idle';
		startedAt = Date.now();
	}
</script>

<Seo title="Contact" description="Get in touch with {data.profile.name}: projects, work, or just to say hello." />

<div class="mx-auto max-w-6xl px-4 sm:px-6">
	<header class="border-b border-ink pt-12 pb-6 sm:pt-16">
		<p class="label text-ink-soft">Write in</p>
		<h1 class="mt-3 font-display text-5xl font-semibold tracking-[-0.035em] sm:text-6xl">Say hello</h1>
		<p class="mt-3 max-w-2xl text-lg text-ink-soft">
			A project, a job, a question about something I wrote: my inbox is open. I usually reply within a couple of days.
		</p>
	</header>

	<div class="grid gap-12 py-10 lg:grid-cols-[minmax(0,7fr)_minmax(0,4fr)]">
		<section aria-labelledby="form-h" class="min-w-0">
			<h2 id="form-h" class="sr-only">Contact form</h2>
			{#if status === 'sent'}
				<div class="print rounded-brand bg-paper-2 p-6 [--print:var(--spot-mint)]" role="status">
					<p class="chip label [--chip:var(--spot-mint)]"><Check class="size-4" aria-hidden="true" /> Sent</p>
					<p class="mt-4 font-display text-3xl font-semibold tracking-[-0.02em]">Thanks, {form.name.trim().split(' ')[0]}!</p>
					<p class="mt-2 text-ink-soft">
						Your message is in. I'll reply to <strong class="text-ink">{form.email.trim()}</strong> soon.
					</p>
					<button type="button" class="btn btn-secondary mt-6" onclick={again}>Send another</button>
				</div>
			{:else}
				<form class="space-y-6" novalidate onsubmit={submit} aria-describedby={status === 'error' ? 'form-error' : undefined}>
					{#if status === 'error'}
						<div id="form-error" tabindex="-1" class="flex gap-3 rounded-[10px] border border-ink bg-spot-rose p-4 text-on-spot" role="alert">
							<CircleAlert class="mt-0.5 size-5 shrink-0" aria-hidden="true" />
							<p>{serverError}</p>
						</div>
					{/if}

					<div class="grid gap-6 sm:grid-cols-2">
						{@render field('name', 'Name', 'text', 'name', LIMITS.name)}
						{@render field('email', 'Email', 'email', 'email')}
					</div>
					{@render field('subject', 'Subject', 'text', 'off', LIMITS.subject, true)}

					<div>
						<div class="flex items-baseline justify-between gap-2">
							<label for="c-message" class="label">Message</label>
							<span class="font-mono text-xs text-ink-soft tabular-nums" aria-hidden="true">
								{form.message.length}/{LIMITS.message}
							</span>
						</div>
						<textarea
							id="c-message"
							name="message"
							rows="8"
							required
							maxlength={LIMITS.message + 200}
							class="input mt-1.5 min-h-40 resize-y"
							aria-invalid={!!show('message')}
							aria-describedby={show('message') ? 'c-message-err' : undefined}
							bind:value={form.message}
							onblur={() => (touched.message = true)}
						></textarea>
						{#if show('message')}<p id="c-message-err" class="err">{show('message')}</p>{/if}
					</div>

					<!-- Honeypot: hidden from people and assistive tech, bots fill it in. -->
					<div class="hp" aria-hidden="true">
						<label for="c-website">Website</label>
						<input id="c-website" name="website" type="text" tabindex="-1" autocomplete="off" bind:value={form.website} />
					</div>

					{#if captchaOn}
						<div
							class="min-h-[65px]"
							{@attach turnstile(PUBLIC_TURNSTILE_SITE_KEY, (t) => (captchaToken = t))}
						></div>
					{/if}

					<button type="submit" class="btn btn-primary" disabled={status === 'sending'}>
						{#if status === 'sending'}Sending…{:else}Send message <Send class="size-4" aria-hidden="true" />{/if}
					</button>
				</form>
			{/if}
		</section>

		<aside aria-labelledby="direct-h">
			<h2 id="direct-h" class="label border-b border-ink pb-2 text-ink-soft">Or reach me directly</h2>
			<ul>
				{#each contacts as c (c.href)}
					{@const external = c.href.startsWith('http')}
					<li class="border-b border-rule">
						<a
							href={c.href}
							class="flex min-h-12 items-center gap-3 py-2 hover:bg-paper-2"
							target={external ? '_blank' : undefined}
							rel={external ? 'me noopener noreferrer' : undefined}
						>
							<SocialIcon icon={c.icon} />
							<span class="font-medium">{c.label}</span>
							<span class="ml-auto min-w-0 truncate font-mono text-sm text-ink-soft">{c.text}</span>
						</a>
					</li>
				{/each}
			</ul>
		</aside>
	</div>
</div>

{#snippet field(
	key: 'name' | 'email' | 'subject',
	label: string,
	type: string,
	autocomplete: 'name' | 'email' | 'off',
	max?: number,
	optional = false
)}
	<div>
		<label for="c-{key}" class="label">
			{label}{#if optional}<span class="ml-1.5 text-ink-soft normal-case tracking-normal">(optional)</span>{/if}
		</label>
		<input
			id="c-{key}"
			name={key}
			{type}
			{autocomplete}
			required={!optional}
			maxlength={max ? max + 50 : undefined}
			class="input mt-1.5"
			aria-invalid={!!show(key)}
			aria-describedby={show(key) ? `c-${key}-err` : undefined}
			bind:value={form[key]}
			onblur={() => (touched[key] = true)}
		/>
		{#if show(key)}<p id="c-{key}-err" class="err">{show(key)}</p>{/if}
	</div>
{/snippet}

<style>
	.input {
		display: block;
		width: 100%;
		min-height: 2.75rem;
		padding: 0.6rem 0.8rem;
		border: 1px solid var(--ink);
		border-radius: 8px;
		background: var(--paper);
		color: var(--ink);
		font: inherit;
		transition: box-shadow 160ms ease-out;
	}
	.input:focus-visible {
		outline: 2px solid var(--ink);
		outline-offset: 2px;
		box-shadow: 3px 3px 0 var(--spot-select);
	}
	.input[aria-invalid='true'] {
		border-color: var(--danger);
		border-width: 2px;
	}
	.err {
		margin-top: 0.4rem;
		font-size: 0.875rem;
		color: var(--danger);
	}
	.hp {
		position: absolute;
		left: -10000px;
		width: 1px;
		height: 1px;
		overflow: hidden;
	}
	button:disabled {
		opacity: 0.7;
		cursor: progress;
	}
</style>
