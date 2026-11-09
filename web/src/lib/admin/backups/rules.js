// Schedule form rules, mirroring the API's validation (contract §2).
// Plain JS so `node --test` can load it. Empty field = use the server default.

/** @typedef {'backup_time' | 'keep_days' | 'keep_min' | 'max_age_hours'} BackupField */
/** @typedef {Record<BackupField, string>} BackupForm */

export const TIME_RE = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;

/** @type {Record<Exclude<BackupField, 'backup_time'>, { min: number; max: number; message: string }>} */
export const RANGES = {
	keep_days: { min: 0, max: 3650, message: 'Enter a whole number from 0 to 3650.' },
	keep_min: { min: 0, max: 1000, message: 'Enter a whole number from 0 to 1000.' },
	max_age_hours: { min: 25, max: 720, message: 'Enter a whole number from 25 to 720.' }
};

export const TIME_MESSAGE = 'Use 24-hour HH:MM, for example 03:00.';

/** Plain-English message per field (used for client errors and API 422s). */
/** @type {Record<BackupField, string>} */
export const MESSAGES = {
	backup_time: TIME_MESSAGE,
	keep_days: RANGES.keep_days.message,
	keep_min: RANGES.keep_min.message,
	max_age_hours: RANGES.max_age_hours.message
};

/** @type {BackupField[]} */
export const FIELDS = ['backup_time', 'keep_days', 'keep_min', 'max_age_hours'];

/**
 * Form values from the stored overrides ("" where the default applies).
 * @param {{ [K in BackupField]: { override: string | number | null } }} settings
 * @returns {BackupForm}
 */
export function formFromSettings(settings) {
	/** @type {BackupForm} */
	const f = { backup_time: '', keep_days: '', keep_min: '', max_age_hours: '' };
	for (const k of FIELDS) {
		const v = settings[k]?.override;
		f[k] = v == null ? '' : String(v);
	}
	return f;
}

/**
 * Check the form and build the PUT body.
 * @param {BackupForm} form
 * @returns {{ input: { backup_time: string | null; keep_days: number | null; keep_min: number | null; max_age_hours: number | null }; errors: Partial<Record<BackupField, string>> }}
 */
export function validateForm(form) {
	/** @type {Partial<Record<BackupField, string>>} */
	const errors = {};
	const time = form.backup_time.trim();
	if (time && !TIME_RE.test(time)) errors.backup_time = TIME_MESSAGE;

	/** @param {'keep_days' | 'keep_min' | 'max_age_hours'} k */
	const int = (k) => {
		const s = form[k].trim();
		if (!s) return null;
		const r = RANGES[k];
		const n = /^\d{1,6}$/.test(s) ? Number(s) : NaN;
		if (!Number.isInteger(n) || n < r.min || n > r.max) {
			errors[k] = r.message;
			return null;
		}
		return n;
	};

	return {
		input: {
			backup_time: time && !errors.backup_time ? time : null,
			keep_days: int('keep_days'),
			keep_min: int('keep_min'),
			max_age_hours: int('max_age_hours')
		},
		errors
	};
}

/**
 * Which field a 422 message from the API is about (it names the field), or null.
 * @param {string} message
 * @returns {BackupField | null}
 */
export function fieldFromMessage(message) {
	return FIELDS.find((k) => message.includes(k)) ?? null;
}
