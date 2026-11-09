// Backups query and mutation option factories (TanStack Query).
// One key for the whole page: every mutation returns a fresh BackupStatus
// (or a queued run), which goes straight into the cache.

import { mutationOptions, queryOptions, type QueryClient } from '@tanstack/svelte-query';
import type { BackupRun, BackupSettingsInput, BackupStatus } from '#lib/api/types.js';
import { admin, get, post, put } from '../api';

export const backupsKey = ['backups'] as const;

/** Poll fast while a backup is queued or running, slowly otherwise. */
export const ACTIVE_POLL_MS = 3_000;
export const IDLE_POLL_MS = 30_000;

export function backupsQuery() {
	return queryOptions({
		queryKey: backupsKey,
		queryFn: () => get<BackupStatus>('/backups'),
		refetchInterval: (q) => (q.state.data?.active_run ? ACTIVE_POLL_MS : IDLE_POLL_MS)
	});
}

function store(qc: QueryClient, s: BackupStatus): void {
	qc.setQueryData(backupsKey, s);
}

export function saveSettingsMutation(qc: QueryClient) {
	return mutationOptions({
		mutationFn: (input: BackupSettingsInput) => put<BackupStatus>('/backups/settings', input),
		onSuccess: (s) => store(qc, s)
	});
}

export function resetSettingsMutation(qc: QueryClient) {
	return mutationOptions({
		// `del` returns void; this DELETE returns the status (overrides cleared, paused kept).
		mutationFn: () => admin<BackupStatus>('/backups/settings', { method: 'DELETE' }),
		onSuccess: (s) => store(qc, s)
	});
}

export function pauseMutation(qc: QueryClient) {
	return mutationOptions({
		mutationFn: (paused: boolean) => put<BackupStatus>('/backups/pause', { paused }),
		onSuccess: (s) => store(qc, s)
	});
}

/** Stand-in for the queued run when the response body is missing. */
function queuedPlaceholder(): BackupRun {
	return {
		id: 0,
		kind: 'manual',
		status: 'queued',
		requested_at: new Date().toISOString(),
		started_at: null,
		finished_at: null,
		name: null,
		size_bytes: null,
		sha256: null,
		tables: null,
		goose_version: null,
		pruned: null,
		prune_error: null,
		error: null
	};
}

export function runNowMutation(qc: QueryClient) {
	return mutationOptions({
		// The shared JSON client drops 202 bodies, so `run` may be undefined.
		mutationFn: () => post<BackupRun | undefined>('/backups/run'),
		onSuccess: (run) => {
			qc.setQueryData<BackupStatus>(backupsKey, (cur) =>
				cur ? { ...cur, active_run: run ?? queuedPlaceholder() } : cur
			);
			return qc.invalidateQueries({ queryKey: backupsKey });
		},
		onError: () => qc.invalidateQueries({ queryKey: backupsKey })
	});
}
