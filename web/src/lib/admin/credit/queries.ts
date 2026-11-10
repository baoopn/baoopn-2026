// Credit query and mutation option factories (TanStack Query).
// The PUT returns the fresh AdminCredit, which goes straight into the cache.

import { mutationOptions, queryOptions, type QueryClient } from '@tanstack/svelte-query';
import type { AdminCredit, CreditInput } from '#lib/api/types.js';
import { get, put } from '../api';

export const creditKey = ['credit'] as const;

export function creditQuery() {
	return queryOptions({
		queryKey: creditKey,
		queryFn: () => get<AdminCredit>('/credit')
	});
}

export function saveCreditMutation(qc: QueryClient) {
	return mutationOptions({
		mutationFn: (input: CreditInput) => put<AdminCredit>('/credit', input),
		onSuccess: (c) => {
			qc.setQueryData(creditKey, c);
		}
	});
}
