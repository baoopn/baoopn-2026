import { get } from './api';

/** Shared inbox state: the unread badge, and a version bump to refresh the thread list. */
class InboxState {
	unread = $state(0);
	/** Incremented when a thread changes, so the list reloads silently. */
	version = $state(0);

	async refreshUnread(): Promise<void> {
		try {
			const r = await get<{ count: number }>('/threads/unread-count');
			this.unread = r.count;
		} catch {
			// keep the last value; the badge is best-effort
		}
	}

	changed(): void {
		this.version++;
		void this.refreshUnread();
	}

	/** Poll the unread count every `ms`; returns a stop function. */
	poll(ms = 60_000): () => void {
		void this.refreshUnread();
		const id = setInterval(() => {
			if (document.visibilityState === 'visible') void this.refreshUnread();
		}, ms);
		return () => clearInterval(id);
	}
}

export const inbox = new InboxState();
