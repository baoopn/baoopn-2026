export type ToastKind = 'success' | 'error' | 'info' | 'warning';

export type Toast = {
	id: number;
	kind: ToastKind;
	title: string;
	message?: string;
	timeout: number;
};

class ToastStore {
	items = $state<Toast[]>([]);
	#next = 1;
	#timers = new Map<number, ReturnType<typeof setTimeout>>();

	push(kind: ToastKind, title: string, message?: string, timeout = kind === 'error' ? 8000 : 4500) {
		const id = this.#next++;
		this.items.push({ id, kind, title, message, timeout });
		if (timeout > 0) {
			this.#timers.set(
				id,
				setTimeout(() => this.dismiss(id), timeout)
			);
		}
		return id;
	}

	success(title: string, message?: string) {
		return this.push('success', title, message);
	}
	error(title: string, message?: string) {
		return this.push('error', title, message);
	}
	info(title: string, message?: string) {
		return this.push('info', title, message);
	}
	warning(title: string, message?: string) {
		return this.push('warning', title, message);
	}

	dismiss(id: number) {
		const t = this.#timers.get(id);
		if (t) clearTimeout(t);
		this.#timers.delete(id);
		this.items = this.items.filter((x) => x.id !== id);
	}
}

export const toasts = new ToastStore();
