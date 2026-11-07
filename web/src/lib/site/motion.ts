/**
 * Soft motion helpers for the public site (tokens: --motion-* / --ease-* in tokens.css).
 * Only transform and opacity move. Nothing is hidden without JS, and nothing that is
 * already on screen at hydration is hidden, so first paint and LCP stay untouched.
 */
import type { Attachment } from 'svelte/attachments';
import { cubicOut } from 'svelte/easing';
import type { TransitionConfig } from 'svelte/transition';

const REDUCE = '(prefers-reduced-motion: reduce)';
const MAX_STAGGER = 4;

/** True when the reader asked for reduced motion (false on the server). */
export function reducedMotion(): boolean {
	return typeof matchMedia === 'function' && matchMedia(REDUCE).matches;
}

let io: IntersectionObserver | undefined;

function show(el: HTMLElement, step: number) {
	el.style.setProperty('--reveal-delay', `calc(${step} * var(--motion-stagger))`);
	el.classList.remove('reveal-pending');
	let done = false;
	const clear = () => {
		if (done) return;
		done = true;
		el.style.removeProperty('--reveal-delay');
		el.removeEventListener('transitionend', clear);
	};
	el.addEventListener('transitionend', clear);
	// Fallback if transitionend never fires (tab hidden, transition cut short).
	setTimeout(clear, 1000);
}

function observer(): IntersectionObserver {
	io ??= new IntersectionObserver(
		(entries) => {
			const hits = entries
				.filter((e) => e.isIntersecting)
				.sort(
					(a, b) =>
						a.boundingClientRect.top - b.boundingClientRect.top ||
						a.boundingClientRect.left - b.boundingClientRect.left
				);
			hits.forEach((e, i) => {
				io?.unobserve(e.target);
				show(e.target as HTMLElement, Math.min(i, MAX_STAGGER));
			});
		},
		{ rootMargin: '0px 0px -8% 0px', threshold: 0 }
	);
	return io;
}

/**
 * Fades and lifts an element in when it scrolls into view. Pair with `data-reveal`.
 * Elements already in the viewport at hydration are left alone.
 */
export function reveal(): Attachment<HTMLElement> {
	return (node) => {
		if (reducedMotion() || typeof IntersectionObserver === 'undefined') return;
		if (node.getBoundingClientRect().top < innerHeight) return;
		node.classList.add('reveal-pending');
		const obs = observer();
		obs.observe(node);
		return () => {
			obs.unobserve(node);
			node.classList.remove('reveal-pending');
		};
	};
}

/**
 * Fades a lazy image in once it loads. Pair with `data-img-in`. Images that finished
 * loading before hydration (node.complete) are left alone.
 */
export function imgIn(): Attachment<HTMLImageElement> {
	return (node) => {
		if (reducedMotion() || node.complete) return;
		node.classList.add('img-pending');
		const done = () => {
			node.classList.remove('img-pending');
			node.removeEventListener('load', done);
			node.removeEventListener('error', done);
		};
		node.addEventListener('load', done);
		node.addEventListener('error', done);
		return () => {
			done();
		};
	};
}

/**
 * Opacity-only fade for Svelte `in:`/`out:`. Svelte transitions run on WAAPI, so the
 * global reduced-motion CSS does not reach them; this checks the media query itself.
 */
export function fadeSoft(_node: Element): TransitionConfig {
	if (reducedMotion()) return { duration: 0 };
	return { duration: 180, easing: cubicOut, css: (t) => `opacity:${t}` };
}
